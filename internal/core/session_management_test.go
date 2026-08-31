// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"testing"
)

func TestIdentitySessionManagementIsScopedAndCanKeepCurrent(t *testing.T) {
	repository, _ := newTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	first, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.1",
	))
	if err != nil {
		t.Fatalf("first PasswordLogin: %v", err)
	}
	second, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.2",
	))
	if err != nil {
		t.Fatalf("second PasswordLogin: %v", err)
	}
	third, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.3",
	))
	if err != nil {
		t.Fatalf("third PasswordLogin: %v", err)
	}
	subject := authenticateForTest(t, repository, first.SessionToken).AuthorizationSubject()
	sessions, err := repository.ListManagedSessions(context.Background(), ListManagedSessionsParams{Subject: subject})
	if err != nil {
		t.Fatalf("ListManagedSessions: %v", err)
	}
	if len(sessions) != 3 || countCurrentSessions(sessions) != 1 {
		t.Fatalf("managed sessions = %#v", sessions)
	}
	if err := repository.RevokeManagedSession(context.Background(), RevokeManagedSessionParams{
		Subject: subject, TargetSessionID: second.Session.ID, SourceAddress: "192.0.2.1",
	}); err != nil {
		t.Fatalf("RevokeManagedSession: %v", err)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: second.SessionToken,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session replay error = %v", err)
	}

	createPasswordIdentity(t, repository, "other@example.com", "another secure password")
	foreign, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"other@example.com", "another secure password", "198.51.100.1",
	))
	if err != nil {
		t.Fatalf("foreign PasswordLogin: %v", err)
	}
	if err := repository.RevokeManagedSession(context.Background(), RevokeManagedSessionParams{
		Subject: subject, TargetSessionID: foreign.Session.ID, SourceAddress: "192.0.2.1",
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("foreign revoke error = %v", err)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: foreign.SessionToken,
	}); err != nil {
		t.Fatalf("foreign session was changed: %v", err)
	}

	allResult, err := repository.RevokeAllManagedSessions(context.Background(), RevokeAllManagedSessionsParams{
		Subject: subject, KeepCurrent: true, SourceAddress: "192.0.2.1",
	})
	if err != nil {
		t.Fatalf("RevokeAllManagedSessions keep current: %v", err)
	}
	if allResult.Revoked != 1 || allResult.CurrentRevoked {
		t.Fatalf("keep-current result = %#v", allResult)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: third.SessionToken,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("third session replay error = %v", err)
	}
	finalResult, err := repository.RevokeAllManagedSessions(context.Background(), RevokeAllManagedSessionsParams{
		Subject: subject, KeepCurrent: false, SourceAddress: "192.0.2.1",
	})
	if err != nil {
		t.Fatalf("RevokeAllManagedSessions current: %v", err)
	}
	if finalResult.Revoked != 1 || !finalResult.CurrentRevoked {
		t.Fatalf("final revoke result = %#v", finalResult)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: first.SessionToken,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("current session replay error = %v", err)
	}
}

func countCurrentSessions(sessions []ManagedSession) int {
	count := 0
	for _, session := range sessions {
		if session.Current {
			count++
		}
	}
	return count
}
