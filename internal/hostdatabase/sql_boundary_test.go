// SPDX-License-Identifier: AGPL-3.0-or-later

package hostdatabase

import (
	"context"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func TestLiteralGrantPatternIsDistinctFromDatabaseIdentifier(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, pattern string }{
		{"plain", "plain"},
		{"app_data", `app\_data`},
		{"sf_0123_app__data", `sf\_0123\_app\_\_data`},
		{`percent%and\slash`, `percent\%and\\slash`},
	} {
		if got := literalGrantPattern(test.name); got != test.pattern {
			t.Fatalf("literalGrantPattern(%q) = %q, want %q", test.name, got, test.pattern)
		}
		if got := quoteIdentifier(test.name); got != "`"+test.name+"`" {
			t.Fatal("CREATE/DROP identifier must not acquire grant-pattern escaping")
		}
	}
}

func TestDatabaseEntryPointsRejectSQLPayloadsBeforeOpeningBackend(t *testing.T) {
	t.Parallel()
	provision := validProvisionRequest()
	rotation := agentprotocol.DatabasePasswordRotateRequest{
		UserAlias: provision.UserAlias, Username: provision.Username, Host: provision.Host,
		Password: provision.Password,
	}
	drop := agentprotocol.DatabaseDropRequest{
		Kind: agentprotocol.DatabaseDropDatabase, Alias: provision.DatabaseAlias, Name: provision.DatabaseName,
		Grants: []agentprotocol.DatabaseDropGrant{{
			UserAlias: provision.UserAlias, Username: provision.Username, Host: provision.Host, Preset: provision.Preset,
		}},
	}
	tests := []struct {
		name string
		call func(*Reconciler) (Result, error)
	}{
		{"provision-physical-name", func(r *Reconciler) (Result, error) {
			request := provision
			request.DatabaseName += "`; DROP DATABASE mysql; --"
			return r.Reconcile(t.Context(), testOperation, testAccount, request)
		}},
		{"provision-alias", func(r *Reconciler) (Result, error) {
			request := provision
			request.DatabaseAlias = "app`"
			return r.Reconcile(t.Context(), testOperation, testAccount, request)
		}},
		{"provision-principal", func(r *Reconciler) (Result, error) {
			request := provision
			request.Username += "'@'%' --"
			return r.Reconcile(t.Context(), testOperation, testAccount, request)
		}},
		{"provision-host", func(r *Reconciler) (Result, error) {
			request := provision
			request.Host = "%"
			return r.Reconcile(t.Context(), testOperation, testAccount, request)
		}},
		{"provision-preset", func(r *Reconciler) (Result, error) {
			request := provision
			request.Preset = "ALL PRIVILEGES WITH GRANT OPTION"
			return r.Reconcile(t.Context(), testOperation, testAccount, request)
		}},
		{"rotation-principal", func(r *Reconciler) (Result, error) {
			request := rotation
			request.Username += "'@'%' --"
			return r.RotatePassword(t.Context(), testRotation, testAccount, request)
		}},
		{"rotation-host", func(r *Reconciler) (Result, error) {
			request := rotation
			request.Host = "localhost' --"
			return r.RotatePassword(t.Context(), testRotation, testAccount, request)
		}},
		{"drop-name", func(r *Reconciler) (Result, error) {
			request := drop
			request.Name += "`; DROP DATABASE mysql; --"
			return r.Drop(t.Context(), testOperation, testAccount, request)
		}},
		{"drop-grant-principal", func(r *Reconciler) (Result, error) {
			request := drop
			request.Grants = append([]agentprotocol.DatabaseDropGrant(nil), drop.Grants...)
			request.Grants[0].Username += "'@'%' --"
			return r.Drop(t.Context(), testOperation, testAccount, request)
		}},
		{"drop-user-host", func(r *Reconciler) (Result, error) {
			request := agentprotocol.DatabaseDropRequest{
				Kind: agentprotocol.DatabaseDropUser, Alias: provision.UserAlias, Name: provision.Username, Host: "%",
			}
			return r.Drop(t.Context(), testOperation, testAccount, request)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened := false
			reconciler := &Reconciler{open: func(context.Context) (backend, error) {
				opened = true
				return newFakeBackend(), nil
			}}
			if _, err := test.call(reconciler); errorKind(err) != ErrorValidation || opened {
				t.Fatalf("invalid SQL input reached backend: error=%v opened=%v", err, opened)
			}
		})
	}
}
