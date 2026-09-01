// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

func TestRequestValidationIsStrictAndAllowlisted(t *testing.T) {
	t.Parallel()

	valid := validHandshakeRequest()
	if err := ValidateRequest(valid); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"wire version", func(request *Request) { request.ProtocolVersion = 2 }},
		{"request id", func(request *Request) { request.RequestID = "contains space" }},
		{"idempotency key", func(request *Request) { request.IdempotencyKey = strings.Repeat("x", 129) }},
		{"operation", func(request *Request) { request.Operation = Operation("command.run") }},
		{"unexpected correlation", func(request *Request) {
			correlation := validIdentityAuditCorrelation()
			request.Correlation = &correlation
		}},
		{"missing tagged payload", func(request *Request) { request.Handshake = nil }},
		{"version range", func(request *Request) { request.Handshake.MinimumVersion = 3; request.Handshake.MaximumVersion = 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			handshake := *valid.Handshake
			request.Handshake = &handshake
			test.mutate(&request)
			if err := ValidateRequest(request); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
}

func TestDecodeRequestRejectsUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()

	unknown := `{"protocolVersion":1,"requestId":"req-1","idempotencyKey":"idem-1","operation":"protocol.handshake","handshake":{"minimumVersion":1,"maximumVersion":1,"clientBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"}},"command":"id"}`
	if _, err := DecodeRequest(strings.NewReader(unknown)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown field error = %v", err)
	}
	encoded := `{"protocolVersion":1,"requestId":"req-1","idempotencyKey":"idem-1","operation":"protocol.handshake","handshake":{"minimumVersion":1,"maximumVersion":1,"clientBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"}}}`
	if _, err := DecodeRequest(strings.NewReader(encoded + `{}`)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("trailing JSON error = %v", err)
	}
}

func TestSemanticDigestIgnoresTransportCorrelationButBindsAuditContext(t *testing.T) {
	t.Parallel()

	first := validHandshakeRequest()
	second := first
	second.RequestID = "req-2"
	second.IdempotencyKey = "idem-2"
	firstDigest, err := SemanticDigest(first)
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	secondDigest, err := SemanticDigest(second)
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if !bytes.Equal(firstDigest[:], secondDigest[:]) {
		t.Fatal("transport correlation fields changed semantic digest")
	}
	correlation := validIdentityAuditCorrelation()
	second.Correlation = &correlation
	correlatedDigest, err := SemanticDigest(second)
	if err != nil {
		t.Fatalf("correlated digest: %v", err)
	}
	if bytes.Equal(firstDigest[:], correlatedDigest[:]) {
		t.Fatal("audit correlation was not bound into semantic digest")
	}
	second.Correlation = nil
	second.Handshake = &HandshakeRequest{
		MinimumVersion: 2, MaximumVersion: 2, ClientBuild: buildinfo.Current(),
	}
	changedDigest, err := SemanticDigest(second)
	if err != nil {
		t.Fatalf("changed digest: %v", err)
	}
	if bytes.Equal(firstDigest[:], changedDigest[:]) {
		t.Fatal("semantic input did not change digest")
	}
}

func TestPrivilegedMutationAuditCorrelationIsStrict(t *testing.T) {
	t.Parallel()

	valid := validIdentityAuditCorrelation()
	if err := validateCorrelationRequirement(true, &valid); err != nil {
		t.Fatalf("valid identity correlation: %v", err)
	}
	system := AuditCorrelation{
		OperationID: valid.OperationID, ActorKind: ActorSystem, AccountID: valid.AccountID,
	}
	if err := validateCorrelationRequirement(true, &system); err != nil {
		t.Fatalf("valid system correlation: %v", err)
	}
	if err := validateCorrelationRequirement(true, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing mutation correlation error = %v", err)
	}
	if err := validateCorrelationRequirement(false, &valid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unexpected read-only correlation error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AuditCorrelation)
	}{
		{"operation id", func(value *AuditCorrelation) { value.OperationID = "operation-1" }},
		{"operation UUID version", func(value *AuditCorrelation) { value.OperationID = "550e8400-e29b-41d4-a716-446655440000" }},
		{"actor kind", func(value *AuditCorrelation) { value.ActorKind = "administrator" }},
		{"missing identity actor", func(value *AuditCorrelation) { value.ActorID = "" }},
		{"account id", func(value *AuditCorrelation) { value.AccountID = "account-1" }},
		{"system actor id", func(value *AuditCorrelation) { value.ActorKind = ActorSystem }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			correlation := valid
			test.mutate(&correlation)
			if err := ValidateAuditCorrelation(correlation); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("invalid correlation error = %v", err)
			}
		})
	}
}

