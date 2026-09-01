// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

const (
	DomainLifecycleKind          = "domain.lifecycle.apply"
	domainLifecycleSchemaVersion = 5
	domainLifecycleMinimumSchema = 2
)

type DomainLifecycleAction string

const (
	DomainLifecycleCreate             DomainLifecycleAction = "create"
	DomainLifecycleEdit               DomainLifecycleAction = "edit"
	DomainLifecycleSuspend            DomainLifecycleAction = "suspend"
	DomainLifecycleResume             DomainLifecycleAction = "resume"
	DomainLifecycleRemove             DomainLifecycleAction = "remove"
	DomainLifecycleCreateWAFException DomainLifecycleAction = "waf_exception_create"
	DomainLifecycleRemoveWAFException DomainLifecycleAction = "waf_exception_remove"
)

type WAFExceptionIntent struct {
	RuleID      uint32    `json:"ruleId"`
	RequestPath string    `json:"requestPath,omitempty"`
	Parameter   string    `json:"parameter,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// DomainLifecyclePayload contains only typed domain intent. For creates, the
// durable operation ID is also the stable domain ID, so a retry cannot create
// a second row.
type DomainLifecyclePayload struct {
	SchemaVersion  int                    `json:"schemaVersion"`
	Action         DomainLifecycleAction  `json:"action"`
	DomainID       string                 `json:"domainId,omitempty"`
	Name           string                 `json:"name,omitempty"`
	CanonicalMode  *core.CanonicalMode    `json:"canonicalMode,omitempty"`
	Target         *core.DomainTargetSpec `json:"target,omitempty"`
	DisableTLS     bool                   `json:"disableTls,omitempty"`
	TLSMode        core.TLSMode           `json:"tlsMode,omitempty"`
	WAFMode        *core.WAFMode          `json:"wafMode,omitempty"`
	WAFException   *WAFExceptionIntent    `json:"wafException,omitempty"`
	WAFExceptionID string                 `json:"wafExceptionId,omitempty"`
	CachePreset    *core.CachePreset      `json:"cachePreset,omitempty"`
}

type domainDesiredStateDocument struct {
	SchemaVersion  int                                `json:"schemaVersion"`
	Domains        []nginxconfig.DomainSpec           `json:"domains"`
	Options        nginxconfig.Options                `json:"options"`
	DocumentRoots  []domainDocumentRootIntent         `json:"documentRoots"`
	PHPVersions    []string                           `json:"phpVersions"`
	PHPMaxChildren uint32                             `json:"phpMaxChildren"`
	PHPMemoryMiB   uint32                             `json:"phpMemoryMib"`
	Pending        []core.DomainActivationExpectation `json:"pending"`
}

type domainDocumentRootIntent struct {
	RelativePath string                           `json:"relativePath"`
	Access       agentprotocol.DocumentRootAccess `json:"access"`
}

type DomainLifecycleRepository interface {
	NGINXActivationRepository
	CreateDomain(context.Context, core.CreateDomainParams) (core.Domain, error)
	UpdateDomain(context.Context, core.UpdateDomainParams) (core.Domain, error)
	SuspendDomain(context.Context, core.ChangeDomainStatusParams) (core.Domain, error)
	ResumeDomain(context.Context, core.ChangeDomainStatusParams) (core.Domain, error)
	RemoveDomain(context.Context, core.RemoveDomainParams) error
	CreateDomainWAFException(context.Context, core.CreateDomainWAFExceptionParams) (core.DomainWAFException, error)
	RemoveDomainWAFException(context.Context, core.RemoveDomainWAFExceptionParams) error
	ListDomains(context.Context, core.ID, bool) ([]core.Domain, error)
	CreateDesiredStateRevision(context.Context, core.CreateDesiredStateRevisionParams) (core.DesiredStateRevision, error)
	DesiredStateRevisionForOperation(context.Context, core.ID, core.ID) (core.DesiredStateRevision, error)
	LatestDesiredStateRevision(context.Context, core.ID) (core.DesiredStateRevision, error)
	ConfirmDomainActivation(context.Context, core.ConfirmDomainActivationParams) (int64, error)
}

type DomainLifecycleClient interface {
	NGINXActivationClient
	EnsureHostingDocumentRoot(
		context.Context,
		string,
		agentprotocol.AuditCorrelation,
		hostingidentity.Spec,
		string,
		agentprotocol.DocumentRootAccess,
	) (agentprotocol.DocumentRootResponse, error)
	ReconcilePHPPools(
		context.Context,
		string,
		agentprotocol.AuditCorrelation,
		phpruntime.PoolSetSpec,
	) (agentprotocol.PHPPoolSetResponse, error)
}

// DomainLifecycleHandler executes a replay-safe saga. The database mutation
// and desired-state capture are individually transactional; every later host
// step is derived from the captured document rather than mutable domain rows.
type DomainLifecycleHandler struct {
	repository DomainLifecycleRepository
	client     DomainLifecycleClient
	nginx      *NGINXActivationHandler
}

func NewDomainLifecycleHandler(
	repository DomainLifecycleRepository,
	client DomainLifecycleClient,
) (*DomainLifecycleHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("domain lifecycle handler requires a repository and agent client")
	}
	nginx, err := NewNGINXActivationHandler(repository, client)
	if err != nil {
		return nil, err
	}
	return &DomainLifecycleHandler{repository: repository, client: client, nginx: nginx}, nil
}

func NewDomainLifecyclePayload(payload DomainLifecyclePayload) (map[string]any, error) {
	if payload.SchemaVersion == 0 {
		payload.SchemaVersion = domainLifecycleSchemaVersion
	}
	if err := validateDomainLifecyclePayload(payload); err != nil {
		return nil, err
	}
	return structToObject(payload)
}

func (handler *DomainLifecycleHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != DomainLifecycleKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "domain.lifecycle_operation_invalid"}
	}
	payload, err := decodeDomainLifecyclePayload(operation.Payload)
	if err != nil || validateDomainLifecyclePayload(payload) != nil {
		return nil, &Failure{Code: "domain.lifecycle_payload_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "mutating", 2, "domain.lifecycle.mutating", map[string]any{
		"action": payload.Action,
	}); err != nil {
		return nil, err
	}
	domainID, err := handler.applyMutation(ctx, operation, payload)
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}

	revision, err := handler.desiredRevision(ctx, operation, payload.Action)
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}
	document, err := decodeDomainDesiredStateDocument(revision.Document)
	if err != nil {
		return nil, &Failure{Code: "domain.desired_state_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "preparing", 5, "domain.lifecycle.preparing", map[string]any{
		"documentRoots": len(document.DocumentRoots), "domains": len(document.Domains),
	}); err != nil {
		return nil, err
	}

	account, err := handler.repository.GetHostingAccount(ctx, *operation.AccountID)
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil || identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "domain.host_identity_invalid"}
	}
	correlation := lifecycleCorrelation(operation)
	for index, root := range document.DocumentRoots {
		response, ensureErr := handler.client.EnsureHostingDocumentRoot(
			ctx,
			fmt.Sprintf("domain-root-%s-%d", operation.ID, index),
			correlation,
			identity,
			root.RelativePath,
			root.Access,
		)
		if ensureErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, classifyDomainFilesystemFailure(ensureErr)
		}
		if response.RelativePath != root.RelativePath {
			return nil, &Failure{Code: "domain.document_root_response_invalid", Retryable: true}
		}
	}
	latest, err := handler.repository.LatestDesiredStateRevision(ctx, *operation.AccountID)
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}
	if latest.ID != revision.ID {
		return nil, &Failure{Code: "domain.lifecycle_superseded"}
	}
	poolSpec := phpruntime.PoolSetSpec{
		Identity: identity, Versions: append([]string(nil), document.PHPVersions...),
		MaxChildren: document.PHPMaxChildren, MemoryLimitMiB: document.PHPMemoryMiB,
	}
	if len(poolSpec.Versions) > 0 {
		if err := reporter.Checkpoint(ctx, "preparing-php", 8, "domain.lifecycle.preparing_php", map[string]any{
			"versions": len(poolSpec.Versions),
		}); err != nil {
			return nil, err
		}
		response, reconcileErr := handler.client.ReconcilePHPPools(
			ctx, "domain-php-prepare-"+string(operation.ID), correlation, poolSpec,
		)
		if reconcileErr != nil {
			return nil, classifyDomainPHPFailure(reconcileErr)
		}
		if !response.Active || !slices.Equal(response.Versions, poolSpec.Versions) {
			return nil, &Failure{Code: "domain.php_pool_response_invalid", Retryable: true}
		}
	}

	activation, err := handler.nginx.runPayload(ctx, operation, reporter, NGINXActivationPayload{
		DesiredStateRevisionID: string(revision.ID), Domains: document.Domains, Options: document.Options,
	})
	if err != nil {
		return nil, err
	}
	if err := reporter.Checkpoint(ctx, "converging-php", 92, "domain.lifecycle.converging_php", map[string]any{
		"versions": len(poolSpec.Versions),
	}); err != nil {
		return nil, err
	}
	poolSpec.RetireAbsent = true
	poolResponse, err := handler.client.ReconcilePHPPools(
		ctx, "domain-php-converge-"+string(operation.ID), correlation, poolSpec,
	)
	if err != nil {
		return nil, classifyDomainPHPFailure(err)
	}
	if !poolResponse.Active || !slices.Equal(poolResponse.Versions, poolSpec.Versions) {
		return nil, &Failure{Code: "domain.php_pool_response_invalid", Retryable: true}
	}
	activated, err := handler.repository.ConfirmDomainActivation(ctx, core.ConfirmDomainActivationParams{
		AccountID: *operation.AccountID, DesiredStateRevisionID: revision.ID,
		OperationID: operation.ID, Expected: document.Pending,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}
	activation["action"] = string(payload.Action)
	activation["activatedDomains"] = activated
	activation["domainId"] = string(domainID)
	return activation, nil
}

func (handler *DomainLifecycleHandler) applyMutation(
	ctx context.Context,
	operation core.Operation,
	payload DomainLifecyclePayload,
) (core.ID, error) {
	accountID := *operation.AccountID
	domainID := operation.ID
	if payload.Action != DomainLifecycleCreate {
		var err error
		domainID, err = core.ParseID(payload.DomainID)
		if err != nil {
			return "", core.ErrInvalidInput
		}
	}
	common := core.ChangeDomainStatusParams{
		AccountID: accountID, DomainID: domainID, OperationID: &operation.ID,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	}
	switch payload.Action {
	case DomainLifecycleCreate:
		wafMode := core.WAFMode("")
		if payload.WAFMode != nil {
			wafMode = *payload.WAFMode
		}
		canonical := core.CanonicalMode("")
		if payload.CanonicalMode != nil {
			canonical = *payload.CanonicalMode
		}
		_, err := handler.repository.CreateDomain(ctx, core.CreateDomainParams{
			AccountID: accountID, DomainID: &domainID, Name: payload.Name,
			CanonicalMode: canonical, Target: *payload.Target,
			DisableTLS: payload.DisableTLS, TLSMode: payload.TLSMode,
			WAFMode:     wafMode,
			CachePreset: cachePresetValue(payload.CachePreset),
			OperationID: &operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
		})
		return domainID, err
	case DomainLifecycleEdit:
		_, err := handler.repository.UpdateDomain(ctx, core.UpdateDomainParams{
			AccountID: accountID, DomainID: domainID, CanonicalMode: payload.CanonicalMode,
			Target: payload.Target, WAFMode: payload.WAFMode, CachePreset: payload.CachePreset, OperationID: &operation.ID,
			ActorID: operation.ActorID, RequestID: operation.RequestID,
		})
		return domainID, err
	case DomainLifecycleSuspend:
		_, err := handler.repository.SuspendDomain(ctx, common)
		return domainID, err
	case DomainLifecycleResume:
		_, err := handler.repository.ResumeDomain(ctx, common)
		return domainID, err
	case DomainLifecycleRemove:
		return domainID, handler.repository.RemoveDomain(ctx, core.RemoveDomainParams{
			AccountID: accountID, DomainID: domainID, OperationID: &operation.ID,
			ActorID: operation.ActorID, RequestID: operation.RequestID,
		})
	case DomainLifecycleCreateWAFException:
		_, err := handler.repository.CreateDomainWAFException(ctx, core.CreateDomainWAFExceptionParams{
			AccountID: accountID, DomainID: domainID, ExceptionID: operation.ID,
			RuleID: payload.WAFException.RuleID, RequestPath: payload.WAFException.RequestPath,
			Parameter: payload.WAFException.Parameter, ExpiresAt: payload.WAFException.ExpiresAt,
			OperationID: operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
		})
		return domainID, err
	case DomainLifecycleRemoveWAFException:
		exceptionID, err := core.ParseID(payload.WAFExceptionID)
		if err != nil {
			return "", core.ErrInvalidInput
		}
		return domainID, handler.repository.RemoveDomainWAFException(ctx, core.RemoveDomainWAFExceptionParams{
			AccountID: accountID, DomainID: domainID, ExceptionID: exceptionID,
			OperationID: operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
		})
	default:
		return "", core.ErrInvalidInput
	}
}

func cachePresetValue(value *core.CachePreset) core.CachePreset {
	if value == nil {
		return core.CachePresetDisabled
	}
	return *value
}

func (handler *DomainLifecycleHandler) desiredRevision(
	ctx context.Context,
	operation core.Operation,
	action DomainLifecycleAction,
) (core.DesiredStateRevision, error) {
	accountID := *operation.AccountID
	existing, err := handler.repository.DesiredStateRevisionForOperation(ctx, accountID, operation.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, core.ErrNotFound) {
		return core.DesiredStateRevision{}, err
	}
	account, err := handler.repository.GetHostingAccount(ctx, accountID)
	if err != nil {
		return core.DesiredStateRevision{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return core.DesiredStateRevision{}, fmt.Errorf("%w: invalid hosting identity", core.ErrConflict)
	}
	domains, err := handler.repository.ListDomains(ctx, accountID, false)
	if err != nil {
		return core.DesiredStateRevision{}, err
	}
	upstreams := []core.OCIApplicationUpstream{}
	if provider, ok := handler.repository.(interface {
		ListOCIApplicationUpstreams(context.Context, core.ID) ([]core.OCIApplicationUpstream, error)
	}); ok {
		upstreams, err = provider.ListOCIApplicationUpstreams(ctx, accountID)
		if err != nil {
			return core.DesiredStateRevision{}, err
		}
	}
	document, err := makeDomainDesiredStateDocument(identity, domains, upstreams)
	if err != nil {
		return core.DesiredStateRevision{}, fmt.Errorf("%w: unsupported domain state", core.ErrConflict)
	}
	object, err := structToObject(document)
	if err != nil {
		return core.DesiredStateRevision{}, err
	}
	return handler.repository.CreateDesiredStateRevision(ctx, core.CreateDesiredStateRevisionParams{
		AccountID: accountID, Document: object, Reason: "domain.lifecycle." + string(action),
		OperationID: &operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
}

func makeDomainDesiredStateDocument(
	identity hostingidentity.Spec,
	domains []core.Domain,
	upstreams []core.OCIApplicationUpstream,
) (domainDesiredStateDocument, error) {
	options := nginxconfig.DefaultOptions()
	options.OCIUpstreams = append([]core.OCIApplicationUpstream(nil), upstreams...)
	specs, err := nginxconfig.SpecsFromDomains(identity, domains, options)
	if err != nil {
		return domainDesiredStateDocument{}, err
	}
	rootAccess := make(map[string]agentprotocol.DocumentRootAccess, len(specs))
	phpVersions := make([]string, 0, len(specs))
	pending := make([]core.DomainActivationExpectation, 0, len(specs))
	for _, domain := range domains {
		if domain.Status == core.DomainSuspended || domain.Status == core.DomainRemoved {
			continue
		}
		if domain.Status == core.DomainPending {
			pending = append(pending, core.DomainActivationExpectation{
				DomainID: domain.ID, TargetID: domain.Target.ID,
			})
		}
		if (domain.Target.Type == core.DomainTargetStatic || domain.Target.Type == core.DomainTargetPHP) &&
			domain.Target.DocumentRoot != nil {
			access := agentprotocol.DocumentRootAccessStatic
			if domain.Target.Type == core.DomainTargetPHP {
				access = agentprotocol.DocumentRootAccessPHP
			}
			path := domain.Target.DocumentRoot.RelativePath
			if existing, found := rootAccess[path]; !found || existing == agentprotocol.DocumentRootAccessStatic {
				rootAccess[path] = access
			}
		}
		if domain.Target.Type == core.DomainTargetPHP {
			phpVersions = append(phpVersions, domain.Target.PHPVersion)
		}
	}
	roots := make([]domainDocumentRootIntent, 0, len(rootAccess))
	for relativePath, access := range rootAccess {
		roots = append(roots, domainDocumentRootIntent{RelativePath: relativePath, Access: access})
	}
	slices.SortFunc(roots, func(a, b domainDocumentRootIntent) int {
		return stringCompare(a.RelativePath, b.RelativePath)
	})
	slices.Sort(phpVersions)
	phpVersions = slices.Compact(phpVersions)
	slices.SortFunc(pending, func(a, b core.DomainActivationExpectation) int {
		return stringCompare(string(a.DomainID), string(b.DomainID))
	})
	return domainDesiredStateDocument{
		SchemaVersion: domainLifecycleSchemaVersion, Domains: specs,
		Options: options, DocumentRoots: roots, PHPVersions: phpVersions,
		PHPMaxChildren: phpruntime.DefaultMaxChildren, PHPMemoryMiB: phpruntime.DefaultMemoryMiB,
		Pending: pending,
	}, nil
}

func decodeDomainLifecyclePayload(value map[string]any) (DomainLifecyclePayload, error) {
	var payload DomainLifecyclePayload
	return payload, decodeStrictObject(value, &payload)
}

func decodeDomainDesiredStateDocument(value map[string]any) (domainDesiredStateDocument, error) {
	var document domainDesiredStateDocument
	err := decodeStrictObject(value, &document)
	if err != nil || document.SchemaVersion < domainLifecycleMinimumSchema ||
		document.SchemaVersion > domainLifecycleSchemaVersion ||
		document.Domains == nil || document.DocumentRoots == nil || document.PHPVersions == nil || document.Pending == nil {
		return domainDesiredStateDocument{}, errors.New("invalid domain desired-state document")
	}
	if !slices.IsSortedFunc(document.DocumentRoots, func(a, b domainDocumentRootIntent) int {
		return stringCompare(a.RelativePath, b.RelativePath)
	}) || !slices.IsSorted(document.PHPVersions) ||
		document.PHPMaxChildren < 1 || document.PHPMaxChildren > phpruntime.MaximumMaxChildren ||
		document.PHPMemoryMiB < phpruntime.MinimumMemoryMiB || document.PHPMemoryMiB > phpruntime.MaximumMemoryMiB {
		return domainDesiredStateDocument{}, errors.New("document roots are not canonical")
	}
	for index, root := range document.DocumentRoots {
		normalized, err := hostingpath.NormalizeDocumentRoot(root.RelativePath)
		if err != nil || normalized != root.RelativePath ||
			agentprotocol.ValidateDocumentRootAccess(root.Access) != nil {
			return domainDesiredStateDocument{}, errors.New("document root is invalid")
		}
		if index > 0 && root.RelativePath == document.DocumentRoots[index-1].RelativePath {
			return domainDesiredStateDocument{}, errors.New("duplicate document root")
		}
	}
	for index, version := range document.PHPVersions {
		if phpruntime.ValidateVersion(version) != nil || index > 0 && version == document.PHPVersions[index-1] {
			return domainDesiredStateDocument{}, errors.New("PHP versions are not canonical")
		}
	}
	return document, nil
}

func validateDomainLifecyclePayload(payload DomainLifecyclePayload) error {
	if payload.SchemaVersion < domainLifecycleMinimumSchema || payload.SchemaVersion > domainLifecycleSchemaVersion {
		return errors.New("unsupported domain lifecycle schema")
	}
	if payload.SchemaVersion < 3 && payload.WAFMode != nil {
		return errors.New("WAF policy requires domain lifecycle schema 3")
	}
	if payload.SchemaVersion < 4 && (payload.WAFException != nil || payload.WAFExceptionID != "" ||
		payload.Action == DomainLifecycleCreateWAFException || payload.Action == DomainLifecycleRemoveWAFException) {
		return errors.New("WAF exceptions require domain lifecycle schema 4")
	}
	if payload.SchemaVersion < 5 && payload.CachePreset != nil {
		return errors.New("cache preset requires domain lifecycle schema 5")
	}
	if payload.CachePreset != nil {
		if _, err := cacheconfig.NormalizePreset(*payload.CachePreset); err != nil {
			return errors.New("invalid cache preset")
		}
	}
	if payload.WAFMode != nil && *payload.WAFMode != core.WAFModeOff &&
		*payload.WAFMode != core.WAFModeDetectionOnly && *payload.WAFMode != core.WAFModeBlockingPL1 {
		return errors.New("invalid WAF mode")
	}
	switch payload.Action {
	case DomainLifecycleCreate:
		if payload.DomainID != "" || payload.Name == "" || payload.Target == nil ||
			payload.WAFException != nil || payload.WAFExceptionID != "" {
			return errors.New("invalid domain create payload")
		}
	case DomainLifecycleEdit:
		if payload.DomainID == "" || payload.Name != "" || payload.DisableTLS || payload.TLSMode != "" ||
			(payload.CanonicalMode == nil && payload.Target == nil && payload.WAFMode == nil && payload.CachePreset == nil) ||
			payload.WAFException != nil || payload.WAFExceptionID != "" {
			return errors.New("invalid domain edit payload")
		}
	case DomainLifecycleSuspend, DomainLifecycleResume, DomainLifecycleRemove:
		if payload.DomainID == "" || payload.Name != "" || payload.CanonicalMode != nil || payload.Target != nil ||
			payload.DisableTLS || payload.TLSMode != "" || payload.WAFMode != nil || payload.CachePreset != nil ||
			payload.WAFException != nil || payload.WAFExceptionID != "" {
			return errors.New("invalid domain status payload")
		}
	case DomainLifecycleCreateWAFException:
		if payload.DomainID == "" || payload.Name != "" || payload.CanonicalMode != nil || payload.Target != nil ||
			payload.DisableTLS || payload.TLSMode != "" || payload.WAFMode != nil || payload.CachePreset != nil ||
			payload.WAFException == nil || payload.WAFExceptionID != "" || payload.WAFException.ExpiresAt.IsZero() ||
			wafconfig.ValidateExceptionScope(payload.WAFException.RuleID, payload.WAFException.RequestPath, payload.WAFException.Parameter) != nil {
			return errors.New("invalid WAF exception create payload")
		}
	case DomainLifecycleRemoveWAFException:
		if payload.DomainID == "" || payload.Name != "" || payload.CanonicalMode != nil || payload.Target != nil ||
			payload.DisableTLS || payload.TLSMode != "" || payload.WAFMode != nil || payload.CachePreset != nil ||
			payload.WAFException != nil || payload.WAFExceptionID == "" {
			return errors.New("invalid WAF exception removal payload")
		}
		if _, err := core.ParseID(payload.WAFExceptionID); err != nil {
			return errors.New("invalid WAF exception ID")
		}
	default:
		return errors.New("invalid domain lifecycle action")
	}
	if payload.DomainID != "" {
		if _, err := core.ParseID(payload.DomainID); err != nil {
			return err
		}
	}
	return nil
}

func structToObject(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	return object, nil
}

func decodeStrictObject(value map[string]any, destination any) error {
	if value == nil {
		return errors.New("missing object")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("object contains trailing JSON")
	}
	return nil
}

func lifecycleCorrelation(operation core.Operation) agentprotocol.AuditCorrelation {
	correlation := agentprotocol.AuditCorrelation{
		OperationID: string(operation.ID), ActorKind: agentprotocol.ActorSystem,
		AccountID: string(*operation.AccountID),
	}
	if operation.ActorID != nil {
		correlation.ActorKind = agentprotocol.ActorIdentity
		correlation.ActorID = string(*operation.ActorID)
	}
	return correlation
}

func classifyDomainRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "domain.lifecycle_state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "domain.lifecycle_conflict"}
	default:
		return &Failure{Code: "domain.lifecycle_state_unavailable", Retryable: true}
	}
}

func classifyDomainFilesystemFailure(err error) error {
	if errors.Is(err, agentprotocol.ErrInvalidRequest) {
		return &Failure{Code: "domain.document_root_rejected"}
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "domain.agent_unreachable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorFilesystemConflict, agentprotocol.ErrorFilesystemMigration,
		agentprotocol.ErrorIdentityConflict, agentprotocol.ErrorIdempotencyConflict,
		agentprotocol.ErrorInvalidRequest:
		return &Failure{Code: "domain.document_root_rejected"}
	default:
		return &Failure{Code: "domain.document_root_failed", Retryable: true}
	}
}

func classifyDomainPHPFailure(err error) error {
	if errors.Is(err, agentprotocol.ErrInvalidRequest) {
		return &Failure{Code: "domain.php_pool_rejected"}
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "domain.agent_unreachable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorPHPConflict, agentprotocol.ErrorInvalidRequest,
		agentprotocol.ErrorIdempotencyConflict:
		return &Failure{Code: "domain.php_pool_rejected"}
	case agentprotocol.ErrorPHPUnavailable:
		if remote.Capability != nil && remote.Capability.Status == agentprotocol.CapabilityUnsupported {
			return &Failure{Code: "domain.php_runtime_unsupported"}
		}
		return &Failure{Code: "domain.php_runtime_unavailable", Retryable: true}
	case agentprotocol.ErrorPHPValidation:
		return &Failure{Code: "domain.php_pool_validation_failed"}
	default:
		return &Failure{Code: "domain.php_pool_failed", Retryable: true}
	}
}

func stringCompare(first, second string) int {
	switch {
	case first < second:
		return -1
	case first > second:
		return 1
	default:
		return 0
	}
}
