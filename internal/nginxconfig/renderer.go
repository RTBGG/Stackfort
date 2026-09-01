// SPDX-License-Identifier: AGPL-3.0-or-later

// Package nginxconfig renders bounded NGINX account configuration from typed,
// already persisted domain state. It has no filesystem or process side effects.
package nginxconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

const (
	MaximumDomains        = 10_000
	MaximumRenderedBytes  = 4 << 20
	maximumHeaderPolicies = 3
)

var (
	ErrInvalidSpec       = errors.New("invalid NGINX account configuration specification")
	ErrUnsupportedTarget = errors.New("unsupported NGINX domain target")
	ErrRenderedTooLarge  = errors.New("rendered NGINX account configuration exceeds limit")
)

type HeaderPolicy string

const (
	HeaderNoSniff                 HeaderPolicy = "x-content-type-options-nosniff"
	HeaderReferrerStrictOrigin    HeaderPolicy = "referrer-policy-strict-origin"
	HeaderPermissionsSensitiveOff HeaderPolicy = "permissions-sensitive-features-off"
)

type Options struct {
	HeaderPolicies       []HeaderPolicy                `json:"headerPolicies"`
	OCIUpstreams         []core.OCIApplicationUpstream `json:"ociUpstreams"`
	ActivationRevisionID string                        `json:"-"`
}

// DomainSpec is the minimal account-scoped domain intent allowed to cross the
// control API/agent boundary. It deliberately has no raw configuration field.
type DomainSpec struct {
	Name             core.NormalizedDomainName `json:"name"`
	Status           core.DomainStatus         `json:"status"`
	CanonicalMode    core.CanonicalMode        `json:"canonicalMode"`
	HTTP01Challenge  bool                      `json:"http01Challenge"`
	TLSCertificateID string                    `json:"tlsCertificateId,omitempty"`
	WAFMode          core.WAFMode              `json:"wafMode,omitempty"`
	WAFExceptions    []WAFExceptionSpec        `json:"wafExceptions"`
	CachePreset      core.CachePreset          `json:"cachePreset,omitempty"`
	Target           TargetSpec                `json:"target"`
}