func TestHostingIdentityStagesAreClosedAndDeletionCannotSelectOne(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	correlation := validIdentityAuditCorrelation()
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "identity-stage-request", IdempotencyKey: "identity-stage-key",
		Operation: OperationReconcileIdentity, Correlation: &correlation,
		ReconcileIdentity: &HostingIdentityRequest{Identity: identity, Stage: HostingIdentityStageBase},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatal(err)
	}
	request.ReconcileIdentity.Stage = "arbitrary"
	if err := ValidateRequest(request); err == nil {
		t.Fatal("arbitrary hosting identity stage was accepted")
	}
	request.Operation = OperationDeleteIdentity
	request.DeleteIdentity = &HostingIdentityRequest{Identity: identity, Stage: HostingIdentityStageRuntime}
	request.ReconcileIdentity = nil
	if err := ValidateRequest(request); err == nil {
		t.Fatal("staged identity deletion was accepted")
	}
}

func TestSupportedOperationsAreCopiedAndHaveExplicitPolicies(t *testing.T) {
	t.Parallel()

	operations := SupportedOperations()
	if len(operations) != 26 || operations[0] != OperationHandshake ||
		operations[1] != OperationInspectCapabilities ||
		operations[2] != OperationReconcileIdentity || operations[3] != OperationDeleteIdentity ||
		operations[4] != OperationReconcileFilesystem || operations[5] != OperationListFiles ||
		operations[6] != OperationReadHostingLogs || operations[7] != OperationReadWAFEvents ||
		operations[8] != OperationInspectCacheMetrics || operations[9] != OperationPurgeCache ||
		operations[10] != OperationReconcileResources || operations[11] != OperationEnsureDocumentRoot ||
		operations[12] != OperationReconcileNGINXBaseline || operations[13] != OperationActivateNGINXSites ||
		operations[14] != OperationReconcileACMEHTTP01 || operations[15] != OperationStageTLSCertificate ||
		operations[16] != OperationInspectPHPPools || operations[17] != OperationReconcilePHPPools ||
		operations[18] != OperationProvisionDatabase || operations[19] != OperationRotateDatabasePassword ||
		operations[20] != OperationDropDatabase || operations[21] != OperationReconcileScheduledJob ||
		operations[22] != OperationPrepareOCIImage || operations[23] != OperationReconcileOCIResources ||
		operations[24] != OperationReconcileOCIDeployment || operations[25] != OperationReadOCIApplicationLogs {
		t.Fatalf("supported operations = %#v", operations)
	}
	operations[0] = "mutated.by.caller"
	if SupportedOperations()[0] != OperationHandshake {
		t.Fatal("caller mutated the operation registry")
	}
	for _, policy := range operationPolicies {
		if !validOperationAccess(policy.access) {
			t.Fatalf("operation %s has no explicit access policy", policy.operation)
		}
	}
}

func TestOCIImagePreparationContractRejectsCallerControlledRuntimeFields(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	spec := ociimage.PrepareSpec{
		Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 1,
		Source: ociapps.Source{Kind: ociapps.SourceImageDigest, ImageReference: "registry.example/app@sha256:" + strings.Repeat("a", 64)},
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "oci-image-request", IdempotencyKey: "oci-image-key",
		Operation: OperationPrepareOCIImage,
		Correlation: &AuditCorrelation{
			OperationID: "019d2eaa-62d0-7f52-8ac7-0aeb932455db", ActorKind: ActorSystem, AccountID: identity.AccountID,
		},
		PrepareOCIImage: &OCIImagePrepareRequest{Spec: spec},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"privileged", "hostPort", "capAdd", "device", "podmanArgs", "socketPath", "workingDirectory"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("OCI image contract exposes %q: %s", forbidden, encoded)
		}
	}
	request.Correlation.AccountID = "019d2eaa-72d0-7f52-8ac7-0aeb932455db"
	if err := ValidateRequest(request); err == nil {
		t.Fatal("cross-account OCI image request was accepted")
	}
}

