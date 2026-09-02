// SPDX-License-Identifier: AGPL-3.0-or-later

package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

type updateRepositoryFake struct {
	mu       sync.Mutex
	settings core.UpdateSettings
	now      time.Time
}

func (repository *updateRepositoryFake) GetUpdateSettings(context.Context) (core.UpdateSettings, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.settings, nil
}

func (repository *updateRepositoryFake) UpdateUpdatePolicy(
	_ context.Context, params core.UpdatePolicyParams,
) (core.UpdateSettings, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.settings.Channel = params.Channel
	repository.settings.AutomaticChecks = params.AutomaticChecks
	return repository.settings, nil
}

func (repository *updateRepositoryFake) RecordUpdateCheckSuccess(
	_ context.Context, params core.RecordUpdateCheckSuccessParams,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.settings.Channel != params.ExpectedChannel {
		return nil
	}
	now := repository.now
	repository.settings.LastAttemptedAt = &now
	repository.settings.LastSuccessfulAt = &now
	repository.settings.LastErrorCode = ""
	repository.settings.RateLimitResetAt = nil
	if !params.NotModified {
		repository.settings.ETag = params.ETag
		repository.settings.LatestRelease = params.LatestRelease
	}
	return nil
}

func (repository *updateRepositoryFake) RecordUpdateCheckFailure(
	_ context.Context, params core.RecordUpdateCheckFailureParams,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.settings.Channel != params.ExpectedChannel {
		return nil
	}
	now := repository.now
	repository.settings.LastAttemptedAt = &now
	repository.settings.LastErrorCode = params.ErrorCode
	repository.settings.RateLimitResetAt = params.RateLimitResetAt
	return nil
}

func TestCheckNowSelectsOnlyCompleteImmutableChannelReleases(t *testing.T) {
	tests := []struct {
		name, channel, wantTag string
	}{
		{name: "stable excludes beta", channel: "stable", wantTag: "v1.4.0"},
		{name: "beta includes stable and beta", channel: "beta", wantTag: "v2.0.0-beta.2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.September, 2, 14, 0, 0, 0, time.UTC)
			repository := &updateRepositoryFake{
				settings: core.UpdateSettings{Channel: core.UpdateChannel(test.channel), AutomaticChecks: true},
				now:      now,
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("Accept"); got != "application/vnd.github+json" {
					t.Errorf("Accept = %q", got)
				}
				if got := request.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
					t.Errorf("X-GitHub-Api-Version = %q", got)
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("ETag", `"release-list"`)
				_ = json.NewEncoder(w).Encode([]githubRelease{
					testGitHubRelease("v9.0.0", false, false, now, true),
					testGitHubRelease("v8.0.0", false, true, now, false),
					testGitHubRelease("v2.0.0-beta.2", true, true, now, true),
					testGitHubRelease("v1.4.0", false, true, now, true),
				})
			}))
			defer server.Close()

			service, err := New(repository, "1.0.0")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			service.endpoint = server.URL
			service.client = server.Client()
			service.now = func() time.Time { return now }
			status, err := service.CheckNow(context.Background())
			if err != nil {
				t.Fatalf("CheckNow: %v", err)
			}
			if status.LatestRelease == nil || status.LatestRelease.Tag != test.wantTag || !status.UpdateAvailable {
				t.Fatalf("status = %#v", status)
			}
			if repository.settings.ETag != `"release-list"` || !status.LatestRelease.Immutable {
				t.Fatalf("stored settings = %#v", repository.settings)
			}
		})
	}
}