type WAFExceptionSpec struct {
	ID          string    `json:"id"`
	RuleID      uint32    `json:"ruleId"`
	RequestPath string    `json:"requestPath,omitempty"`
	Parameter   string    `json:"parameter,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// TargetSpec is a tagged union for the target types implemented by this
// renderer. DocumentRoot is account-relative and Redirect is structured.
type TargetSpec struct {
	Type          core.DomainTargetType `json:"type"`
	DocumentRoot  string                `json:"documentRoot,omitempty"`
	PHPVersion    string                `json:"phpVersion,omitempty"`
	Redirect      *RedirectSpec         `json:"redirect,omitempty"`
	ApplicationID string                `json:"applicationId,omitempty"`
}

type RedirectSpec struct {
	StatusCode         core.RedirectStatusCode `json:"statusCode"`
	TargetURL          string                  `json:"targetUrl"`
	TargetASCIIHost    string                  `json:"targetAsciiHost"`
	HostMode           core.RedirectHostMode   `json:"hostMode"`
	PreservePath       bool                    `json:"preservePath"`
	PreserveQuery      bool                    `json:"preserveQuery"`
	WildcardSubdomains bool                    `json:"wildcardSubdomains"`
}

// DefaultOptions returns a fresh copy of the conservative static-site headers.
func DefaultOptions() Options {
	return Options{HeaderPolicies: []HeaderPolicy{HeaderNoSniff, HeaderReferrerStrictOrigin}}
}

type Rendered struct {
	FileName        string
	Content         []byte
	Digest          [sha256.Size]byte
	RenderedDomains int
}

type renderedHeader struct {
	policy HeaderPolicy
	name   string
	value  string
}

type preparedDomain struct {
	domain             core.Domain
	base               string
	documentRoot       string
	phpSocket          string
	http01             bool
	certificateID      string
	wafProfile         string
	wafExceptions      []preparedWAFException
	cachePreset        core.CachePreset
	activationVariable string
	redirect           *preparedRedirect
	upstreamPort       int64
	applicationID      string
}

type preparedWAFException struct {
	localRuleID uint32
	ruleID      uint32
	requestPath string
	parameter   string
	expiresUnix int64
}

type preparedRedirect struct {
	statusCode       int64
	target           string
	hostMode         core.RedirectHostMode
	preservePath     bool
	preserveQuery    bool
	wildcard         bool
	requiresQueryMap bool
	queryVariable    string
}

// RenderAccount emits one byte-stable account include. Domain and header input
// order is semantically irrelevant and is canonicalized before rendering.
func RenderAccount(
	identity hostingidentity.Spec,
	domains []core.Domain,
	options Options,
) (Rendered, error) {
	if err := hostingidentity.Validate(identity); err != nil || len(domains) > MaximumDomains {
		return Rendered{}, ErrInvalidSpec
	}
	headers, err := prepareHeaders(options.HeaderPolicies)
	if err != nil {
		return Rendered{}, err
	}
	upstreams, err := prepareOCIUpstreams(options.OCIUpstreams)
	if err != nil {
		return Rendered{}, err
	}
	activationVariable := ""
	if options.ActivationRevisionID != "" {
		revisionID, err := core.ParseID(options.ActivationRevisionID)
		if err != nil || string(revisionID) != options.ActivationRevisionID {
			return Rendered{}, ErrInvalidSpec
		}
		activationVariable = "$sf_activation_" + strings.ReplaceAll(identity.AccountID, "-", "")
	}
	prepared := make([]preparedDomain, 0, len(domains))
	for _, domain := range domains {
		item, include, err := prepareDomain(identity, domain, upstreams)
		if err != nil {
			return Rendered{}, err
		}
		if include {
			item.activationVariable = activationVariable
			prepared = append(prepared, item)
		}
	}
	sort.Slice(prepared, func(first, second int) bool {
		return prepared[first].base < prepared[second].base
	})
	if err := validateRouteConflicts(prepared); err != nil {
		return Rendered{}, err
	}
	assignQueryVariables(prepared)

	var output bytes.Buffer
	output.WriteString("# Managed by Stackfort. Do not edit.\n")
	output.WriteString("# Account: ")
	output.WriteString(identity.AccountID)
	output.WriteString("\n\n")
	if activationVariable != "" {
		writeActivationMap(&output, activationVariable, options.ActivationRevisionID)
	}
	for _, item := range prepared {
		if item.redirect != nil && item.redirect.queryVariable != "" {
			writeQueryMap(&output, item.redirect.queryVariable)
		}
	}
	writeOCIUpstreamBlocks(&output, prepared)
	if output.Len() > MaximumRenderedBytes {
		return Rendered{}, ErrRenderedTooLarge
	}
	for _, item := range prepared {
		if item.upstreamPort != 0 {
			writeOCIApplicationDomain(&output, item, headers)
		} else if item.cachePreset != core.CachePresetDisabled {
			writeCachedDomain(&output, item, headers)
		} else if item.redirect == nil {
			writeStaticDomain(&output, item, headers)
		} else {
			writeRedirectDomain(&output, item, headers)
		}
		if output.Len() > MaximumRenderedBytes {
			return Rendered{}, ErrRenderedTooLarge
		}
	}
	content := append([]byte(nil), output.Bytes()...)
	return Rendered{
		FileName:        "account-" + identity.AccountID + ".conf",
		Content:         content,
		Digest:          sha256.Sum256(content),
		RenderedDomains: len(prepared),
	}, nil
}

// SpecsFromDomains validates persisted domain records and converts only their
// rendering intent into the bounded agent wire representation.
func SpecsFromDomains(
	identity hostingidentity.Spec,
	domains []core.Domain,
	options Options,
) ([]DomainSpec, error) {
	if _, err := RenderAccount(identity, domains, options); err != nil {
		return nil, err
	}
	result := make([]DomainSpec, 0, len(domains))
	for _, domain := range domains {
		if domain.Status == core.DomainSuspended || domain.Status == core.DomainRemoved {
			continue
		}
		spec := DomainSpec{
			Name: domain.Name, Status: domain.Status, CanonicalMode: domain.CanonicalMode,
			HTTP01Challenge: domain.TLS.Enabled && domain.TLS.Mode == core.TLSModeACME &&
				domain.TLS.ChallengeType == core.TLSChallengeHTTP01,
			TLSCertificateID: domain.TLS.ActiveCertificateRef,
			WAFMode:          domain.WAF.Mode,
			WAFExceptions:    make([]WAFExceptionSpec, 0, len(domain.WAF.Exceptions)),
			CachePreset:      domain.Cache.Preset,
			Target:           TargetSpec{Type: domain.Target.Type},
		}
		for _, exception := range domain.WAF.Exceptions {
			spec.WAFExceptions = append(spec.WAFExceptions, WAFExceptionSpec{
				ID: string(exception.ID), RuleID: exception.RuleID,
				RequestPath: exception.RequestPath, Parameter: exception.Parameter,
				ExpiresAt: exception.ExpiresAt,
			})
		}
		switch domain.Target.Type {
		case core.DomainTargetStatic, core.DomainTargetPHP:
			spec.Target.DocumentRoot = domain.Target.DocumentRoot.RelativePath
			spec.Target.PHPVersion = domain.Target.PHPVersion
		case core.DomainTargetRedirect:
			redirect := domain.Target.Redirect
			spec.Target.Redirect = &RedirectSpec{
				StatusCode: redirect.StatusCode, TargetURL: redirect.TargetURL,
				TargetASCIIHost: redirect.TargetASCIIHost, HostMode: redirect.HostMode,
				PreservePath:  redirect.PreservePath,
				PreserveQuery: redirect.PreserveQuery, WildcardSubdomains: redirect.WildcardSubdomains,
			}
		case core.DomainTargetOCIApplication:
			if domain.Target.ApplicationID == nil {
				return nil, ErrInvalidSpec
			}
			spec.Target.ApplicationID = string(*domain.Target.ApplicationID)
		default:
			return nil, ErrUnsupportedTarget
		}
		result = append(result, spec)
	}
	return result, nil
}

// RenderSpecs rehydrates account ownership server-side and then applies the
// same invariant checks as rendering directly from persisted records.
func RenderSpecs(
	identity hostingidentity.Spec,
	domains []DomainSpec,
	options Options,
) (Rendered, error) {
	accountID := core.ID(identity.AccountID)
	rehydrated := make([]core.Domain, 0, len(domains))
	for _, domain := range domains {
		item := core.Domain{
			AccountID: accountID, Name: domain.Name, Status: domain.Status,
			CanonicalMode: domain.CanonicalMode,
			WAF:           core.DomainWAFPolicy{Mode: domain.WAFMode},
			Cache:         core.DomainCachePolicy{Preset: domain.CachePreset},
			Target:        core.DomainTarget{Type: domain.Target.Type},
		}
		item.WAF.Exceptions = make([]core.DomainWAFException, 0, len(domain.WAFExceptions))
		for _, exception := range domain.WAFExceptions {
			item.WAF.Exceptions = append(item.WAF.Exceptions, core.DomainWAFException{
				ID: core.ID(exception.ID), AccountID: accountID, RuleID: exception.RuleID,
				RequestPath: exception.RequestPath, Parameter: exception.Parameter,
				ExpiresAt: exception.ExpiresAt,
			})
		}
		if domain.HTTP01Challenge {
			item.TLS = core.DomainTLSState{
				Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
			}
		}
		if domain.TLSCertificateID != "" {
			item.TLS = core.DomainTLSState{
				Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
				IssuanceStatus: core.TLSActive, ActiveCertificateRef: domain.TLSCertificateID,
				Names: []string{domain.Name.ASCII, "www." + domain.Name.ASCII},
			}
		}
		switch domain.Target.Type {
		case core.DomainTargetStatic, core.DomainTargetPHP:
			if domain.Target.DocumentRoot == "" || domain.Target.Redirect != nil ||
				domain.Target.Type == core.DomainTargetStatic && domain.Target.PHPVersion != "" ||
				domain.Target.Type == core.DomainTargetPHP && domain.Target.PHPVersion == "" {
				return Rendered{}, ErrInvalidSpec
			}
			item.Target.DocumentRoot = &core.DocumentRoot{
				AccountID: accountID, RelativePath: domain.Target.DocumentRoot,
			}
			item.Target.PHPVersion = domain.Target.PHPVersion
		case core.DomainTargetRedirect:
			if domain.Target.DocumentRoot != "" || domain.Target.Redirect == nil {
				return Rendered{}, ErrInvalidSpec
			}
			redirect := domain.Target.Redirect
			item.Target.Redirect = &core.DomainRedirect{
				StatusCode: redirect.StatusCode, TargetURL: redirect.TargetURL,
				TargetASCIIHost: redirect.TargetASCIIHost, HostMode: redirect.HostMode,
				PreservePath:  redirect.PreservePath,
				PreserveQuery: redirect.PreserveQuery, WildcardSubdomains: redirect.WildcardSubdomains,
			}
		case core.DomainTargetOCIApplication:
			applicationID, err := core.ParseID(domain.Target.ApplicationID)
			if err != nil || domain.Target.ApplicationID != string(applicationID) ||
				domain.Target.DocumentRoot != "" || domain.Target.PHPVersion != "" || domain.Target.Redirect != nil {
				return Rendered{}, ErrInvalidSpec
			}
			item.Target.ApplicationID = &applicationID
		default:
			return Rendered{}, ErrInvalidSpec
		}
		rehydrated = append(rehydrated, item)
	}
	return RenderAccount(identity, rehydrated, options)
}

func prepareHeaders(policies []HeaderPolicy) ([]renderedHeader, error) {
	if len(policies) > maximumHeaderPolicies {
		return nil, ErrInvalidSpec
	}
	headers := make([]renderedHeader, 0, len(policies))
	seen := make(map[HeaderPolicy]struct{}, len(policies))
	for _, policy := range policies {
		if _, exists := seen[policy]; exists {
			return nil, ErrInvalidSpec
		}
		seen[policy] = struct{}{}
		switch policy {
		case HeaderNoSniff:
			headers = append(headers, renderedHeader{policy, "X-Content-Type-Options", "nosniff"})
		case HeaderReferrerStrictOrigin:
			headers = append(headers, renderedHeader{policy, "Referrer-Policy", "strict-origin-when-cross-origin"})
		case HeaderPermissionsSensitiveOff:
			headers = append(headers, renderedHeader{policy, "Permissions-Policy", "camera=(), microphone=(), geolocation=()"})
		default:
			return nil, ErrInvalidSpec
		}
	}
	sort.Slice(headers, func(first, second int) bool { return headers[first].policy < headers[second].policy })
	return headers, nil
}

func prepareOCIUpstreams(values []core.OCIApplicationUpstream) (map[core.ID]int64, error) {
	if len(values) > MaximumDomains {
		return nil, ErrInvalidSpec
	}
	result := make(map[core.ID]int64, len(values))
	ports := make(map[int64]struct{}, len(values))
	previous := ""
	for _, upstream := range values {
		applicationID, err := core.ParseID(string(upstream.ApplicationID))
		if err != nil || applicationID != upstream.ApplicationID || string(applicationID) <= previous ||
			upstream.LoopbackPort < ocideployment.MinimumLoopbackPort ||
			upstream.LoopbackPort > ocideployment.MaximumLoopbackPort {
			return nil, ErrInvalidSpec
		}
		if _, duplicate := ports[upstream.LoopbackPort]; duplicate {
			return nil, ErrInvalidSpec
		}
		previous = string(applicationID)
		ports[upstream.LoopbackPort] = struct{}{}
		result[applicationID] = upstream.LoopbackPort
	}
	return result, nil
}

func prepareDomain(identity hostingidentity.Spec, domain core.Domain,
	upstreams map[core.ID]int64) (preparedDomain, bool, error) {
	if string(domain.AccountID) != identity.AccountID {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	name, err := core.NormalizeDomainName(domain.Name.ASCII)
	if err != nil || name.ASCII != domain.Name.ASCII || strings.HasPrefix(name.ASCII, "www.") {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	display, err := core.NormalizeDomainName(domain.Name.Display)
	if err != nil || display != domain.Name {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	if domain.CanonicalMode != core.CanonicalPreferApex &&
		domain.CanonicalMode != core.CanonicalPreferWWW &&
		domain.CanonicalMode != core.CanonicalServeBoth {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	switch domain.Status {
	case core.DomainPending, core.DomainActive:
	case core.DomainSuspended, core.DomainRemoved:
		return preparedDomain{}, false, nil
	default:
		return preparedDomain{}, false, ErrInvalidSpec
	}
	http01 := domain.TLS.Enabled && domain.TLS.Mode == core.TLSModeACME &&
		domain.TLS.ChallengeType == core.TLSChallengeHTTP01
	certificateID := domain.TLS.ActiveCertificateRef
	if certificateID != "" {
		if _, err := tlsartifact.CertificatePath(certificateID); err != nil ||
			!slices.Equal(domain.TLS.Names, []string{name.ASCII, "www." + name.ASCII}) {
			return preparedDomain{}, false, ErrInvalidSpec
		}
	}
	wafProfile, err := wafconfig.ProfilePath(domain.WAF.Mode)
	if err != nil {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	cachePreset, err := cacheconfig.NormalizePreset(domain.Cache.Preset)
	if err != nil || cachePreset != core.CachePresetDisabled && domain.Target.Type != core.DomainTargetPHP {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	item := preparedDomain{
		domain: domain, base: name.ASCII, http01: http01, certificateID: certificateID,
		wafProfile: wafProfile, cachePreset: cachePreset,
	}
	item.wafExceptions, err = prepareWAFExceptions(domain)
	if err != nil {
		return preparedDomain{}, false, ErrInvalidSpec
	}
	switch domain.Target.Type {
	case core.DomainTargetStatic, core.DomainTargetPHP:
		if domain.Target.DocumentRoot == nil || domain.Target.Redirect != nil ||
			domain.Target.ApplicationID != nil ||
			domain.Target.Type == core.DomainTargetStatic && domain.Target.PHPVersion != "" ||
			domain.Target.Type == core.DomainTargetPHP && domain.Target.PHPVersion == "" ||
			string(domain.Target.DocumentRoot.AccountID) != identity.AccountID {
			return preparedDomain{}, false, ErrInvalidSpec
		}
		relative, normalizeErr := hostingpath.NormalizeDocumentRoot(domain.Target.DocumentRoot.RelativePath)
		if normalizeErr != nil || relative != domain.Target.DocumentRoot.RelativePath {
			return preparedDomain{}, false, ErrInvalidSpec
		}
		item.documentRoot = path.Join(identity.HomeDirectory, relative)
		if domain.Target.Type == core.DomainTargetPHP {
			item.phpSocket, normalizeErr = phpruntime.SocketPath(identity, domain.Target.PHPVersion)
			if normalizeErr != nil {
				return preparedDomain{}, false, ErrInvalidSpec
			}
		}
	case core.DomainTargetRedirect:
		redirect, redirectErr := prepareRedirect(domain)
		if redirectErr != nil {
			return preparedDomain{}, false, redirectErr
		}
		item.redirect = redirect
	case core.DomainTargetOCIApplication:
		if domain.Target.ApplicationID == nil || domain.Target.DocumentRoot != nil ||
			domain.Target.Redirect != nil || domain.Target.PHPVersion != "" {
			return preparedDomain{}, false, ErrInvalidSpec
		}
		port, found := upstreams[*domain.Target.ApplicationID]
		if !found {
			return preparedDomain{}, false, ErrInvalidSpec
		}
		item.upstreamPort = port
		item.applicationID = string(*domain.Target.ApplicationID)
	default:
		return preparedDomain{}, false, ErrInvalidSpec
	}
	return item, true, nil
}

func prepareWAFExceptions(domain core.Domain) ([]preparedWAFException, error) {
	if len(domain.WAF.Exceptions) > wafconfig.MaximumExceptions ||
		domain.WAF.Mode == core.WAFModeOff && len(domain.WAF.Exceptions) != 0 {
		return nil, ErrInvalidSpec
	}
	result := make([]preparedWAFException, 0, len(domain.WAF.Exceptions))
	seenIDs := make(map[string]struct{}, len(domain.WAF.Exceptions))
	seenRules := make(map[uint32]struct{}, len(domain.WAF.Exceptions))
	for _, exception := range domain.WAF.Exceptions {
		id, err := core.ParseID(string(exception.ID))
		if err != nil || string(id) != string(exception.ID) || exception.ExpiresAt.IsZero() ||
			wafconfig.ValidateExceptionScope(exception.RuleID, exception.RequestPath, exception.Parameter) != nil ||
			exception.AccountID != "" && exception.AccountID != domain.AccountID ||
			exception.DomainID != "" && domain.ID != "" && exception.DomainID != domain.ID {
			return nil, ErrInvalidSpec
		}
		localRuleID := exceptionRuleID(string(exception.ID))
		if _, duplicate := seenIDs[string(exception.ID)]; duplicate {
			return nil, ErrInvalidSpec
		}
		if _, collision := seenRules[localRuleID]; collision {
			return nil, ErrInvalidSpec
		}
		seenIDs[string(exception.ID)] = struct{}{}
		seenRules[localRuleID] = struct{}{}
		result = append(result, preparedWAFException{
			localRuleID: localRuleID, ruleID: exception.RuleID,
			requestPath: exception.RequestPath, parameter: exception.Parameter,
			expiresUnix: exception.ExpiresAt.UTC().Unix(),
		})
	}
	sort.Slice(result, func(first, second int) bool { return result[first].localRuleID < result[second].localRuleID })
	return result, nil
}

func exceptionRuleID(exceptionID string) uint32 {
	digest := sha256.Sum256([]byte(exceptionID))
	// Keep generated exclusions in Stackfort's closed local-rule range below
	// CRS (920000+). Coraza evaluates phase peers in rule order/ID order, so a
	// billion-range ID would let the protected CRS rule run first.
	return 100_000 + binary.BigEndian.Uint32(digest[:4])%100_000
}

func prepareRedirect(domain core.Domain) (*preparedRedirect, error) {
	if domain.Target.Redirect == nil || domain.Target.DocumentRoot != nil ||
		domain.Target.ApplicationID != nil || domain.Target.PHPVersion != "" {
		return nil, ErrInvalidSpec
	}
	redirect := domain.Target.Redirect
	if redirect.StatusCode != core.RedirectPermanent && redirect.StatusCode != core.RedirectTemporary {
		return nil, ErrInvalidSpec
	}
	hostMode := redirect.HostMode
	if hostMode == "" {
		hostMode = core.RedirectHostBoth
	}
	if hostMode != core.RedirectHostApexOnly && hostMode != core.RedirectHostWWWOnly &&
		hostMode != core.RedirectHostBoth {
		return nil, ErrInvalidSpec
	}
	if redirect.WildcardSubdomains && hostMode != core.RedirectHostBoth {
		return nil, ErrInvalidSpec
	}
	normalized, host, _, err := core.NormalizeRedirectURL(redirect.TargetURL)
	if err != nil || normalized != redirect.TargetURL || host != redirect.TargetASCIIHost {
		return nil, ErrInvalidSpec
	}
	result := &preparedRedirect{
		statusCode: int64(redirect.StatusCode), target: normalized,
		hostMode:     hostMode,
		preservePath: redirect.PreservePath, preserveQuery: redirect.PreserveQuery,
		wildcard: redirect.WildcardSubdomains,
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, ErrInvalidSpec
	}
	if redirect.PreserveQuery && parsed.RawQuery != "" {
		result.requiresQueryMap = true
	}
	return result, nil
}

func assignQueryVariables(domains []preparedDomain) {
	next := 1
	for index := range domains {
		if domains[index].redirect == nil || !domains[index].redirect.requiresQueryMap {
			continue
		}
		domains[index].redirect.queryVariable = fmt.Sprintf("sf_redirect_query_%05d", next)
		next++
	}
}

func validateRouteConflicts(domains []preparedDomain) error {
	wildcardParents := make(map[string]struct{})
	for index, domain := range domains {
		if index > 0 && domains[index-1].base == domain.base {
			return ErrInvalidSpec
		}
		if domain.redirect != nil && domain.redirect.wildcard {
			wildcardParents[domain.base] = struct{}{}
		}
	}
	for _, domain := range domains {
		candidate := domain.base
		for {
			separator := strings.IndexByte(candidate, '.')
			if separator < 0 {
				break
			}
			candidate = candidate[separator+1:]
			if _, exists := wildcardParents[candidate]; exists {
				return ErrInvalidSpec
			}
		}
	}
	return nil
}

func writeQueryMap(output *bytes.Buffer, variable string) {
	output.WriteString("map $args $")
	output.WriteString(variable)
	output.WriteString(" {\n    \"\" \"\";\n    default \"&$args\";\n}\n\n")
}

func writeActivationMap(output *bytes.Buffer, variable, revisionID string) {
	// $request_uri remains immutable across try_files internal redirects. Using
	// $uri here would drop the probe token when a PHP-only account internally
	// redirects the missing probe path to index.php.
	output.WriteString("map $request_uri ")
	output.WriteString(variable)
	output.WriteString(" {\n    default \"\";\n    \"/.__stackfort_activation_probe__\" ")
	output.WriteString(quoteLiteral(revisionID))
	output.WriteString(";\n}\n\n")
}

func writeOCIUpstreamBlocks(output *bytes.Buffer, domains []preparedDomain) {
	seen := make(map[string]struct{})
	for _, item := range domains {
		if item.applicationID == "" {
			continue
		}
		if _, exists := seen[item.applicationID]; exists {
			continue
		}
		seen[item.applicationID] = struct{}{}
		output.WriteString("upstream ")
		output.WriteString(ociUpstreamName(item.applicationID))
		output.WriteString(" {\n    server 127.0.0.1:")
		output.WriteString(strconv.FormatInt(item.upstreamPort, 10))
		output.WriteString(";\n    keepalive 32;\n}\n\n")
	}
}

func ociUpstreamName(applicationID string) string {
	return "sf_app_" + strings.ReplaceAll(applicationID, "-", "")
}

func writeOCIApplicationDomain(output *bytes.Buffer, item preparedDomain, headers []renderedHeader) {
	canonical, alias := canonicalHosts(item.base, item.domain.CanonicalMode)
	if alias != "" {
		writeHTTPServerStart(output, []string{alias})
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.http01 {
			writeHTTP01Location(output, item.wafProfile != "")
		}
		if item.wafProfile != "" {
			writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
		} else {
			writeReturnLocation(output, 301, "https://"+canonical, "$request_uri")
		}
		output.WriteString("}\n\n")
		if item.certificateID != "" {
			writeHTTPSServerStart(output, []string{alias}, item.certificateID)
			writeDomainLogs(output, item)
			writeWAFPolicy(output, item)
			writeHeaders(output, headers, item.activationVariable)
			if item.wafProfile != "" {
				writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			} else {
				writeReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			}
			output.WriteString("}\n\n")
		}
	}
	hosts := []string{canonical}
	if item.domain.CanonicalMode == core.CanonicalServeBoth {
		hosts = []string{item.base, "www." + item.base}
	}
	if item.certificateID != "" {
		writeHTTPServerStart(output, hosts)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.http01 {
			writeHTTP01Location(output, item.wafProfile != "")
		}
		redirectHost, variables := "https://"+canonical, "$request_uri"
		if item.domain.CanonicalMode == core.CanonicalServeBoth {
			redirectHost, variables = "https://", "$host$request_uri"
		}
		if item.wafProfile != "" {
			writeWAFReturnLocation(output, 301, redirectHost, variables)
		} else {
			writeReturnLocation(output, 301, redirectHost, variables)
		}
		output.WriteString("}\n\n")
		writeHTTPSServerStart(output, hosts, item.certificateID)
	} else {
		writeHTTPServerStart(output, hosts)
	}
	writeDomainLogs(output, item)
	writeWAFPolicy(output, item)
	writeHeaders(output, headers, item.activationVariable)
	if item.http01 && item.certificateID == "" {
		writeHTTP01Location(output, item.wafProfile != "")
	}
	output.WriteString("\n    location / {\n        proxy_http_version 1.1;\n")
	output.WriteString("        proxy_set_header Connection \"\";\n        proxy_set_header Host $host;\n")
	output.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
	output.WriteString("        proxy_set_header X-Forwarded-For $remote_addr;\n")
	output.WriteString("        proxy_set_header X-Forwarded-Host $host;\n")
	output.WriteString("        proxy_connect_timeout 2s;\n        proxy_send_timeout 60s;\n        proxy_read_timeout 60s;\n")
	output.WriteString("        proxy_pass http://")
	output.WriteString(ociUpstreamName(item.applicationID))
	output.WriteString(";\n    }\n}\n\n")
}

func writeStaticDomain(output *bytes.Buffer, item preparedDomain, headers []renderedHeader) {
	canonical, alias := canonicalHosts(item.base, item.domain.CanonicalMode)
	if alias != "" {
		writeHTTPServerStart(output, []string{alias})
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.http01 {
			writeHTTP01Location(output, item.wafProfile != "")
			if item.wafProfile != "" {
				writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			} else {
				writeReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			}
		} else if item.wafProfile != "" {
			writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
		} else {
			output.WriteString("    return 301 ")
			output.WriteString(quoteDynamic("https://"+canonical, "$request_uri"))
			output.WriteString(";\n")
		}
		output.WriteString("}\n\n")
		if item.certificateID != "" {
			writeHTTPSServerStart(output, []string{alias}, item.certificateID)
			writeDomainLogs(output, item)
			writeWAFPolicy(output, item)
			writeHeaders(output, headers, item.activationVariable)
			if item.wafProfile != "" {
				writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			} else {
				output.WriteString("    return 301 ")
				output.WriteString(quoteDynamic("https://"+canonical, "$request_uri"))
				output.WriteString(";\n")
			}
			output.WriteString("}\n\n")
		}
	}
	hosts := []string{canonical}
	if item.domain.CanonicalMode == core.CanonicalServeBoth {
		hosts = []string{item.base, "www." + item.base}
	}
	if item.certificateID != "" {
		writeHTTPServerStart(output, hosts)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.http01 {
			writeHTTP01Location(output, item.wafProfile != "")
		}
		redirectHost := "https://" + canonical
		redirectVariables := "$request_uri"
		if item.domain.CanonicalMode == core.CanonicalServeBoth {
			redirectHost = "https://"
			redirectVariables = "$host$request_uri"
		}
		if item.wafProfile != "" {
			writeWAFReturnLocation(output, 301, redirectHost, redirectVariables)
		} else {
			writeReturnLocation(output, 301, redirectHost, redirectVariables)
		}
		output.WriteString("}\n\n")
		writeHTTPSServerStart(output, hosts, item.certificateID)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
	} else {
		writeHTTPServerStart(output, hosts)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
	}
	output.WriteString("    root ")
	output.WriteString(quoteLiteral(item.documentRoot))
	output.WriteString(";\n    disable_symlinks on from=$document_root;\n")
	if item.phpSocket == "" {
		output.WriteString("    index index.html;\n")
	} else {
		output.WriteString("    index index.php index.html;\n")
	}
	writeHeaders(output, headers, item.activationVariable)
	if item.http01 && item.certificateID == "" {
		writeHTTP01Location(output, item.wafProfile != "")
	}
	if item.phpSocket == "" {
		output.WriteString("\n    location / {\n        try_files $uri $uri/ =404;\n    }\n}\n\n")
		return
	}
	output.WriteString("\n    location / {\n        try_files $uri $uri/ /index.php?$query_string;\n    }\n")
	output.WriteString("\n    location ~ \\.php$ {\n        try_files $uri =404;\n")
	output.WriteString("        include /etc/nginx/fastcgi_params;\n")
	output.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	output.WriteString("        fastcgi_param HTTP_PROXY \"\";\n        fastcgi_pass ")
	output.WriteString(quoteLiteral("unix:" + item.phpSocket))
	output.WriteString(";\n    }\n}\n\n")
}

func writeCachedDomain(output *bytes.Buffer, item preparedDomain, headers []renderedHeader) {
	canonical, alias := canonicalHosts(item.base, item.domain.CanonicalMode)
	if alias != "" {
		writeHTTPServerStart(output, []string{alias})
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.http01 {
			writeHTTP01Location(output, item.wafProfile != "")
		}
		if item.wafProfile != "" {
			writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
		} else {
			writeReturnLocation(output, 301, "https://"+canonical, "$request_uri")
		}
		output.WriteString("}\n\n")
		if item.certificateID != "" {
			writeHTTPSServerStart(output, []string{alias}, item.certificateID)
			writeDomainLogs(output, item)
			writeWAFPolicy(output, item)
			writeHeaders(output, headers, item.activationVariable)
			if item.wafProfile != "" {
				writeWAFReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			} else {
				writeReturnLocation(output, 301, "https://"+canonical, "$request_uri")
			}
			output.WriteString("}\n\n")
		}
	}
	hosts := []string{canonical}
	if item.domain.CanonicalMode == core.CanonicalServeBoth {
		hosts = []string{item.base, "www." + item.base}
	}
	if item.certificateID != "" {
		writeHTTPServerStart(output, hosts)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.http01 {
			writeHTTP01Location(output, item.wafProfile != "")
		}
		redirectHost, variables := "https://"+canonical, "$request_uri"
		if item.domain.CanonicalMode == core.CanonicalServeBoth {
			redirectHost, variables = "https://", "$host$request_uri"
		}
		if item.wafProfile != "" {
			writeWAFReturnLocation(output, 301, redirectHost, variables)
		} else {
			writeReturnLocation(output, 301, redirectHost, variables)
		}
		output.WriteString("}\n\n")
		writeHTTPSServerStart(output, hosts, item.certificateID)
	} else {
		writeHTTPServerStart(output, hosts)
	}
	writeDomainLogs(output, item)
	writeWAFPolicy(output, item)
	writeHeaders(output, headers, item.activationVariable)
	if item.http01 && item.certificateID == "" {
		writeHTTP01Location(output, item.wafProfile != "")
	}
	writeVinylProxyLocation(output, item.cachePreset)
	output.WriteString("}\n\n")
	writeCachedOrigin(output, item)
}

func writeVinylProxyLocation(output *bytes.Buffer, preset core.CachePreset) {
	output.WriteString("\n    location / {\n")
	output.WriteString("        proxy_http_version 1.1;\n        proxy_set_header Connection \"\";\n")
	output.WriteString("        proxy_set_header Host $host;\n        proxy_set_header Authorization $http_authorization;\n")
	output.WriteString("        proxy_set_header Cookie $http_cookie;\n        proxy_set_header X-Forwarded-Proto $scheme;\n")
	output.WriteString("        proxy_set_header X-Forwarded-For $remote_addr;\n        proxy_set_header X-Stackfort-Cache-Preset ")
	output.WriteString(quoteLiteral(string(preset)))
	output.WriteString(";\n        proxy_pass http://")
	output.WriteString(cacheconfig.CacheAddress)
	output.WriteString(";\n    }\n")
}

func writeCachedOrigin(output *bytes.Buffer, item preparedDomain) {
	output.WriteString("server {\n    listen ")
	output.WriteString(cacheconfig.OriginAddress)
	output.WriteString(";\n    server_name ")
	output.WriteString(item.base)
	output.WriteString(" www.")
	output.WriteString(item.base)
	output.WriteString(";\n    access_log off;\n    error_log ")
	accountID := string(item.domain.AccountID)
	output.WriteString(quoteLiteral(hostinglogs.DomainFile(accountID, item.base, "error")))
	output.WriteString(" warn;\n    root ")
	output.WriteString(quoteLiteral(item.documentRoot))
	output.WriteString(";\n    disable_symlinks on from=$document_root;\n    index index.php index.html;\n")
	output.WriteString("\n    location / {\n        try_files $uri $uri/ /index.php?$query_string;\n    }\n")
	output.WriteString("\n    location ~ \\.php$ {\n        try_files $uri =404;\n")
	output.WriteString("        include /etc/nginx/fastcgi_params;\n")
	output.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	output.WriteString("        fastcgi_param HTTP_PROXY \"\";\n        fastcgi_pass ")
	output.WriteString(quoteLiteral("unix:" + item.phpSocket))
	output.WriteString(";\n    }\n}\n\n")
}

func writeRedirectDomain(output *bytes.Buffer, item preparedDomain, headers []renderedHeader) {
	hosts, inactiveHosts := redirectHosts(item.base, item.redirect.hostMode)
	if item.redirect.wildcard {
		hosts = append(hosts, "*."+item.base)
	}
	writeHTTPServerStart(output, hosts)
	writeDomainLogs(output, item)
	writeWAFPolicy(output, item)
	writeHeaders(output, headers, item.activationVariable)
	value := quoteRedirectValue(*item.redirect)
	if item.http01 {
		writeHTTP01Location(output, item.wafProfile != "")
		if item.wafProfile != "" {
			writeWAFQuotedReturnLocation(output, item.redirect.statusCode, value)
		} else {
			writeQuotedReturnLocation(output, item.redirect.statusCode, value)
		}
	} else if item.wafProfile != "" {
		writeWAFQuotedReturnLocation(output, item.redirect.statusCode, value)
	} else {
		output.WriteString("    return ")
		output.WriteString(fmt.Sprintf("%d ", item.redirect.statusCode))
		output.WriteString(value)
		output.WriteString(";\n")
	}
	output.WriteString("}\n\n")
	if item.certificateID != "" {
		writeHTTPSServerStart(output, hosts, item.certificateID)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		if item.wafProfile != "" {
			writeWAFQuotedReturnLocation(output, item.redirect.statusCode, value)
		} else {
			output.WriteString("    return ")
			output.WriteString(fmt.Sprintf("%d ", item.redirect.statusCode))
			output.WriteString(value)
			output.WriteString(";\n")
		}
		output.WriteString("}\n\n")
	}
	if len(inactiveHosts) != 0 {
		writeInactiveRedirectHosts(output, item, inactiveHosts, headers)
	}
}

func redirectHosts(base string, mode core.RedirectHostMode) (active, inactive []string) {
	switch mode {
	case core.RedirectHostApexOnly:
		return []string{base}, []string{"www." + base}
	case core.RedirectHostWWWOnly:
		return []string{"www." + base}, []string{base}
	default:
		return []string{base, "www." + base}, nil
	}
}

func writeInactiveRedirectHosts(
	output *bytes.Buffer,
	item preparedDomain,
	hosts []string,
	headers []renderedHeader,
) {
	writeHTTPServerStart(output, hosts)
	writeDomainLogs(output, item)
	writeWAFPolicy(output, item)
	writeHeaders(output, headers, item.activationVariable)
	if item.http01 {
		writeHTTP01Location(output, item.wafProfile != "")
	}
	writeNotFoundLocation(output)
	output.WriteString("}\n\n")
	if item.certificateID != "" {
		writeHTTPSServerStart(output, hosts, item.certificateID)
		writeDomainLogs(output, item)
		writeWAFPolicy(output, item)
		writeHeaders(output, headers, item.activationVariable)
		writeNotFoundLocation(output)
		output.WriteString("}\n\n")
	}
}

func writeDomainLogs(output *bytes.Buffer, item preparedDomain) {
	accountID := string(item.domain.AccountID)
	accessPath := hostinglogs.DomainFile(accountID, item.base, "access")
	errorPath := hostinglogs.DomainFile(accountID, item.base, "error")
	output.WriteString("    access_log ")
	output.WriteString(quoteLiteral(accessPath))
	output.WriteString(" ")
	output.WriteString(hostinglogs.FormatName)
	output.WriteString(";\n    error_log ")
	output.WriteString(quoteLiteral(errorPath))
	output.WriteString(" warn;\n")
}

func writeWAFPolicy(output *bytes.Buffer, item preparedDomain) {
	if item.wafProfile == "" {
		return
	}
	output.WriteString("    coraza on;\n    coraza_transaction_id $request_id;\n")
	if len(item.wafExceptions) == 0 {
		output.WriteString("    coraza_rules_file ")
		output.WriteString(quoteLiteral(item.wafProfile))
		output.WriteString(";\n")
		return
	}
	// coraza-nginx collects file and inline directives separately. Keep each
	// runtime ctl exclusion and the fixed profile Include in one parser input so
	// the exclusions are unambiguously registered before the matching CRS rule.
	output.WriteString("    coraza_rules '\n")
	for _, exception := range item.wafExceptions {
		writeWAFExceptionRules(output, exception)
	}
	output.WriteString("        Include ")
	output.WriteString(item.wafProfile)
	output.WriteString("\n    ';\n")
}

func writeWAFExceptionRules(output *bytes.Buffer, exception preparedWAFException) {
	requestPattern := `^/`
	if exception.requestPath != "" {
		requestPattern = `^` + regexp.QuoteMeta(exception.requestPath) + `(?:\?.*)?$`
	}
	action := "ctl:ruleRemoveById=" + strconv.FormatUint(uint64(exception.ruleID), 10)
	if exception.parameter != "" {
		action = "ctl:ruleRemoveTargetById=" + strconv.FormatUint(uint64(exception.ruleID), 10) + ";ARGS:" + exception.parameter
	}
	// Both statements live in one quoted directive so Coraza parses the chain
	// atomically. Every interpolated byte has passed the closed validators.
	output.WriteString("        SecRule TIME_EPOCH \"@lt ")
	output.WriteString(strconv.FormatInt(exception.expiresUnix, 10))
	output.WriteString("\" \"id:")
	output.WriteString(strconv.FormatUint(uint64(exception.localRuleID), 10))
	output.WriteString(",phase:1,pass,nolog,chain\"\n        SecRule REQUEST_URI \"@rx ")
	output.WriteString(requestPattern)
	output.WriteString("\" \"t:none,")
	output.WriteString(action)
	output.WriteString("\"\n")
}

func writeHTTP01Location(output *bytes.Buffer, disableWAF bool) {
	output.WriteString("\n    # Root-owned ACME HTTP-01 responses bypass site redirects and caches.\n")
	output.WriteString("    location ^~ /.well-known/acme-challenge/ {\n")
	if disableWAF {
		output.WriteString("        coraza off;\n")
	}
	output.WriteString("        alias ")
	output.WriteString(acmehttp01.ChallengeDirectory)
	output.WriteString("/;\n        default_type application/octet-stream;\n")
	output.WriteString("        add_header Cache-Control \"no-store\" always;\n")
	output.WriteString("        add_header X-Content-Type-Options \"nosniff\" always;\n")
	output.WriteString("        limit_except GET HEAD { deny all; }\n")
	output.WriteString("        autoindex off;\n        disable_symlinks on;\n    }\n")
}

func writeReturnLocation(output *bytes.Buffer, statusCode int64, literal, variables string) {
	writeQuotedReturnLocation(output, statusCode, quoteDynamic(literal, variables))
}

func writeWAFReturnLocation(output *bytes.Buffer, statusCode int64, literal, variables string) {
	writeWAFQuotedReturnLocation(output, statusCode, quoteDynamic(literal, variables))
}

// A rewrite-module return executes before Coraza's pre-access phase. WAF
// domains therefore reach their redirect through try_files' pre-content phase;
// attacks can be denied first, while the named location keeps the final target
// as a bounded renderer-owned return.
func writeWAFQuotedReturnLocation(output *bytes.Buffer, statusCode int64, value string) {
	output.WriteString("\n    location / {\n        try_files /.__stackfort_waf_redirect_never_exists__ @stackfort_waf_redirect;\n    }\n")
	output.WriteString("\n    location @stackfort_waf_redirect {\n        return ")
	output.WriteString(fmt.Sprintf("%d ", statusCode))
	output.WriteString(value)
	output.WriteString(";\n    }\n")
}

func writeQuotedReturnLocation(output *bytes.Buffer, statusCode int64, value string) {
	output.WriteString("\n    location / {\n        return ")
	output.WriteString(fmt.Sprintf("%d ", statusCode))
	output.WriteString(value)
	output.WriteString(";\n    }\n")
}

func writeNotFoundLocation(output *bytes.Buffer) {
	output.WriteString("\n    location / {\n        return 404;\n    }\n")
}

func canonicalHosts(base string, mode core.CanonicalMode) (canonical, alias string) {
	switch mode {
	case core.CanonicalPreferWWW:
		return "www." + base, base
	case core.CanonicalServeBoth:
		return base, ""
	default:
		return base, "www." + base
	}
}

func quoteRedirectValue(redirect preparedRedirect) string {
	parsed, _ := url.Parse(redirect.target)
	rawQuery := parsed.RawQuery
	parsed.RawQuery = ""
	var result strings.Builder
	result.Grow(len(redirect.target) + 48)
	result.WriteByte('"')
	writeQuotedContent(&result, parsed.String(), true)
	if redirect.preservePath {
		result.WriteString("$uri")
	}
	if rawQuery != "" {
		writeQuotedContent(&result, "?"+rawQuery, true)
		if redirect.preserveQuery {
			result.WriteString("$")
			result.WriteString(redirect.queryVariable)
		}
	} else if redirect.preserveQuery {
		result.WriteString("$is_args$args")
	}
	result.WriteByte('"')
	return result.String()
}

func writeHTTPServerStart(output *bytes.Buffer, hosts []string) {
	output.WriteString("server {\n    listen 80;\n    listen [::]:80;\n    server_name ")
	output.WriteString(strings.Join(hosts, " "))
	output.WriteString(";\n")
}

func writeHTTPSServerStart(output *bytes.Buffer, hosts []string, certificateID string) {
	certificatePath, _ := tlsartifact.CertificatePath(certificateID)
	privateKeyPath, _ := tlsartifact.PrivateKeyPath(certificateID)
	output.WriteString("server {\n    listen 443 ssl;\n    listen [::]:443 ssl;\n    server_name ")
	output.WriteString(strings.Join(hosts, " "))
	output.WriteString(";\n    ssl_certificate ")
	output.WriteString(quoteLiteral(certificatePath))
	output.WriteString(";\n    ssl_certificate_key ")
	output.WriteString(quoteLiteral(privateKeyPath))
	output.WriteString(";\n    ssl_protocols TLSv1.2 TLSv1.3;\n    ssl_session_tickets off;\n")
}

func writeHeaders(output *bytes.Buffer, headers []renderedHeader, activationVariable string) {
	for _, header := range headers {
		output.WriteString("    add_header ")
		output.WriteString(header.name)
		output.WriteByte(' ')
		output.WriteString(quoteLiteral(header.value))
		output.WriteString(" always;\n")
	}
	if activationVariable != "" {
		output.WriteString("    add_header X-Stackfort-Activation ")
		output.WriteString(activationVariable)
		output.WriteString(" always;\n")
	}
}

func quoteLiteral(value string) string { return quote(value, "", false) }

func quoteDynamic(literal, trustedVariables string) string {
	return quote(literal, trustedVariables, true)
}

func quote(literal, trustedVariables string, encodeURLDollar bool) string {
	var result strings.Builder
	result.Grow(len(literal) + len(trustedVariables) + 2)
	result.WriteByte('"')
	writeQuotedContent(&result, literal, encodeURLDollar)
	result.WriteString(trustedVariables)
	result.WriteByte('"')
	return result.String()
}

func writeQuotedContent(result *strings.Builder, literal string, encodeURLDollar bool) {
	for _, character := range literal {
		switch character {
		case '$':
			if encodeURLDollar {
				// The NGINX configuration parser removes a backslash before the
				// rewrite module compiles variables. URL-encode an untrusted literal
				// dollar instead; only renderer-owned variables are appended below.
				result.WriteString("%24")
				continue
			}
		case '\\', '"':
			result.WriteByte('\\')
		}
		result.WriteRune(character)
	}
}
