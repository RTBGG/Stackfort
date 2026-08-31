// SPDX-License-Identifier: AGPL-3.0-or-later

package logworkspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

const (
	logTestAccountID = core.ID("019c1234-5678-7abc-8def-0123456789ab")
	logTestDomainID  = core.ID("019c1234-5678-7abc-8def-0123456789ac")
)

func TestReadAuthorizesAndDerivesDomainAtTheControlPlane(t *testing.T) {
	t.Parallel()
	domainName, _ := core.NormalizeDomainName("example.test")
	username, _ := hostingidentity.UsernameForAccount(string(logTestAccountID))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(logTestAccountID))
	repository := &logRepositoryStub{
		ready: true,
		account: core.HostingAccount{ID: logTestAccountID, UnixIdentity: core.HostingUnixIdentity{
			AccountID: logTestAccountID, Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home,
		}},
		domains: []core.Domain{{ID: logTestDomainID, AccountID: logTestAccountID, Name: domainName, Status: core.DomainActive}},
	}
	agent := &logAgentStub{response: agentprotocol.HostingLogReadResponse{
		Domain: domainName, Kind: agentprotocol.HostingLogAccess, Records: []agentprotocol.HostingLogRecord{},
		RetentionDays: hostinglogs.RetentionDays, MaximumActiveBytes: hostinglogs.MaximumActiveBytes,
		SensitiveRedaction: true,
	}}
	service, _ := New(repository, agent)
	response, err := service.Read(t.Context(), ReadParams{
		AccountID: logTestAccountID, DomainID: logTestDomainID, Kind: agentprotocol.HostingLogAccess,
	})
	if err != nil || response.Domain != domainName || repository.authorization.Action != core.AuthorizationAccountLogsView ||
		repository.authorization.AccountID == nil || *repository.authorization.AccountID != logTestAccountID ||
		agent.request.Domain != domainName || agent.request.Identity.AccountID != string(logTestAccountID) ||
		agent.request.Limit != agentprotocol.MaximumHostingLogEntries || !strings.HasPrefix(agent.key, "hosting-log-read-") {
		t.Fatalf("response=%#v err=%v authorization=%#v request=%#v key=%q", response, err,
			repository.authorization, agent.request, agent.key)
	}
}

func TestReadStopsBeforeAgentForDeniedOrCrossAccountDomain(t *testing.T) {
	t.Parallel()
	repository := &logRepositoryStub{ready: true, authorizeErr: core.ErrAuthorizationDenied}
	agent := &logAgentStub{}
	service, _ := New(repository, agent)
	_, err := service.Read(t.Context(), ReadParams{
		AccountID: logTestAccountID, DomainID: logTestDomainID, Kind: agentprotocol.HostingLogError,
	})
	if !errors.Is(err, core.ErrAuthorizationDenied) || agent.calls != 0 {
		t.Fatalf("denied err=%v calls=%d", err, agent.calls)
	}
	repository.authorizeErr = nil
	repository.account.ID = logTestAccountID
	repository.ready = true
	_, err = service.Read(t.Context(), ReadParams{
		AccountID: logTestAccountID, DomainID: logTestDomainID, Kind: agentprotocol.HostingLogError,
	})
	if !errors.Is(err, core.ErrNotFound) || agent.calls != 0 {
		t.Fatalf("missing domain err=%v calls=%d", err, agent.calls)
	}
}

type logRepositoryStub struct {
	authorization core.AuthorizeParams
	authorizeErr  error
	ready         bool
	account       core.HostingAccount
	domains       []core.Domain
}

func (stub *logRepositoryStub) Authorize(_ context.Context, params core.AuthorizeParams) (core.AuthorizationDecision, error) {
	stub.authorization = params
	return core.AuthorizationDecision{}, stub.authorizeErr
}
func (stub *logRepositoryStub) GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error) {
	return stub.account, nil
}
func (stub *logRepositoryStub) HostingAccountHostReady(context.Context, core.ID) (bool, error) {
	return stub.ready, nil
}
func (stub *logRepositoryStub) ListDomains(context.Context, core.ID, bool) ([]core.Domain, error) {
	return stub.domains, nil
}

type logAgentStub struct {
	response    agentprotocol.HostingLogReadResponse
	wafResponse agentprotocol.WAFEventReadResponse
	err         error
	request     agentprotocol.HostingLogReadRequest
	key         string
	calls       int
}

func (stub *logAgentStub) ReadHostingLogs(
	_ context.Context, key string, request agentprotocol.HostingLogReadRequest,
) (agentprotocol.HostingLogReadResponse, error) {
	stub.calls++
	stub.key, stub.request = key, request
	return stub.response, stub.err
}

func (stub *logAgentStub) ReadWAFEvents(
	_ context.Context, key string, request agentprotocol.WAFEventReadRequest,
) (agentprotocol.WAFEventReadResponse, error) {
	stub.calls++
	stub.key = key
	stub.request = agentprotocol.HostingLogReadRequest{Identity: request.Identity, Domain: request.Domain, Cursor: request.Cursor, Limit: request.Limit}
	return stub.wafResponse, stub.err
}

func TestReadWAFEventsAuthorizesAndReturnsOnlyAgentUnion(t *testing.T) {
	t.Parallel()
	domainName, _ := core.NormalizeDomainName("example.test")
	username, _ := hostingidentity.UsernameForAccount(string(logTestAccountID))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(logTestAccountID))
	repository := &logRepositoryStub{ready: true, account: core.HostingAccount{
		ID: logTestAccountID, UnixIdentity: core.HostingUnixIdentity{AccountID: logTestAccountID,
			Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home},
	}, domains: []core.Domain{{ID: logTestDomainID, AccountID: logTestAccountID, Name: domainName, Status: core.DomainActive}}}
	agent := &logAgentStub{wafResponse: agentprotocol.WAFEventReadResponse{
		Domain: domainName, Events: []agentprotocol.WAFEvent{}, RetentionDays: hostinglogs.RetentionDays,
		MaximumActiveBytes: hostinglogs.MaximumActiveBytes, NativeDataWithheld: true,
	}}
	service, _ := New(repository, agent)
	response, err := service.ReadWAFEvents(t.Context(), WAFReadParams{AccountID: logTestAccountID, DomainID: logTestDomainID})
	if err != nil || response.Domain != domainName || repository.authorization.Action != core.AuthorizationAccountLogsView ||
		agent.request.Domain != domainName || agent.request.Limit != agentprotocol.MaximumWAFEventEntries ||
		!strings.HasPrefix(agent.key, "waf-event-read-") {
		t.Fatalf("response=%#v err=%v authorization=%#v request=%#v key=%q", response, err,
			repository.authorization, agent.request, agent.key)
	}
}
