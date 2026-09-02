// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestUpdateSettingsDefaultAndLifecycle(t *testing.T) {
	repository, state := newTestRepository(t)
	ctx := context.Background()
	current := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }

	settings, err := repository.GetUpdateSettings(ctx)
	if err != nil {
		t.Fatalf("GetUpdateSettings default: %v", err)
	}
	if settings.Channel != UpdateChannelStable || !settings.AutomaticChecks || settings.LatestRelease != nil {
		t.Fatalf("default update settings = %#v", settings)
	}

	admin := createTestIdentity(t, repository, "update-admin@example.test")
	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{
		IdentityID: admin.ID, Role: PlatformAdministrator,
	}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	subject := createAuthorizationSubject(t, repository, admin)
	publishedAt := current.Add(-time.Hour)
	if err := repository.RecordUpdateCheckSuccess(ctx, RecordUpdateCheckSuccessParams{
		ExpectedChannel: UpdateChannelStable,
		ETag:            `"stable-etag"`,
		LatestRelease: &UpdateRelease{
			Version: "1.2.3", Tag: "v1.2.3", URL: "https://github.com/RTBGG/Stackfort/releases/tag/v1.2.3",
			PublishedAt: publishedAt, Immutable: true,
		},
	}); err != nil {
		t.Fatalf("RecordUpdateCheckSuccess: %v", err)
	}

	resetAt := current.Add(time.Hour)
	current = current.Add(time.Minute)
	if err := repository.RecordUpdateCheckFailure(ctx, RecordUpdateCheckFailureParams{
		ExpectedChannel: UpdateChannelStable, ErrorCode: "update_check_rate_limited", RateLimitResetAt: &resetAt,
	}); err != nil {
		t.Fatalf("RecordUpdateCheckFailure: %v", err)
	}
	settings, err = repository.GetUpdateSettings(ctx)
	if err != nil || settings.LatestRelease == nil || settings.LastErrorCode != "update_check_rate_limited" {
		t.Fatalf("settings after failure = %#v, %v", settings, err)
	}

	settings, err = repository.UpdateUpdatePolicy(ctx, UpdatePolicyParams{
		Subject: subject, Channel: UpdateChannelBeta, AutomaticChecks: false,
		RequestID: "request-update-policy", SourceAddress: "192.0.2.10",
	})
	if err != nil {
		t.Fatalf("UpdateUpdatePolicy: %v", err)
	}
	if settings.Channel != UpdateChannelBeta || settings.AutomaticChecks || settings.ETag != "" ||
		settings.LastAttemptedAt != nil || settings.LatestRelease != nil || settings.LastErrorCode != "" {
		t.Fatalf("settings after channel switch = %#v", settings)
	}

	var audits int
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events
			WHERE action = 'platform.update_policy_changed' AND target_id = 'global'`).Scan(&audits)
	}); err != nil || audits != 1 {
		t.Fatalf("policy audit count = %d, %v", audits, err)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestUpdatePolicyValidationAndRecentAuthentication(t *testing.T) {
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	current := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	admin := createTestIdentity(t, repository, "update-policy@example.test")
	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{
		IdentityID: admin.ID, Role: PlatformAdministrator,
	}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	subject := createAuthorizationSubject(t, repository, admin)

	_, err := repository.UpdateUpdatePolicy(ctx, UpdatePolicyParams{
		Subject: subject, Channel: "candidate", AutomaticChecks: true,
		RequestID: "invalid-channel", SourceAddress: "192.0.2.11",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid channel error = %v, want ErrInvalidInput", err)
	}

	current = current.Add(recentAuthenticationTTL + time.Second)
	_, err = repository.UpdateUpdatePolicy(ctx, UpdatePolicyParams{
		Subject: subject, Channel: UpdateChannelStable, AutomaticChecks: false,
		RequestID: "stale-session", SourceAddress: "192.0.2.11",
	})
	if !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale policy change error = %v, want ErrRecentAuthenticationRequired", err)
	}
}

func TestUpdateCheckRecordValidation(t *testing.T) {
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	if err := repository.RecordUpdateCheckSuccess(ctx, RecordUpdateCheckSuccessParams{
		ExpectedChannel: UpdateChannelStable,
		LatestRelease: &UpdateRelease{
			Version: "1.0.0", Tag: "v1.0.0", URL: "https://example.test/release",
			PublishedAt: time.Now().UTC(), Immutable: false,
		},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mutable release error = %v, want ErrInvalidInput", err)
	}
	if err := repository.RecordUpdateCheckFailure(ctx, RecordUpdateCheckFailureParams{
		ExpectedChannel: UpdateChannelStable, ErrorCode: "contains spaces",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsafe error code error = %v, want ErrInvalidInput", err)
	}
}
