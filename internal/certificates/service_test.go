// SPDX-License-Identifier: AGPL-3.0-or-later

package certificates

import (
	"context"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
)

func TestAutomaticWorkQueuesBoundedIdempotentIssueAndRenewal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	repository := &certificateRepositoryStub{
		pending: []core.PendingTLSCertificateIssuance{{
			AccountID:   "019c1234-5678-7abc-8def-0123456789ab",
			DomainID:    "019c1234-5678-7abc-8def-0123456789ac",
			Environment: core.ACMELetsEncryptProduction,
		}},
		renewals: []core.DueTLSCertificateRenewal{{
			AccountID:     "019c1234-5678-7abc-8def-0123456789ab",
			DomainID:      "019c1234-5678-7abc-8def-0123456789ac",
			CertificateID: "019c1234-5678-7abc-8def-0123456789ae",
			Environment:   core.ACMELetsEncryptProduction, NextRenewalAt: now.Add(-time.Minute),
		}},
	}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	queued, err := service.QueueAutomaticWork(context.Background(), 10)
	if err != nil || queued != 2 || len(repository.created) != 2 {
		t.Fatalf("queued=%d operations=%#v err=%v", queued, repository.created, err)
	}
	for _, operation := range repository.created {
		if operation.Kind != operations.TLSCertificateLifecycleKind || operation.RetryClass != core.RetrySafe ||
			operation.MaxAttempts != 4 || operation.ActorID != nil || operation.IdempotencyKey == "" {
			t.Fatalf("automatic operation = %#v", operation)
		}
	}
	if repository.created[0].Payload["replacesCertificateId"] != nil {
		t.Fatalf("initial issuance payload = %#v", repository.created[0].Payload)
	}
	if repository.created[1].Payload["replacesCertificateId"] != string(repository.renewals[0].CertificateID) {
		t.Fatalf("renewal payload = %#v", repository.created[1].Payload)
	}
}

type certificateRepositoryStub struct {
	pending  []core.PendingTLSCertificateIssuance
	renewals []core.DueTLSCertificateRenewal
	created  []core.CreateOperationParams
}

func (stub *certificateRepositoryStub) Authorize(
	context.Context,
	core.AuthorizeParams,
) (core.AuthorizationDecision, error) {
	return core.AuthorizationDecision{}, nil
}

func (stub *certificateRepositoryStub) CreateOperation(
	_ context.Context,
	params core.CreateOperationParams,
) (core.Operation, error) {
	stub.created = append(stub.created, params)
	return core.Operation{ID: "019c1234-5678-7abc-8def-0123456789af", Status: core.OperationPending}, nil
}

func (stub *certificateRepositoryStub) ListTLSCertificates(
	context.Context,
	core.ID,
	core.ID,
) ([]core.TLSCertificate, error) {
	return nil, nil
}

func (stub *certificateRepositoryStub) ListPendingTLSCertificateIssuances(
	context.Context,
	int,
) ([]core.PendingTLSCertificateIssuance, error) {
	return stub.pending, nil
}

func (stub *certificateRepositoryStub) ListDueTLSCertificateRenewals(
	context.Context,
	time.Time,
	int,
) ([]core.DueTLSCertificateRenewal, error) {
	return stub.renewals, nil
}