func TestOCIPrivateResourceContractIsMetadataOnlyAndAccountBound(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	spec := ociresources.Spec{
		Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 1,
		EnvironmentReferences: []ociresources.EnvironmentReference{{
			SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", Environment: "DATABASE_URL", Generation: 2,
		}},
		Volumes: []ociapps.VolumeMount{{
			VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dd", ContainerPath: "/var/lib/app",
		}},
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "oci-resource-request", IdempotencyKey: "oci-resource-key",
		Operation: OperationReconcileOCIResources,
		Correlation: &AuditCorrelation{
			OperationID: "019d2eaa-62d0-7f52-8ac7-0aeb932455db", ActorKind: ActorSystem, AccountID: identity.AccountID,
		},
		ReconcileOCIResources: &OCIResourceReconcileRequest{Spec: spec},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"secretValue", "plaintext", "hostPath", "hostPort", "privileged", "capAdd", "device", "namespace", "podmanArgs",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("OCI resource contract exposes %q: %s", forbidden, encoded)
		}
	}
	request.Correlation.AccountID = "019d2eaa-72d0-7f52-8ac7-0aeb932455db"
	if err := ValidateRequest(request); err == nil {
		t.Fatal("cross-account OCI resource request was accepted")
	}
}

func TestFileListingIsReadOnlyBoundedAndCanonical(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "file-list-request-1", IdempotencyKey: "file-list-key-1",
		Operation: OperationListFiles,
		ListFiles: &FileListRequest{Identity: identity, Path: "public_html", Limit: 50},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid file listing request: %v", err)
	}
	if RequiresAuditCorrelation(OperationListFiles) {
		t.Fatal("read-only file listing unexpectedly requires mutation correlation")
	}
	invalid := request
	payload := *request.ListFiles
	payload.Path = "../etc"
	invalid.ListFiles = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("traversal request error = %v", err)
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		FileListing: &FileListResponse{Path: "public_html", Entries: []FileEntry{
			{Name: ".env", Type: FileEntryRegular, SizeBytes: 12, Mode: 0o600, ModifiedAt: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC), Hidden: true},
			{Name: "assets", Type: FileEntryDirectory, Mode: 0o750, ModifiedAt: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)},
		}},
	}
	if err := ValidateResponse(response, response.RequestID, OperationListFiles); err != nil {
		t.Fatalf("valid file listing response: %v", err)
	}
	response.FileListing.Entries[1].Name = "../escape"
	if err := ValidateResponse(response, response.RequestID, OperationListFiles); err == nil {
		t.Fatal("malformed file listing entry was accepted")
	}
}

func TestMaximumFileListingFitsAgentResponseBoundary(t *testing.T) {
	t.Parallel()
	entries := make([]FileEntry, 0, MaximumFileListingEntries)
	for index := 0; index < MaximumFileListingEntries; index++ {
		entries = append(entries, FileEntry{
			Name: fmt.Sprintf("%03d%s", index, strings.Repeat("x", 252)), Type: FileEntryRegular,
			SizeBytes: ^uint64(0), Mode: 0o7777,
			ModifiedAt: time.Date(2026, 8, 26, 12, 0, 0, 999999999, time.UTC),
		})
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: "file-list-boundary-request",
		FileListing: &FileListResponse{Path: strings.Repeat("p", 255), Entries: entries,
			Next: "9223372036854775807", OmittedEntries: ^uint64(0)},
	}
	if err := ValidateResponse(response, response.RequestID, OperationListFiles); err != nil {
		t.Fatalf("maximum listing validation: %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxResponseBytes {
		t.Fatalf("maximum listing encoded size=%d exceeds %d", len(encoded), MaxResponseBytes)
	}
}

func TestPHPPoolInspectionIsReadOnlyBoundedAndAccountDerived(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "php-inspect-request-1", IdempotencyKey: "php-inspect-key-1",
		Operation:       OperationInspectPHPPools,
		InspectPHPPools: &PHPPoolInspectRequest{Identity: identity, Versions: []string{"8.4"}},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid PHP inspection request: %v", err)
	}
	if RequiresAuditCorrelation(OperationInspectPHPPools) {
		t.Fatal("read-only PHP inspection unexpectedly requires mutation correlation")
	}
	invalid := request
	payload := *request.InspectPHPPools
	payload.Versions = []string{"8.4", "8.4"}
	invalid.InspectPHPPools = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate PHP inspection version error = %v", err)
	}
	memory, cpu, processes := uint64(32<<20), uint64(9_000_000), uint64(2)
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		PHPPoolInspection: &PHPPoolInspectResponse{Pools: []PHPPoolStatus{{
			Version: "8.4", State: PHPPoolActive, MemoryBytes: &memory,
			CPUTimeNanosec: &cpu, Processes: &processes,
		}}},
	}
	if err := ValidateResponse(response, response.RequestID, OperationInspectPHPPools); err != nil {
		t.Fatalf("valid PHP inspection response: %v", err)
	}
	response.PHPPoolInspection.Pools[0].State = "unknown"
	if err := ValidateResponse(response, response.RequestID, OperationInspectPHPPools); err == nil {
		t.Fatal("invalid PHP pool state was accepted")
	}
}

