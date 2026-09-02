// SPDX-License-Identifier: AGPL-3.0-or-later

package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
)

const (
	githubReleasesEndpoint = "https://api.github.com/repos/RTBGG/Stackfort/releases?per_page=20"
	githubAPIVersion       = "2026-03-10"
	maximumResponseBytes   = 2 << 20
	AutomaticCheckInterval = 6 * time.Hour
	automaticFailureRetry  = time.Hour
	manualCheckCooldown    = time.Minute
	maximumRateLimitDelay  = 24 * time.Hour
)

var (
	ErrDiscoveryUnavailable         = errors.New("release discovery is unavailable")
	ErrRateLimited                  = errors.New("release discovery is rate limited")
	ErrInvalidResponse              = errors.New("release discovery returned an invalid response")
	ErrFunctionalUpdatesUnavailable = errors.New("functional updates are unavailable on this platform")

	sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const (
	errorDiscoveryUnavailable = "release_discovery_unavailable"
	errorRateLimited          = "update_check_rate_limited"
	errorInvalidResponse      = "invalid_release_response"
)

type Repository interface {
	GetUpdateSettings(context.Context) (core.UpdateSettings, error)
	UpdateUpdatePolicy(context.Context, core.UpdatePolicyParams) (core.UpdateSettings, error)
	RecordUpdateCheckSuccess(context.Context, core.RecordUpdateCheckSuccessParams) error
	RecordUpdateCheckFailure(context.Context, core.RecordUpdateCheckFailureParams) error
}

type Release struct {
	Version     string    `json:"version"`
	Tag         string    `json:"tag"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	Prerelease  bool      `json:"prerelease"`
	Immutable   bool      `json:"immutable"`
}

type Status struct {
	CurrentVersion             string                                      `json:"currentVersion"`
	CurrentVersionValid        bool                                        `json:"currentVersionValid"`
	Channel                    core.UpdateChannel                          `json:"channel"`
	AutomaticChecks            bool                                        `json:"automaticChecks"`
	AutomaticFunctionalUpdates bool                                        `json:"automaticFunctionalUpdates"`
	CheckIntervalSeconds       int64                                       `json:"checkIntervalSeconds"`
	LastAttemptedAt            *time.Time                                  `json:"lastAttemptedAt,omitempty"`
	LastSuccessfulAt           *time.Time                                  `json:"lastSuccessfulAt,omitempty"`
	NextAutomaticCheckAt       *time.Time                                  `json:"nextAutomaticCheckAt,omitempty"`
	LatestRelease              *Release                                    `json:"latestRelease,omitempty"`
	UpdateAvailable            bool                                        `json:"updateAvailable"`
	LastErrorCode              string                                      `json:"lastErrorCode,omitempty"`
	RateLimitResetAt           *time.Time                                  `json:"rateLimitResetAt,omitempty"`
	PlatformUpdate             *agentprotocol.PlatformUpdateStatusResponse `json:"platformUpdate,omitempty"`
}

type Service struct {
	repository     Repository
	client         *http.Client
	endpoint       string
	currentVersion string
	now            func() time.Time
	checkMu        sync.Mutex
}

func New(repository Repository, currentVersion string) (*Service, error) {
	if repository == nil {
		return nil, errors.New("update check service requires a repository")
	}
	if strings.TrimSpace(currentVersion) == "" || len(currentVersion) > 64 {
		return nil, errors.New("update check service requires a bounded current version")
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("release discovery redirects are not allowed")
		},
	}
	return &Service{
		repository: repository, client: client, endpoint: githubReleasesEndpoint,
		currentVersion: currentVersion, now: time.Now,
	}, nil
}

func (service *Service) Status(ctx context.Context) (Status, error) {
	settings, err := service.repository.GetUpdateSettings(ctx)
	if err != nil {
		return Status{}, err
	}
	return service.status(settings), nil
}

func (service *Service) UpdatePolicy(
	ctx context.Context,
	params core.UpdatePolicyParams,
) (Status, error) {
	settings, err := service.repository.UpdateUpdatePolicy(ctx, params)
	if err != nil {
		return Status{}, err
	}
	return service.status(settings), nil
}

func (service *Service) CheckNow(ctx context.Context) (Status, error) {
	_, status, err := service.check(ctx, true)
	return status, err
}

// StartUpdate keeps the discovery-only service safe on unsupported platforms.
// Linux production wiring replaces it with updateworkspace.Service.
func (service *Service) StartUpdate(
	context.Context, core.PrepareUpdateActivationParams,
) (agentprotocol.PlatformUpdateStartResponse, error) {
	return agentprotocol.PlatformUpdateStartResponse{}, ErrFunctionalUpdatesUnavailable
}

// RunAutomatic performs a network request only when the durable default-on
// policy is due. The bool reports whether a request was attempted.
func (service *Service) RunAutomatic(ctx context.Context) (bool, Status, error) {
	return service.check(ctx, false)
}

func (service *Service) check(ctx context.Context, manual bool) (bool, Status, error) {
	service.checkMu.Lock()
	defer service.checkMu.Unlock()

	settings, err := service.repository.GetUpdateSettings(ctx)
	if err != nil {
		return false, Status{}, err
	}
	now := service.now().UTC()
	if settings.RateLimitResetAt != nil && settings.RateLimitResetAt.After(now) {
		return false, service.status(settings), ErrRateLimited
	}
	if !manual {
		if !settings.AutomaticChecks || now.Before(nextAutomaticCheck(settings, now)) {
			return false, service.status(settings), nil
		}
	} else if settings.LastAttemptedAt != nil && settings.LastAttemptedAt.Add(manualCheckCooldown).After(now) {
		return false, service.status(settings), nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.endpoint, nil)
	if err != nil {
		return false, service.status(settings), fmt.Errorf("build release discovery request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	request.Header.Set("User-Agent", "Stackfort/"+service.currentVersion+" (+https://github.com/RTBGG/Stackfort)")
	if settings.ETag != "" {
		request.Header.Set("If-None-Match", settings.ETag)
	}

	response, err := service.client.Do(request)
	if err != nil {
		status, recordErr := service.recordFailure(ctx, settings, errorDiscoveryUnavailable, nil,
			fmt.Errorf("%w: %v", ErrDiscoveryUnavailable, err))
		return true, status, recordErr
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotModified {
		err = service.repository.RecordUpdateCheckSuccess(ctx, core.RecordUpdateCheckSuccessParams{
			ExpectedChannel: settings.Channel, NotModified: true,
		})
		if err != nil {
			return true, Status{}, err
		}
		status, statusErr := service.Status(ctx)
		return true, status, statusErr
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		resetAt := rateLimitReset(response.Header, now)
		status, recordErr := service.recordFailure(ctx, settings, errorRateLimited, resetAt, ErrRateLimited)
		return true, status, recordErr
	}
	if response.StatusCode != http.StatusOK {
		status, recordErr := service.recordFailure(ctx, settings, errorDiscoveryUnavailable, nil,
			fmt.Errorf("%w: GitHub returned HTTP %d", ErrDiscoveryUnavailable, response.StatusCode))
		return true, status, recordErr
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		status, recordErr := service.recordFailure(ctx, settings, errorInvalidResponse, nil, ErrInvalidResponse)
		return true, status, recordErr
	}
	etag := strings.TrimSpace(response.Header.Get("ETag"))
	if len(etag) > 512 || strings.ContainsAny(etag, "\r\n") {
		status, recordErr := service.recordFailure(ctx, settings, errorInvalidResponse, nil, ErrInvalidResponse)
		return true, status, recordErr
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(payload) > maximumResponseBytes {
		status, recordErr := service.recordFailure(ctx, settings, errorInvalidResponse, nil, ErrInvalidResponse)
		return true, status, recordErr
	}
	var releases []githubRelease
	if err := json.Unmarshal(payload, &releases); err != nil || len(releases) > 20 {
		status, recordErr := service.recordFailure(ctx, settings, errorInvalidResponse, nil, ErrInvalidResponse)
		return true, status, recordErr
	}
	latest := selectRelease(settings.Channel, releases)
	err = service.repository.RecordUpdateCheckSuccess(ctx, core.RecordUpdateCheckSuccessParams{
		ExpectedChannel: settings.Channel, ETag: etag, LatestRelease: latest,
	})
	if err != nil {
		return true, Status{}, err
	}
	status, statusErr := service.Status(ctx)
	return true, status, statusErr
}

func (service *Service) recordFailure(
	ctx context.Context,
	settings core.UpdateSettings,
	code string,
	resetAt *time.Time,
	discoveryErr error,
) (Status, error) {
	if err := service.repository.RecordUpdateCheckFailure(ctx, core.RecordUpdateCheckFailureParams{
		ExpectedChannel: settings.Channel, ErrorCode: code, RateLimitResetAt: resetAt,
	}); err != nil {
		return Status{}, errors.Join(discoveryErr, err)
	}
	status, err := service.Status(ctx)
	return status, errors.Join(discoveryErr, err)
}

func (service *Service) status(settings core.UpdateSettings) Status {
	now := service.now().UTC()
	current, currentValid := parseInstalledVersion(service.currentVersion)
	result := Status{
		CurrentVersion: service.currentVersion, CurrentVersionValid: currentValid,
		Channel: settings.Channel, AutomaticChecks: settings.AutomaticChecks,
		AutomaticFunctionalUpdates: false,
		CheckIntervalSeconds:       int64(AutomaticCheckInterval / time.Second),
		LastAttemptedAt:            settings.LastAttemptedAt, LastSuccessfulAt: settings.LastSuccessfulAt,
		LastErrorCode: settings.LastErrorCode, RateLimitResetAt: settings.RateLimitResetAt,
	}
	if settings.AutomaticChecks {
		next := nextAutomaticCheck(settings, now)
		result.NextAutomaticCheckAt = &next
	}
	if settings.LatestRelease != nil {
		release := settings.LatestRelease
		result.LatestRelease = &Release{
			Version: release.Version, Tag: release.Tag, URL: release.URL,
			PublishedAt: release.PublishedAt, Prerelease: release.Prerelease,
			Immutable: release.Immutable,
		}
		latest, latestValid := parseInstalledVersion(release.Version)
		result.UpdateAvailable = currentValid && latestValid && compareVersions(latest, current) > 0
	}
	return result
}

func nextAutomaticCheck(settings core.UpdateSettings, now time.Time) time.Time {
	if settings.LastAttemptedAt == nil {
		return now
	}
	interval := AutomaticCheckInterval
	if settings.LastErrorCode != "" {
		interval = automaticFailureRetry
	}
	next := settings.LastAttemptedAt.Add(interval)
	if settings.RateLimitResetAt != nil && settings.RateLimitResetAt.After(next) {
		next = *settings.RateLimitResetAt
	}
	return next
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Immutable   bool          `json:"immutable"`
	PublishedAt *time.Time    `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

func selectRelease(channel core.UpdateChannel, releases []githubRelease) *core.UpdateRelease {
	var selected *core.UpdateRelease
	var selectedVersion semanticVersion
	for _, release := range releases {
		parsed, version, ok := parseReleaseVersion(release.TagName)
		if !ok || release.Draft || !release.Immutable || release.PublishedAt == nil || release.PublishedAt.IsZero() {
			continue
		}
		if parsed.prerelease != release.Prerelease {
			continue
		}
		if channel == core.UpdateChannelStable && parsed.prerelease {
			continue
		}
		if !completeReleaseAssets(version, release.Assets) {
			continue
		}
		if selected != nil && compareVersions(parsed, selectedVersion) <= 0 {
			continue
		}
		selectedVersion = parsed
		selected = &core.UpdateRelease{
			Version: version, Tag: release.TagName,
			URL:         "https://github.com/RTBGG/Stackfort/releases/tag/" + url.PathEscape(release.TagName),
			PublishedAt: release.PublishedAt.UTC(), Prerelease: release.Prerelease, Immutable: true,
		}
	}
	return selected
}

func completeReleaseAssets(version string, assets []githubAsset) bool {
	packageVersion := version
	if separator := strings.IndexByte(version, '-'); separator >= 0 {
		packageVersion = version[:separator] + "~" + version[separator+1:]
	}
	debPackage := "stackfort-release_" + packageVersion + "-1_amd64.deb"
	rpmPackage := "stackfort-release-" + packageVersion + "-1.sf1.x86_64.rpm"
	required := map[string]bool{
		"SHA256SUMS": false,
		"stackfort-" + version + "-linux-amd64.tar.gz":    false,
		"stackfort-installer-" + version + "-linux-amd64": false,
		"stackfort-" + version + ".spdx.json":             false,
		debPackage:                                        false,
		debPackage + ".release.json":                      false,
		rpmPackage:                                        false,
		rpmPackage + ".release.json":                      false,
	}
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if _, exists := seen[asset.Name]; exists {
			return false
		}
		seen[asset.Name] = struct{}{}
		if asset.State != "uploaded" || asset.Size <= 0 || !sha256DigestPattern.MatchString(asset.Digest) {
			continue
		}
		if _, exists := required[asset.Name]; exists {
			required[asset.Name] = true
		}
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
}

func rateLimitReset(header http.Header, now time.Time) *time.Time {
	if retryAfter, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && retryAfter > 0 {
		delay := maximumRateLimitDelay
		if retryAfter <= int64(maximumRateLimitDelay/time.Second) {
			delay = time.Duration(retryAfter) * time.Second
		}
		reset := now.Add(delay).UTC()
		return &reset
	}
	if epoch, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && epoch > 0 {
		reset := time.Unix(epoch, 0).UTC()
		if reset.After(now) {
			maximum := now.Add(maximumRateLimitDelay).UTC()
			if reset.After(maximum) {
				reset = maximum
			}
			return &reset
		}
	}
	reset := now.Add(automaticFailureRetry).UTC()
	return &reset
}
