// SPDX-License-Identifier: AGPL-3.0-or-later
package panelworkspace

import (
	"context"
	"errors"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
	"testing"
)

type repositoryStub struct {
	action core.AuthorizationAction
	create *core.CreateOperationParams
	err    error
}

func (r *repositoryStub) Authorize(_ context.Context, p core.AuthorizeParams) (core.AuthorizationDecision, error) {
	r.action = p.Action
	return core.AuthorizationDecision{}, r.err
}
func (r *repositoryStub) CreateOperation(_ context.Context, p core.CreateOperationParams) (core.Operation, error) {
	r.create = &p
	return core.Operation{ID: "queued"}, nil
}

type inspectorStub struct{ calls int }

func (i *inspectorStub) InspectPanel(context.Context, string) (agentprotocol.PanelStatusResponse, error) {
	i.calls++
	return agentprotocol.PanelStatusResponse{}, nil
}

func TestPanelRequiresPlatformAuthorizationAndExplicitSingleAttempt(t *testing.T) {
	repo, agent := &repositoryStub{}, &inspectorStub{}
	service := Service{Repository: repo, Agent: agent}
	subject := (core.AuthenticatedSession{Identity: core.Identity{ID: "019c1234-5678-7abc-8def-0123456789ab"}, Session: core.Session{ID: "019c1234-5678-7abc-8def-0123456789ac"}}).AuthorizationSubject()
	input := agentprotocol.PanelIssueRequest{Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true, ConfirmOriginChange: true}
	if _, err := service.Queue(t.Context(), subject, input, "request", "key"); err != nil {
		t.Fatal(err)
	}
	if repo.action != core.AuthorizationPlatformManage || repo.create.ActorID == nil || *repo.create.ActorID != subject.IdentityID() || repo.create.AccountID != nil || repo.create.Kind != operations.PanelIssueKind || repo.create.RetryClass != core.RetryNone || repo.create.MaxAttempts != 1 {
		t.Fatalf("unsafe queue: %#v", repo.create)
	}
	for _, denied := range []error{core.ErrAuthorizationDenied, core.ErrSessionInvalid, core.ErrRecentAuthenticationRequired} {
		repo.err, repo.create = denied, nil
		if _, err := service.Queue(t.Context(), subject, input, "request", "key"); !errors.Is(err, denied) || repo.create != nil {
			t.Fatalf("denied queue: %v %#v", err, repo.create)
		}
		if _, err := service.Status(t.Context(), subject); !errors.Is(err, denied) || agent.calls != 0 {
			t.Fatalf("denied status: %v", err)
		}
	}
	repo.err = nil
	input.ConfirmOriginChange = false
	if _, err := service.Queue(t.Context(), subject, input, "request", "key"); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := service.Status(t.Context(), subject); err != nil || repo.action != core.AuthorizationPlatformView || agent.calls != 1 {
		t.Fatal("status authorization missing")
	}
}
