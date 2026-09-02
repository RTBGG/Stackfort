// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

type UpdateChannel string

const (
	UpdateChannelStable UpdateChannel = "stable"
	UpdateChannelBeta   UpdateChannel = "beta"
)

type UpdateRelease struct {
	Version     string
	Tag         string
	URL         string
	PublishedAt time.Time
	Prerelease  bool
	Immutable   bool
}

// UpdateSettings is the durable discovery policy and its last bounded result.
// ETag is deliberately omitted by the browser API.
type UpdateSettings struct {
	Channel          UpdateChannel
	AutomaticChecks  bool
	PolicyUpdatedAt  time.Time
	ETag             string
	LastAttemptedAt  *time.Time
	LastSuccessfulAt *time.Time
	LatestRelease    *UpdateRelease
	LastErrorCode    string
	RateLimitResetAt *time.Time
}

type UpdatePolicyParams struct {
	Subject         AuthorizationSubject
	Channel         UpdateChannel
	AutomaticChecks bool
	RequestID       string
	SourceAddress   string
}

type RecordUpdateCheckSuccessParams struct {
	ExpectedChannel UpdateChannel
	ETag            string
	NotModified     bool
	LatestRelease   *UpdateRelease
}

type RecordUpdateCheckFailureParams struct {
	ExpectedChannel  UpdateChannel
	ErrorCode        string
	RateLimitResetAt *time.Time
}

func validUpdateChannel(channel UpdateChannel) bool {
	return channel == UpdateChannelStable || channel == UpdateChannelBeta
}

func (r *Repository) GetUpdateSettings(ctx context.Context) (UpdateSettings, error) {
	var result UpdateSettings
	var automatic int64
	var policyUpdatedAt string
	var lastAttemptedAt, lastSuccessfulAt sql.NullString
	var latestVersion, latestTag, latestURL, latestPublishedAt sql.NullString
	var latestPrerelease, latestImmutable sql.NullInt64
	var rateLimitResetAt sql.NullString
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT p.channel, p.automatic_checks, p.updated_at,
			       s.etag, s.last_attempted_at, s.last_successful_at,
			       s.latest_version, s.latest_tag, s.latest_url, s.latest_published_at,
			       s.latest_prerelease, s.latest_immutable, s.last_error_code,
			       s.rate_limit_reset_at
			FROM update_policy p JOIN update_check_state s ON s.singleton = p.singleton
			WHERE p.singleton = 1`).Scan(
			&result.Channel, &automatic, &policyUpdatedAt,
			&result.ETag, &lastAttemptedAt, &lastSuccessfulAt,
			&latestVersion, &latestTag, &latestURL, &latestPublishedAt,
			&latestPrerelease, &latestImmutable, &result.LastErrorCode,
			&rateLimitResetAt,
		)
	})
	if err != nil {
		return UpdateSettings{}, classifyDatabaseError(err)
	}
	result.AutomaticChecks = automatic == 1
	result.PolicyUpdatedAt, err = parseTime(policyUpdatedAt)
	if err != nil {
		return UpdateSettings{}, err
	}
	if result.LastAttemptedAt, err = parseOptionalStoredTime(lastAttemptedAt); err != nil {
		return UpdateSettings{}, err
	}
	if result.LastSuccessfulAt, err = parseOptionalStoredTime(lastSuccessfulAt); err != nil {
		return UpdateSettings{}, err
	}
	if result.RateLimitResetAt, err = parseOptionalStoredTime(rateLimitResetAt); err != nil {
		return UpdateSettings{}, err
	}
	if latestVersion.Valid {
		publishedAt, parseErr := time.Parse(time.RFC3339Nano, latestPublishedAt.String)
		if parseErr != nil {
			return UpdateSettings{}, fmt.Errorf("parse stored release publication time: %w", parseErr)
		}
		result.LatestRelease = &UpdateRelease{
			Version: latestVersion.String, Tag: latestTag.String, URL: latestURL.String,
			PublishedAt: publishedAt.UTC(), Prerelease: latestPrerelease.Int64 == 1,
			Immutable: latestImmutable.Int64 == 1,
		}
	}
	return result, nil
}

func parseOptionalStoredTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (r *Repository) UpdateUpdatePolicy(ctx context.Context, params UpdatePolicyParams) (UpdateSettings, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject, Action: AuthorizationPlatformManage,
	}); err != nil {
		return UpdateSettings{}, err
	}
	if !validUpdateChannel(params.Channel) {
		return UpdateSettings{}, fmt.Errorf("%w: update channel must be stable or beta", ErrInvalidInput)
	}
	requestID, sourceAddress, err := validateSessionManagementMetadata(params.RequestID, params.SourceAddress)
	if err != nil {
		return UpdateSettings{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		var previousChannel UpdateChannel
		if err := executor.QueryRowContext(ctx,
			"SELECT channel FROM update_policy WHERE singleton = 1").Scan(&previousChannel); err != nil {
			return err
		}
		automatic := 0
		if params.AutomaticChecks {
			automatic = 1
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE update_policy
			SET channel = ?, automatic_checks = ?, updated_at = ?
			WHERE singleton = 1`, string(params.Channel), automatic, formatTime(now))
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return err
		}
		if previousChannel != params.Channel {
			if _, err := executor.ExecContext(ctx, `
				UPDATE update_check_state
				SET etag = '', last_attempted_at = NULL, last_successful_at = NULL,
				    latest_version = NULL, latest_tag = NULL, latest_url = NULL,
				    latest_published_at = NULL, latest_prerelease = NULL,
				    latest_immutable = NULL, last_error_code = '', rate_limit_reset_at = NULL
				WHERE singleton = 1`); err != nil {
				return err
			}
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "platform.update_policy_changed",
			TargetType: "update_policy", TargetID: "global", RequestID: requestID,
			Result: AuditSuccess, Details: map[string]any{
				"channel": params.Channel, "automaticChecks": params.AutomaticChecks,
			},
		}, now)
	})
	if err != nil {
		return UpdateSettings{}, classifyDatabaseError(err)
	}
	return r.GetUpdateSettings(ctx)
}