func TestPHPPoolMutationRequiresClosedAccountIntent(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	correlation := validIdentityAuditCorrelation()
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "php-request-1", IdempotencyKey: "php-key-1",
		Operation: OperationReconcilePHPPools, Correlation: &correlation,
		ReconcilePHPPools: &PHPPoolSetRequest{Pools: phpruntime.PoolSetSpec{
			Identity: identity, Versions: []string{"8.4"}, MaxChildren: 4, MemoryLimitMiB: 128,
		}},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid PHP pool request: %v", err)
	}
	invalid := request
	payload := *request.ReconcilePHPPools
	payload.Pools.Versions = []string{"8.4;id"}
	invalid.ReconcilePHPPools = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("untrusted PHP version error = %v", err)
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		PHPPools: &PHPPoolSetResponse{
			Versions: []string{"8.4"}, Changed: true, Active: true,
			Capability: Capability{Status: CapabilityAvailable},
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationReconcilePHPPools); err != nil {
		t.Fatalf("valid PHP pool response: %v", err)
	}
	if err := ValidateResponse(response, response.RequestID, OperationActivateNGINXSites); err == nil {
		t.Fatal("PHP response was accepted for a different operation")
	}
}

func TestDatabaseProvisioningRequiresDerivedTenantNamesAndBoundedSecret(t *testing.T) {
	t.Parallel()
	correlation := validIdentityAuditCorrelation()
	accountPrefix := "sf_" + strings.ReplaceAll(correlation.AccountID, "-", "") + "_"
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "database-request-1", IdempotencyKey: "database-key-1",
		Operation: OperationProvisionDatabase, Correlation: &correlation,
		ProvisionDatabase: &DatabaseProvisionRequest{
			DatabaseAlias: "application", DatabaseName: accountPrefix + "application",
			UserAlias: "application", Username: accountPrefix + "application",
			Host: "localhost", Password: []byte("0123456789abcdefghijklmn"), CreateUser: true,
			Preset: DatabaseGrantReadWrite,
		},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid database provisioning request: %v", err)
	}
	if !RequiresAuditCorrelation(OperationProvisionDatabase) {
		t.Fatal("database provisioning does not require audit correlation")
	}
	invalid := request
	payload := *request.ProvisionDatabase
	payload.DatabaseName = "other_account_application"
	invalid.ProvisionDatabase = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("substituted physical name error = %v", err)
	}
	payload = *request.ProvisionDatabase
	payload.Host = "%"
	invalid.ProvisionDatabase = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("wildcard database host error = %v", err)
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		Database: &DatabaseProvisionResponse{
			DatabaseName: request.ProvisionDatabase.DatabaseName,
			Username:     request.ProvisionDatabase.Username, Host: "localhost",
			Preset: DatabaseGrantReadWrite, Active: true,
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationProvisionDatabase); err != nil {
		t.Fatalf("valid database provisioning response: %v", err)
	}
}