func TestCheckNowUsesETagAndPreservesCandidateOnNotModified(t *testing.T) {
	now := time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC)
	published := now.Add(-time.Hour)
	repository := &updateRepositoryFake{
		settings: core.UpdateSettings{
			Channel: core.UpdateChannelStable, AutomaticChecks: true, ETag: `"previous"`,
			LatestRelease: &core.UpdateRelease{
				Version: "1.1.0", Tag: "v1.1.0", URL: "https://github.com/RTBGG/Stackfort/releases/tag/v1.1.0",
				PublishedAt: published, Immutable: true,
			},
			LastErrorCode: errorDiscoveryUnavailable,
		},
		now: now,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("If-None-Match"); got != `"previous"` {
			t.Errorf("If-None-Match = %q", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	service, _ := New(repository, "1.0.0")
	service.endpoint, service.client, service.now = server.URL, server.Client(), func() time.Time { return now }

	status, err := service.CheckNow(context.Background())
	if err != nil || status.LatestRelease == nil || status.LatestRelease.Tag != "v1.1.0" || status.LastErrorCode != "" {
		t.Fatalf("CheckNow status = %#v, %v", status, err)
	}
}

func TestCheckNowPersistsRateLimitWithoutDiscardingLastGoodRelease(t *testing.T) {
	now := time.Date(2026, time.September, 2, 16, 0, 0, 0, time.UTC)
	repository := &updateRepositoryFake{
		settings: core.UpdateSettings{
			Channel: core.UpdateChannelStable, AutomaticChecks: true,
			LatestRelease: &core.UpdateRelease{Version: "1.0.0", Tag: "v1.0.0", PublishedAt: now, Immutable: true},
		},
		now: now,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	service, _ := New(repository, "1.0.0")
	service.endpoint, service.client, service.now = server.URL, server.Client(), func() time.Time { return now }

	status, err := service.CheckNow(context.Background())
	if !errors.Is(err, ErrRateLimited) || status.LatestRelease == nil || status.LastErrorCode != errorRateLimited {
		t.Fatalf("CheckNow status = %#v, err = %v", status, err)
	}
	if status.RateLimitResetAt == nil || !status.RateLimitResetAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("rate-limit reset = %v", status.RateLimitResetAt)
	}
}

func TestRunAutomaticHonorsDefaultPolicyAndSchedule(t *testing.T) {
	now := time.Date(2026, time.September, 2, 17, 0, 0, 0, time.UTC)
	lastAttempt := now.Add(-time.Hour)
	repository := &updateRepositoryFake{
		settings: core.UpdateSettings{
			Channel: core.UpdateChannelStable, AutomaticChecks: true, LastAttemptedAt: &lastAttempt,
		},
		now: now,
	}
	service, _ := New(repository, "1.0.0")
	service.now = func() time.Time { return now }
	attempted, status, err := service.RunAutomatic(context.Background())
	if err != nil || attempted || status.NextAutomaticCheckAt == nil ||
		!status.NextAutomaticCheckAt.Equal(lastAttempt.Add(AutomaticCheckInterval)) {
		t.Fatalf("not-due automatic check = %t, %#v, %v", attempted, status, err)
	}

	repository.settings.AutomaticChecks = false
	attempted, status, err = service.RunAutomatic(context.Background())
	if err != nil || attempted || status.NextAutomaticCheckAt != nil {
		t.Fatalf("disabled automatic check = %t, %#v, %v", attempted, status, err)
	}
}

func TestCheckNowRejectsInvalidResponse(t *testing.T) {
	now := time.Date(2026, time.September, 2, 18, 0, 0, 0, time.UTC)
	repository := &updateRepositoryFake{
		settings: core.UpdateSettings{Channel: core.UpdateChannelStable, AutomaticChecks: true}, now: now,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("not JSON"))
	}))
	defer server.Close()
	service, _ := New(repository, "1.0.0")
	service.endpoint, service.client, service.now = server.URL, server.Client(), func() time.Time { return now }
	status, err := service.CheckNow(context.Background())
	if !errors.Is(err, ErrInvalidResponse) || status.LastErrorCode != errorInvalidResponse {
		t.Fatalf("CheckNow status = %#v, err = %v", status, err)
	}
}

func TestCompleteReleaseAssetsRequiresVersionBoundUniqueDigests(t *testing.T) {
	if !completeReleaseAssets("1.2.3-beta.4", testReleaseAssets("1.2.3-beta.4")) {
		t.Fatal("complete beta release assets were rejected")
	}
	tests := []struct {
		name   string
		mutate func([]githubAsset) []githubAsset
	}{
		{name: "wrong package version", mutate: func(assets []githubAsset) []githubAsset {
			assets[4].Name = "stackfort-release_1.2.2-1_amd64.deb"
			return assets
		}},
		{name: "duplicate name", mutate: func(assets []githubAsset) []githubAsset {
			return append(assets, assets[0])
		}},
		{name: "missing digest", mutate: func(assets []githubAsset) []githubAsset {
			assets[0].Digest = ""
			return assets
		}},
		{name: "empty asset", mutate: func(assets []githubAsset) []githubAsset {
			assets[1].Size = 0
			return assets
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if completeReleaseAssets("1.2.3", test.mutate(testReleaseAssets("1.2.3"))) {
				t.Fatal("unsafe release assets were accepted")
			}
		})
	}
}

func TestRateLimitResetIsBounded(t *testing.T) {
	now := time.Date(2026, time.September, 2, 19, 0, 0, 0, time.UTC)
	header := make(http.Header)
	header.Set("Retry-After", "9223372036854775807")
	reset := rateLimitReset(header, now)
	if reset == nil || !reset.Equal(now.Add(maximumRateLimitDelay)) {
		t.Fatalf("Retry-After reset = %v", reset)
	}
	header = make(http.Header)
	header.Set("X-RateLimit-Reset", "253402300799")
	reset = rateLimitReset(header, now)
	if reset == nil || !reset.Equal(now.Add(maximumRateLimitDelay)) {
		t.Fatalf("epoch reset = %v", reset)
	}
}

func testGitHubRelease(
	tag string, prerelease, immutable bool, publishedAt time.Time, complete bool,
) githubRelease {
	_, version, _ := parseReleaseVersion(tag)
	release := githubRelease{
		TagName: tag, Prerelease: prerelease, Immutable: immutable, PublishedAt: &publishedAt,
		Assets: testReleaseAssets(version),
	}
	if !complete {
		release.Assets = release.Assets[:len(release.Assets)-1]
	}
	return release
}

func testReleaseAssets(version string) []githubAsset {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	names := []string{
		"SHA256SUMS",
		"stackfort-" + version + "-linux-amd64.tar.gz",
		"stackfort-installer-" + version + "-linux-amd64",
		"stackfort-" + version + ".spdx.json",
		"stackfort-release_" + testPackageVersion(version) + "-1_amd64.deb",
		"stackfort-release_" + testPackageVersion(version) + "-1_amd64.deb.release.json",
		"stackfort-release-" + testPackageVersion(version) + "-1.sf1.x86_64.rpm",
		"stackfort-release-" + testPackageVersion(version) + "-1.sf1.x86_64.rpm.release.json",
	}
	assets := make([]githubAsset, 0, len(names))
	for _, name := range names {
		assets = append(assets, githubAsset{Name: name, State: "uploaded", Size: 1, Digest: digest})
	}
	return assets
}

func testPackageVersion(version string) string {
	for index, character := range version {
		if character == '-' {
			return version[:index] + "~" + version[index+1:]
		}
	}
	return version
}