func (r *Repository) RecordUpdateCheckSuccess(
	ctx context.Context,
	params RecordUpdateCheckSuccessParams,
) error {
	if !validUpdateChannel(params.ExpectedChannel) {
		return fmt.Errorf("%w: invalid expected update channel", ErrInvalidInput)
	}
	etag, err := validateOptionalText(params.ETag, "etag", 512)
	if err != nil {
		return err
	}
	if params.NotModified && params.LatestRelease != nil {
		return fmt.Errorf("%w: unchanged update check cannot replace release metadata", ErrInvalidInput)
	}
	if params.LatestRelease != nil {
		if err := validateUpdateRelease(*params.LatestRelease); err != nil {
			return err
		}
	}
	now := r.timestamp()
	return r.state.Write(ctx, func(executor store.Executor) error {
		if params.NotModified {
			_, err := executor.ExecContext(ctx, `
				UPDATE update_check_state
				SET last_attempted_at = ?, last_successful_at = ?, last_error_code = '',
				    rate_limit_reset_at = NULL
				WHERE singleton = 1 AND EXISTS (
				    SELECT 1 FROM update_policy WHERE singleton = 1 AND channel = ?
				)`, formatTime(now), formatTime(now), string(params.ExpectedChannel))
			return err
		}
		var version, tag, releaseURL, publishedAt any
		var prerelease, immutable any
		if params.LatestRelease != nil {
			version = params.LatestRelease.Version
			tag = params.LatestRelease.Tag
			releaseURL = params.LatestRelease.URL
			publishedAt = formatTime(params.LatestRelease.PublishedAt)
			prerelease = boolInteger(params.LatestRelease.Prerelease)
			immutable = boolInteger(params.LatestRelease.Immutable)
		}
		_, err := executor.ExecContext(ctx, `
			UPDATE update_check_state
			SET etag = ?, last_attempted_at = ?, last_successful_at = ?,
			    latest_version = ?, latest_tag = ?, latest_url = ?, latest_published_at = ?,
			    latest_prerelease = ?, latest_immutable = ?, last_error_code = '',
			    rate_limit_reset_at = NULL
			WHERE singleton = 1 AND EXISTS (
			    SELECT 1 FROM update_policy WHERE singleton = 1 AND channel = ?
			)`, etag, formatTime(now), formatTime(now), version, tag, releaseURL,
			publishedAt, prerelease, immutable, string(params.ExpectedChannel))
		return err
	})
}

func (r *Repository) RecordUpdateCheckFailure(
	ctx context.Context,
	params RecordUpdateCheckFailureParams,
) error {
	if !validUpdateChannel(params.ExpectedChannel) {
		return fmt.Errorf("%w: invalid expected update channel", ErrInvalidInput)
	}
	errorCode, err := validateText(params.ErrorCode, "errorCode", 1, 80)
	if err != nil {
		return err
	}
	if _, err := validateAction(errorCode, "errorCode", 80); err != nil {
		return err
	}
	now := r.timestamp()
	var resetAt any
	if params.RateLimitResetAt != nil {
		resetAt = formatTime(params.RateLimitResetAt.UTC())
	}
	return r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			UPDATE update_check_state
			SET last_attempted_at = ?, last_error_code = ?, rate_limit_reset_at = ?
			WHERE singleton = 1 AND EXISTS (
			    SELECT 1 FROM update_policy WHERE singleton = 1 AND channel = ?
			)`, formatTime(now), errorCode, resetAt, string(params.ExpectedChannel))
		return err
	})
}

func validateUpdateRelease(release UpdateRelease) error {
	if !release.Immutable {
		return fmt.Errorf("%w: discovered update release must be immutable", ErrInvalidInput)
	}
	for _, field := range []struct{ name, value string }{
		{name: "version", value: release.Version},
		{name: "tag", value: release.Tag},
		{name: "url", value: release.URL},
	} {
		if _, err := validateText(field.value, field.name, 1, 512); err != nil {
			return err
		}
	}
	if release.PublishedAt.IsZero() {
		return fmt.Errorf("%w: release publication time is required", ErrInvalidInput)
	}
	return nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