func TestDatabasePasswordRotationRequiresOwnedLocalPrincipalAndBoundedSecret(t *testing.T) {
	t.Parallel()
	correlation := validIdentityAuditCorrelation()
	prefix := "sf_" + strings.ReplaceAll(correlation.AccountID, "-", "") + "_"
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "database-rotate-request", IdempotencyKey: "database-rotate-key",
		Operation: OperationRotateDatabasePassword, Correlation: &correlation,
		RotateDatabasePassword: &DatabasePasswordRotateRequest{
			UserAlias: "application", Username: prefix + "application", Host: "localhost",
			Password: []byte("0123456789abcdefghijklmn"),
		},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid database password rotation request: %v", err)
	}
	if !RequiresAuditCorrelation(OperationRotateDatabasePassword) {
		t.Fatal("database password rotation lacks audit correlation")
	}
	invalid := request
	payload := *request.RotateDatabasePassword
	payload.Username = "root"
	invalid.RotateDatabasePassword = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unowned rotation user error = %v", err)
	}
	payload = *request.RotateDatabasePassword
	payload.Password = []byte("short")
	invalid.RotateDatabasePassword = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("short rotation secret error = %v", err)
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		DatabasePasswordRotation: &DatabasePasswordRotateResponse{
			Username: request.RotateDatabasePassword.Username, Host: "localhost", Active: true,
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationRotateDatabasePassword); err != nil {
		t.Fatalf("valid database password rotation response: %v", err)
	}
}

func TestDatabaseDeletionRequiresDerivedTargetAndClosedGrantList(t *testing.T) {
	t.Parallel()
	correlation := validIdentityAuditCorrelation()
	prefix := "sf_" + strings.ReplaceAll(correlation.AccountID, "-", "") + "_"
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "database-drop-request", IdempotencyKey: "database-drop-key",
		Operation: OperationDropDatabase, Correlation: &correlation,
		DropDatabase: &DatabaseDropRequest{
			Kind: DatabaseDropDatabase, Alias: "records", Name: prefix + "records", Grants: []DatabaseDropGrant{{
				UserAlias: "application", Username: prefix + "application", Host: "localhost",
				Preset: DatabaseGrantReadWrite,
			}},
		},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid drop request: %v", err)
	}
	if !RequiresAuditCorrelation(OperationDropDatabase) {
		t.Fatal("database deletion lacks audit correlation")
	}
	invalid := request
	payload := *request.DropDatabase
	payload.Name = "unowned"
	invalid.DropDatabase = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unowned drop error = %v", err)
	}
	response := Response{ProtocolVersion: WireVersion, RequestID: request.RequestID,
		DatabaseDrop: &DatabaseDropResponse{Kind: DatabaseDropDatabase, Name: prefix + "records", Deleted: true}}
	if err := ValidateResponse(response, response.RequestID, OperationDropDatabase); err != nil {
		t.Fatalf("valid drop response: %v", err)
	}
}

func TestHostingIdentityMutationsRequireMatchingCorrelationAndTypedPayload(t *testing.T) {
	t.Parallel()
	spec := validHostingIdentitySpec()
	correlation := validIdentityAuditCorrelation()
	valid := Request{
		ProtocolVersion: WireVersion, RequestID: "identity-req-1", IdempotencyKey: "identity-key-1",
		Operation: OperationReconcileIdentity, Correlation: &correlation,
		ReconcileIdentity: &HostingIdentityRequest{Identity: spec, Stage: HostingIdentityStageBase},
	}
	if err := ValidateRequest(valid); err != nil {
		t.Fatalf("valid mutation: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"missing correlation", func(request *Request) { request.Correlation = nil }},
		{"mismatched account", func(request *Request) { request.Correlation.AccountID = request.Correlation.ActorID }},
		{"substituted username", func(request *Request) { request.ReconcileIdentity.Identity.Username = "root" }},
		{"wrong payload", func(request *Request) { request.DeleteIdentity = request.ReconcileIdentity }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			copiedCorrelation := *valid.Correlation
			copiedPayload := *valid.ReconcileIdentity
			request.Correlation, request.ReconcileIdentity = &copiedCorrelation, &copiedPayload
			test.mutate(&request)
			if err := ValidateRequest(request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHostingIdentityMutationResponsesAreOperationSpecific(t *testing.T) {
	t.Parallel()
	reconciled := Response{
		ProtocolVersion: WireVersion, RequestID: "identity-response-1",
		HostingIdentity: &HostingIdentityResponse{Changed: true, UserCreated: true},
	}
	if err := ValidateResponse(reconciled, reconciled.RequestID, OperationReconcileIdentity); err != nil {
		t.Fatalf("reconcile response: %v", err)
	}
	if err := ValidateResponse(reconciled, reconciled.RequestID, OperationDeleteIdentity); err == nil {
		t.Fatal("reconcile fields were accepted for deletion")
	}
	deleted := Response{
		ProtocolVersion: WireVersion, RequestID: "identity-response-2",
		HostingIdentity: &HostingIdentityResponse{Changed: true, UserDeleted: true},
	}
	if err := ValidateResponse(deleted, deleted.RequestID, OperationDeleteIdentity); err != nil {
		t.Fatalf("delete response: %v", err)
	}
}

func TestHostingFilesystemMutationsRequireCompleteTypedIntent(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	correlation := validIdentityAuditCorrelation()
	storage := hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID, ByteLimit: 10 << 30, InodeLimit: 100000,
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "filesystem-request-1", IdempotencyKey: "filesystem-key-1",
		Operation: OperationReconcileFilesystem, Correlation: &correlation,
		ReconcileFilesystem: &HostingFilesystemRequest{Storage: storage},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid filesystem request: %v", err)
	}
	invalidProject := request
	invalidPayload := *request.ReconcileFilesystem
	invalidPayload.Storage.ProjectID++
	invalidProject.ReconcileFilesystem = &invalidPayload
	if err := ValidateRequest(invalidProject); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("project mismatch error = %v", err)
	}

	document := Request{
		ProtocolVersion: WireVersion, RequestID: "document-request-1", IdempotencyKey: "document-key-1",
		Operation: OperationEnsureDocumentRoot, Correlation: &correlation,
		EnsureDocumentRoot: &DocumentRootRequest{
			Identity: identity, RelativePath: "domains/example.test/public", Access: DocumentRootAccessStatic,
		},
	}
	if err := ValidateRequest(document); err != nil {
		t.Fatalf("valid document-root request: %v", err)
	}
	document.EnsureDocumentRoot.RelativePath = "public_html/../private"
	if err := ValidateRequest(document); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("traversal error = %v", err)
	}
}

func TestHostingFilesystemResponsesAreCapabilityLabelled(t *testing.T) {
	t.Parallel()
	response := Response{
		ProtocolVersion: WireVersion, RequestID: "filesystem-response-1",
		HostingFilesystem: &HostingFilesystemResponse{
			ProjectID: hostingidentity.MinimumID, ProjectAssigned: true,
			DirectoriesCreated: []string{"applications", "backups", "domains"},
			QuotaApplied:       true, Capability: Capability{Status: CapabilityAvailable},
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationReconcileFilesystem); err != nil {
		t.Fatalf("valid filesystem response: %v", err)
	}
	quotaError := Response{
		ProtocolVersion: WireVersion, RequestID: "filesystem-response-2",
		Error: &ResponseError{
			Code: ErrorQuotaUnavailable, Message: "Project quota is unavailable.",
			Capability: &Capability{Status: CapabilityUnavailable, ReasonCode: "project-quota-not-mounted"},
		},
	}
	if err := ValidateResponse(quotaError, quotaError.RequestID, OperationReconcileFilesystem); err != nil {
		t.Fatalf("valid quota error: %v", err)
	}
	quotaError.Error.Capability = nil
	if err := ValidateResponse(quotaError, quotaError.RequestID, OperationReconcileFilesystem); err == nil {
		t.Fatal("unlabelled quota error was accepted")
	}
}

func TestHostingResourceMutationPreservesUnlimitedAndExplicitZeroSemantics(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	correlation := validIdentityAuditCorrelation()
	resources := hostingresources.Spec{
		Identity:        identity,
		CPUQuotaPercent: hostingresources.OptionalUint64{Set: true, Value: 250},
		MemoryBytes:     hostingresources.OptionalUint64{Set: true, Value: 512 << 20},
		SwapBytes:       hostingresources.OptionalUint64{Set: true, Value: 0},
		ProcessLimit:    hostingresources.OptionalUint64{Set: true, Value: 64},
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "resources-request-1", IdempotencyKey: "resources-key-1",
		Operation: OperationReconcileResources, Correlation: &correlation,
		ReconcileResources: &HostingResourcesRequest{Resources: resources},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid resources request: %v", err)
	}
	invalid := request
	invalidPayload := *request.ReconcileResources
	invalidPayload.Resources.MemoryBytes = hostingresources.OptionalUint64{Value: 1}
	invalid.ReconcileResources = &invalidPayload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ambiguous resource limit error = %v", err)
	}

	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		HostingResources: &HostingResourcesResponse{
			UID: identity.UID, UnitName: "stackfort-accounts-200000.slice",
			ControlGroup:  "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200000.slice",
			LimitsApplied: true, Capability: Capability{Status: CapabilityAvailable},
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationReconcileResources); err != nil {
		t.Fatalf("valid resources response: %v", err)
	}
	response.HostingResources.ControlGroup = "/system.slice"
	if err := ValidateResponse(response, response.RequestID, OperationReconcileResources); err == nil {
		t.Fatal("accepted a resource response outside the managed hierarchy")
	}

	unavailable := Response{
		ProtocolVersion: WireVersion, RequestID: "resources-response-2",
		Error: &ResponseError{
			Code: ErrorResourceControlUnavailable, Message: "Resource control is unavailable.",
			Capability: &Capability{Status: CapabilityUnavailable, ReasonCode: "cgroup-controller-pids-missing"},
		},
	}
	if err := ValidateResponse(unavailable, unavailable.RequestID, OperationReconcileResources); err != nil {
		t.Fatalf("valid unavailable response: %v", err)
	}
}

func TestResponseValidationRejectsInvalidAgentVersionRange(t *testing.T) {
	t.Parallel()

	valid := Response{
		ProtocolVersion: WireVersion,
		RequestID:       "req-1",
		Handshake: &HandshakeResponse{
			SelectedVersion: 1, AgentMinimumVersion: 1, AgentMaximumVersion: 1,
			AgentBuild: buildinfo.Current(), SupportedOperations: []Operation{OperationHandshake},
		},
	}
	if err := ValidateResponse(valid, valid.RequestID, OperationHandshake); err != nil {
		t.Fatalf("valid response: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*HandshakeResponse)
	}{
		{"zero minimum", func(response *HandshakeResponse) { response.AgentMinimumVersion = 0 }},
		{"reversed range", func(response *HandshakeResponse) { response.AgentMinimumVersion = 2 }},
		{"unbounded maximum", func(response *HandshakeResponse) { response.AgentMaximumVersion = 1_000_001 }},
		{"selection below range", func(response *HandshakeResponse) { response.SelectedVersion = 0 }},
		{"selection above range", func(response *HandshakeResponse) { response.SelectedVersion = 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := valid
			handshake := *valid.Handshake
			response.Handshake = &handshake
			test.mutate(response.Handshake)
			if err := ValidateResponse(response, response.RequestID, OperationHandshake); err == nil {
				t.Fatal("invalid response was accepted")
			}
		})
	}
}

func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"protocolVersion":1,"requestId":"fuzz-1","idempotencyKey":"fuzz-key-1","operation":"protocol.handshake","handshake":{"minimumVersion":1,"maximumVersion":1,"clientBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"}}}`))
	f.Add([]byte(`{"protocolVersion":1,"requestId":"fuzz-2","idempotencyKey":"fuzz-key-2","operation":"host.capabilities.inspect","inspectCapabilities":{}}`))
	f.Add([]byte(`{"protocolVersion":1,"requestId":"fuzz-3","idempotencyKey":"fuzz-key-3","operation":"hosting.identity.reconcile","correlation":{"operationId":"019c1234-5678-7abc-8def-0123456789ab","actorKind":"system","accountId":"019c1234-5678-7abc-8def-0123456789ad"},"reconcileIdentity":{"identity":{"accountId":"019c1234-5678-7abc-8def-0123456789ad","username":"sf_3456787abc8def0123456789ad","uid":200000,"gid":200000,"homeDirectory":"/srv/hosting/accounts/019c1234-5678-7abc-8def-0123456789ad"}}}`))
	f.Add([]byte(`{"operation":"command.run","command":"rm"}`))
	f.Add([]byte{0xff, 0x00, 0x7b})
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = DecodeRequest(bytes.NewReader(input))
	})
}

func validHostingIdentitySpec() hostingidentity.Spec {
	accountID := validIdentityAuditCorrelation().AccountID
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}
}

func validHandshakeRequest() Request {
	return Request{
		ProtocolVersion: WireVersion, RequestID: "req-1", IdempotencyKey: "idem-1",
		Operation: OperationHandshake,
		Handshake: &HandshakeRequest{
			MinimumVersion: MinimumVersion, MaximumVersion: MaximumVersion,
			ClientBuild: buildinfo.Current(),
		},
	}
}

func validIdentityAuditCorrelation() AuditCorrelation {
	return AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab",
		ActorKind:   ActorIdentity,
		ActorID:     "019c1234-5678-7abc-8def-0123456789ac",
		AccountID:   "019c1234-5678-7abc-8def-0123456789ad",
	}
}
