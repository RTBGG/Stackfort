// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentprotocol defines the bounded, versioned wire contract shared by
// the unprivileged control API and the privileged local host agent.
package agentprotocol

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"github.com/google/uuid"
)

const (
	Endpoint          = "/rpc/v1"
	DefaultSocketPath = "/run/stackfort/agent.sock"
	MediaType         = "application/json"
	WireVersion       = 1
	MinimumVersion    = 1
	MaximumVersion    = 1
	MaxRequestBytes   = 2 << 20
	MaxResponseBytes  = 512 << 10
)

type Operation string

const (
	OperationHandshake              Operation = "protocol.handshake"
	OperationInspectCapabilities    Operation = "host.capabilities.inspect"
	OperationReconcileIdentity      Operation = "hosting.identity.reconcile"
	OperationDeleteIdentity         Operation = "hosting.identity.delete"
	OperationReconcileFilesystem    Operation = "hosting.filesystem.reconcile"
	OperationListFiles              Operation = "hosting.files.list"
	OperationReadHostingLogs        Operation = "hosting.logs.read"
	OperationReadWAFEvents          Operation = "hosting.waf-events.read"
	OperationInspectCacheMetrics    Operation = "hosting.cache.metrics"
	OperationPurgeCache             Operation = "hosting.cache.purge"
	OperationReconcileResources     Operation = "hosting.resources.reconcile"
	OperationEnsureDocumentRoot     Operation = "hosting.document-root.ensure"
	OperationReconcileNGINXBaseline Operation = "web.nginx-baseline.reconcile"
	OperationActivateNGINXSites     Operation = "web.nginx-sites.activate"
	OperationReconcileACMEHTTP01    Operation = "tls.acme-http01.reconcile"
	OperationStageTLSCertificate    Operation = "tls.certificate.stage"
	OperationInspectPHPPools        Operation = "php.fpm-pools.inspect"
	OperationReconcilePHPPools      Operation = "php.fpm-pools.reconcile"
	OperationProvisionDatabase      Operation = "database.provision"
	OperationRotateDatabasePassword Operation = "database.password.rotate"
	OperationDropDatabase           Operation = "database.drop"
	OperationReconcileScheduledJob  Operation = "hosting.jobs.reconcile"
	OperationPrepareOCIImage        Operation = "oci.image.prepare"
	OperationReconcileOCIResources  Operation = "oci.resources.reconcile"
	OperationReconcileOCIDeployment Operation = "oci.deployment.reconcile"
	OperationReadOCIApplicationLogs Operation = "oci.logs.read"
	OperationInspectPlatformUpdate  Operation = "platform.update.inspect"
	OperationStartPlatformUpdate    Operation = "platform.update.start"
)

type ActorKind string

const (
	ActorIdentity ActorKind = "identity"
	ActorSystem   ActorKind = "system"
)

// AuditCorrelation binds a privileged agent mutation to the already persisted
// control-plane operation and the actor that authorized or initiated it.
type AuditCorrelation struct {
	OperationID string    `json:"operationId"`
	ActorKind   ActorKind `json:"actorKind"`
	ActorID     string    `json:"actorId,omitempty"`
	AccountID   string    `json:"accountId,omitempty"`
}

type operationAccess uint8

const (
	operationAccessUnset operationAccess = iota
	operationProtocol
	operationReadOnly
	operationPrivilegedMutation
)

type operationPolicy struct {
	operation Operation
	access    operationAccess
}

var operationPolicies = [...]operationPolicy{
	{operation: OperationHandshake, access: operationProtocol},
	{operation: OperationInspectCapabilities, access: operationReadOnly},
	{operation: OperationReconcileIdentity, access: operationPrivilegedMutation},
	{operation: OperationDeleteIdentity, access: operationPrivilegedMutation},
	{operation: OperationReconcileFilesystem, access: operationPrivilegedMutation},
	{operation: OperationListFiles, access: operationReadOnly},
	{operation: OperationReadHostingLogs, access: operationReadOnly},
	{operation: OperationReadWAFEvents, access: operationReadOnly},
	{operation: OperationInspectCacheMetrics, access: operationReadOnly},
	{operation: OperationPurgeCache, access: operationPrivilegedMutation},
	{operation: OperationReconcileResources, access: operationPrivilegedMutation},
	{operation: OperationEnsureDocumentRoot, access: operationPrivilegedMutation},
	{operation: OperationReconcileNGINXBaseline, access: operationPrivilegedMutation},
	{operation: OperationActivateNGINXSites, access: operationPrivilegedMutation},
	{operation: OperationReconcileACMEHTTP01, access: operationPrivilegedMutation},
	{operation: OperationStageTLSCertificate, access: operationPrivilegedMutation},
	{operation: OperationInspectPHPPools, access: operationReadOnly},
	{operation: OperationReconcilePHPPools, access: operationPrivilegedMutation},
	{operation: OperationProvisionDatabase, access: operationPrivilegedMutation},
	{operation: OperationRotateDatabasePassword, access: operationPrivilegedMutation},
	{operation: OperationDropDatabase, access: operationPrivilegedMutation},
	{operation: OperationReconcileScheduledJob, access: operationPrivilegedMutation},
	{operation: OperationPrepareOCIImage, access: operationPrivilegedMutation},
	{operation: OperationReconcileOCIResources, access: operationPrivilegedMutation},
	{operation: OperationReconcileOCIDeployment, access: operationPrivilegedMutation},
	{operation: OperationReadOCIApplicationLogs, access: operationReadOnly},
	{operation: OperationInspectPlatformUpdate, access: operationReadOnly},
	{operation: OperationStartPlatformUpdate, access: operationPrivilegedMutation},
}

type ErrorCode string

const (
	ErrorInvalidRequest             ErrorCode = "invalid_request"
	ErrorRequestTooLarge            ErrorCode = "request_too_large"
	ErrorUnsupportedMediaType       ErrorCode = "unsupported_media_type"
	ErrorIncompatibleProtocol       ErrorCode = "incompatible_protocol"
	ErrorUnsupportedOperation       ErrorCode = "unsupported_operation"
	ErrorIdempotencyConflict        ErrorCode = "idempotency_conflict"
	ErrorIdentityConflict           ErrorCode = "identity_conflict"
	ErrorOCIRuntimeUnavailable      ErrorCode = "oci_runtime_unavailable"
	ErrorArchiveRequired            ErrorCode = "archive_required"
	ErrorFilesystemConflict         ErrorCode = "filesystem_conflict"
	ErrorFilesystemMigration        ErrorCode = "filesystem_migration_required"
	ErrorFileNotFound               ErrorCode = "file_not_found"
	ErrorFileConflict               ErrorCode = "file_conflict"
	ErrorFileUnavailable            ErrorCode = "file_listing_unavailable"
	ErrorFileDownloadUnavailable    ErrorCode = "file_download_unavailable"
	ErrorFileDownloadTooLarge       ErrorCode = "file_download_too_large"
	ErrorFileRangeNotSatisfiable    ErrorCode = "file_range_not_satisfiable"
	ErrorFileDownloadBusy           ErrorCode = "file_download_busy"
	ErrorFileQuotaExceeded          ErrorCode = "file_quota_exceeded"
	ErrorHostingLogNotFound         ErrorCode = "hosting_log_not_found"
	ErrorHostingLogConflict         ErrorCode = "hosting_log_conflict"
	ErrorHostingLogUnavailable      ErrorCode = "hosting_log_unavailable"
	ErrorBackupNotFound             ErrorCode = "backup_not_found"
	ErrorBackupConflict             ErrorCode = "backup_conflict"
	ErrorBackupUnavailable          ErrorCode = "backup_unavailable"
	ErrorBackupTooLarge             ErrorCode = "backup_too_large"
	ErrorBackupBusy                 ErrorCode = "backup_busy"
	ErrorBackupIntegrity            ErrorCode = "backup_integrity_failed"
	ErrorBackupQuotaExceeded        ErrorCode = "backup_repository_quota_exceeded"
	ErrorQuotaUnavailable           ErrorCode = "quota_unavailable"
	ErrorResourceControlConflict    ErrorCode = "resource_control_conflict"
	ErrorResourceControlUnavailable ErrorCode = "resource_control_unavailable"
	ErrorNGINXConflict              ErrorCode = "nginx_conflict"
	ErrorNGINXUnavailable           ErrorCode = "nginx_unavailable"
	ErrorNGINXValidation            ErrorCode = "nginx_validation_failed"
	ErrorNGINXActivation            ErrorCode = "nginx_activation_failed"
	ErrorNGINXHealthCheck           ErrorCode = "nginx_health_check_failed"
	ErrorACMEHTTP01Conflict         ErrorCode = "acme_http01_conflict"
	ErrorACMEHTTP01Unavailable      ErrorCode = "acme_http01_unavailable"
	ErrorTLSCertificateConflict     ErrorCode = "tls_certificate_conflict"
	ErrorTLSCertificateUnavailable  ErrorCode = "tls_certificate_unavailable"
	ErrorPHPConflict                ErrorCode = "php_conflict"
	ErrorPHPUnavailable             ErrorCode = "php_unavailable"
	ErrorPHPValidation              ErrorCode = "php_validation_failed"
	ErrorPHPActivation              ErrorCode = "php_activation_failed"
	ErrorDatabaseConflict           ErrorCode = "database_conflict"
	ErrorDatabaseUnavailable        ErrorCode = "database_unavailable"
	ErrorDatabaseValidation         ErrorCode = "database_validation_failed"
	ErrorDatabaseMutation           ErrorCode = "database_mutation_failed"
	ErrorScheduledJobInvalid        ErrorCode = "scheduled_job_invalid"
	ErrorScheduledJobNotFound       ErrorCode = "scheduled_job_script_not_found"
	ErrorScheduledJobConflict       ErrorCode = "scheduled_job_conflict"
	ErrorScheduledJobUnavailable    ErrorCode = "scheduled_job_unavailable"
	ErrorCacheConflict              ErrorCode = "cache_conflict"
	ErrorCacheUnavailable           ErrorCode = "cache_unavailable"
	ErrorOCIImageInvalid            ErrorCode = "oci_image_invalid"
	ErrorOCIImageUnavailable        ErrorCode = "oci_image_unavailable"
	ErrorOCIImageRejected           ErrorCode = "oci_image_rejected"
	ErrorOCIResourceInvalid         ErrorCode = "oci_resource_invalid"
	ErrorOCIResourceConflict        ErrorCode = "oci_resource_conflict"
	ErrorOCIResourceUnavailable     ErrorCode = "oci_resource_unavailable"
	ErrorOCIDeploymentInvalid       ErrorCode = "oci_deployment_invalid"
	ErrorOCIDeploymentConflict      ErrorCode = "oci_deployment_conflict"
	ErrorOCIDeploymentUnhealthy     ErrorCode = "oci_deployment_unhealthy"
	ErrorOCIDeploymentUnavailable   ErrorCode = "oci_deployment_unavailable"
	ErrorPlatformUpdateInvalid      ErrorCode = "platform_update_invalid"
	ErrorPlatformUpdateConflict     ErrorCode = "platform_update_conflict"
	ErrorPlatformUpdateUnavailable  ErrorCode = "platform_update_unavailable"
	ErrorMutationFailed             ErrorCode = "mutation_failed"
	ErrorInternal                   ErrorCode = "internal_error"
)

var (
	ErrInvalidRequest         = errors.New("invalid agent protocol request")
	ErrUnsupportedWireVersion = errors.New("unsupported agent protocol wire version")
	ErrUnsupportedOperation   = errors.New("unsupported agent protocol operation")
	boundedIdentifierPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	validUnitNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@:-]*$`)
)

type Request struct {
	ProtocolVersion        int                            `json:"protocolVersion"`
	RequestID              string                         `json:"requestId"`
	IdempotencyKey         string                         `json:"idempotencyKey"`
	Operation              Operation                      `json:"operation"`
	Correlation            *AuditCorrelation              `json:"correlation,omitempty"`
	Handshake              *HandshakeRequest              `json:"handshake,omitempty"`
	InspectCapabilities    *InspectCapabilitiesRequest    `json:"inspectCapabilities,omitempty"`
	ReconcileIdentity      *HostingIdentityRequest        `json:"reconcileIdentity,omitempty"`
	DeleteIdentity         *HostingIdentityRequest        `json:"deleteIdentity,omitempty"`
	ReconcileFilesystem    *HostingFilesystemRequest      `json:"reconcileFilesystem,omitempty"`
	ListFiles              *FileListRequest               `json:"listFiles,omitempty"`
	ReadHostingLogs        *HostingLogReadRequest         `json:"readHostingLogs,omitempty"`
	ReadWAFEvents          *WAFEventReadRequest           `json:"readWafEvents,omitempty"`
	InspectCacheMetrics    *CacheMetricsRequest           `json:"inspectCacheMetrics,omitempty"`
	PurgeCache             *CachePurgeRequest             `json:"purgeCache,omitempty"`
	ReconcileResources     *HostingResourcesRequest       `json:"reconcileResources,omitempty"`
	EnsureDocumentRoot     *DocumentRootRequest           `json:"ensureDocumentRoot,omitempty"`
	ReconcileNGINXBaseline *NGINXBaselineRequest          `json:"reconcileNginxBaseline,omitempty"`
	ActivateNGINXSites     *NGINXActivationRequest        `json:"activateNginxSites,omitempty"`
	ReconcileACMEHTTP01    *ACMEHTTP01Request             `json:"reconcileAcmeHttp01,omitempty"`
	StageTLSCertificate    *TLSCertificateStageRequest    `json:"stageTlsCertificate,omitempty"`
	InspectPHPPools        *PHPPoolInspectRequest         `json:"inspectPhpPools,omitempty"`
	ReconcilePHPPools      *PHPPoolSetRequest             `json:"reconcilePhpPools,omitempty"`
	ProvisionDatabase      *DatabaseProvisionRequest      `json:"provisionDatabase,omitempty"`
	RotateDatabasePassword *DatabasePasswordRotateRequest `json:"rotateDatabasePassword,omitempty"`
	DropDatabase           *DatabaseDropRequest           `json:"dropDatabase,omitempty"`
	ReconcileScheduledJob  *ScheduledJobReconcileRequest  `json:"reconcileScheduledJob,omitempty"`
	PrepareOCIImage        *OCIImagePrepareRequest        `json:"prepareOciImage,omitempty"`
	ReconcileOCIResources  *OCIResourceReconcileRequest   `json:"reconcileOciResources,omitempty"`
	ReconcileOCIDeployment *OCIDeploymentRequest          `json:"reconcileOciDeployment,omitempty"`
	ReadOCIApplicationLogs *OCIApplicationLogReadRequest  `json:"readOciApplicationLogs,omitempty"`
	InspectPlatformUpdate  *PlatformUpdateInspectRequest  `json:"inspectPlatformUpdate,omitempty"`
	StartPlatformUpdate    *PlatformUpdateStartRequest    `json:"startPlatformUpdate,omitempty"`
}

type HandshakeRequest struct {
	MinimumVersion int            `json:"minimumVersion"`
	MaximumVersion int            `json:"maximumVersion"`
	ClientBuild    buildinfo.Info `json:"clientBuild"`
}

type Response struct {
	ProtocolVersion          int                             `json:"protocolVersion"`
	RequestID                string                          `json:"requestId"`
	Handshake                *HandshakeResponse              `json:"handshake,omitempty"`
	Capabilities             *CapabilityReport               `json:"capabilities,omitempty"`
	HostingIdentity          *HostingIdentityResponse        `json:"hostingIdentity,omitempty"`
	HostingFilesystem        *HostingFilesystemResponse      `json:"hostingFilesystem,omitempty"`
	FileListing              *FileListResponse               `json:"fileListing,omitempty"`
	HostingLogs              *HostingLogReadResponse         `json:"hostingLogs,omitempty"`
	WAFEvents                *WAFEventReadResponse           `json:"wafEvents,omitempty"`
	CacheMetrics             *CacheMetricsResponse           `json:"cacheMetrics,omitempty"`
	CachePurge               *CachePurgeResponse             `json:"cachePurge,omitempty"`
	HostingResources         *HostingResourcesResponse       `json:"hostingResources,omitempty"`
	DocumentRoot             *DocumentRootResponse           `json:"documentRoot,omitempty"`
	NGINXBaseline            *NGINXBaselineResponse          `json:"nginxBaseline,omitempty"`
	NGINXActivation          *NGINXActivationResponse        `json:"nginxActivation,omitempty"`
	ACMEHTTP01               *ACMEHTTP01Response             `json:"acmeHttp01,omitempty"`
	TLSCertificate           *TLSCertificateStageResponse    `json:"tlsCertificate,omitempty"`
	PHPPoolInspection        *PHPPoolInspectResponse         `json:"phpPoolInspection,omitempty"`
	PHPPools                 *PHPPoolSetResponse             `json:"phpPools,omitempty"`
	Database                 *DatabaseProvisionResponse      `json:"database,omitempty"`
	DatabasePasswordRotation *DatabasePasswordRotateResponse `json:"databasePasswordRotation,omitempty"`
	DatabaseDrop             *DatabaseDropResponse           `json:"databaseDrop,omitempty"`
	ScheduledJob             *ScheduledJobReconcileResponse  `json:"scheduledJob,omitempty"`
	OCIImage                 *OCIImagePrepareResponse        `json:"ociImage,omitempty"`
	OCIResources             *OCIResourceReconcileResponse   `json:"ociResources,omitempty"`
	OCIDeployment            *OCIDeploymentResponse          `json:"ociDeployment,omitempty"`
	OCIApplicationLogs       *OCIApplicationLogReadResponse  `json:"ociApplicationLogs,omitempty"`
	PlatformUpdateStart      *PlatformUpdateStartResponse    `json:"platformUpdateStart,omitempty"`
	PlatformUpdateStatus     *PlatformUpdateStatusResponse   `json:"platformUpdateStatus,omitempty"`
	Error                    *ResponseError                  `json:"error,omitempty"`
}

type HandshakeResponse struct {
	SelectedVersion     int            `json:"selectedVersion"`
	AgentMinimumVersion int            `json:"agentMinimumVersion"`
	AgentMaximumVersion int            `json:"agentMaximumVersion"`
	AgentBuild          buildinfo.Info `json:"agentBuild"`
	SupportedOperations []Operation    `json:"supportedOperations"`
}

type ResponseError struct {
	Code       ErrorCode   `json:"code"`
	Message    string      `json:"message"`
	Capability *Capability `json:"capability,omitempty"`
}

func ValidateRequest(request Request) error {
	if request.ProtocolVersion != WireVersion {
		return ErrUnsupportedWireVersion
	}
	if !validBoundedIdentifier(request.RequestID) {
		return fmt.Errorf("%w: requestId is malformed", ErrInvalidRequest)
	}
	if !validBoundedIdentifier(request.IdempotencyKey) {
		return fmt.Errorf("%w: idempotencyKey is malformed", ErrInvalidRequest)
	}
	policy, exists := policyForOperation(request.Operation)
	if !exists {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, ErrUnsupportedOperation)
	}
	if err := validateCorrelationRequirement(
		policy.access == operationPrivilegedMutation, request.Correlation,
	); err != nil {
		return err
	}
	switch request.Operation {
	case OperationHandshake:
		if request.Handshake == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: handshake payload is required", ErrInvalidRequest)
		}
		if request.Handshake.MinimumVersion < 1 ||
			request.Handshake.MaximumVersion < request.Handshake.MinimumVersion ||
			request.Handshake.MaximumVersion > 1_000_000 {
			return fmt.Errorf("%w: handshake version range is invalid", ErrInvalidRequest)
		}
		if err := validateBuildInfo(request.Handshake.ClientBuild); err != nil {
			return err
		}
	case OperationInspectCapabilities:
		if request.InspectCapabilities == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: inspectCapabilities payload is required", ErrInvalidRequest)
		}
	case OperationReconcileIdentity:
		if request.ReconcileIdentity == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: reconcileIdentity payload is required", ErrInvalidRequest)
		}
		if err := validateHostingIdentityMutation(request.Correlation, request.ReconcileIdentity.Identity); err != nil {
			return err
		}
		if !validHostingIdentityStage(request.ReconcileIdentity.Stage) {
			return fmt.Errorf("%w: hosting identity stage is invalid", ErrInvalidRequest)
		}
	case OperationDeleteIdentity:
		if request.DeleteIdentity == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: deleteIdentity payload is required", ErrInvalidRequest)
		}
		if err := validateHostingIdentityMutation(request.Correlation, request.DeleteIdentity.Identity); err != nil {
			return err
		}
		if request.DeleteIdentity.Stage != HostingIdentityStageFull {
			return fmt.Errorf("%w: deletion cannot select an identity stage", ErrInvalidRequest)
		}
	case OperationReconcileFilesystem:
		if request.ReconcileFilesystem == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: reconcileFilesystem payload is required", ErrInvalidRequest)
		}
		if err := validateHostingIdentityMutation(request.Correlation, request.ReconcileFilesystem.Storage.Identity); err != nil {
			return err
		}
		if err := hostingstorage.Validate(request.ReconcileFilesystem.Storage); err != nil {
			return fmt.Errorf("%w: hosting filesystem storage intent is malformed", ErrInvalidRequest)
		}
	case OperationListFiles:
		if request.ListFiles == nil || requestPayloadCount(request) != 1 || validateFileListRequest(*request.ListFiles) != nil {
			return fmt.Errorf("%w: file listing request is malformed", ErrInvalidRequest)
		}
	case OperationReadHostingLogs:
		if request.ReadHostingLogs == nil || requestPayloadCount(request) != 1 ||
			validateHostingLogReadRequest(*request.ReadHostingLogs) != nil {
			return fmt.Errorf("%w: hosting log read request is malformed", ErrInvalidRequest)
		}
	case OperationReadWAFEvents:
		if request.ReadWAFEvents == nil || requestPayloadCount(request) != 1 ||
			validateWAFEventReadRequest(*request.ReadWAFEvents) != nil {
			return fmt.Errorf("%w: WAF event read request is malformed", ErrInvalidRequest)
		}
	case OperationInspectCacheMetrics:
		if request.InspectCacheMetrics == nil || requestPayloadCount(request) != 1 ||
			validateCacheMetricsRequest(*request.InspectCacheMetrics) != nil {
			return fmt.Errorf("%w: cache metrics request is malformed", ErrInvalidRequest)
		}
	case OperationPurgeCache:
		if request.PurgeCache == nil || requestPayloadCount(request) != 1 ||
			validateCachePurgeRequest(request.Correlation, *request.PurgeCache) != nil {
			return fmt.Errorf("%w: cache purge request is malformed", ErrInvalidRequest)
		}
	case OperationReconcileResources:
		if request.ReconcileResources == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: reconcileResources payload is required", ErrInvalidRequest)
		}
		if err := validateHostingIdentityMutation(request.Correlation, request.ReconcileResources.Resources.Identity); err != nil {
			return err
		}
		if err := hostingresources.Validate(request.ReconcileResources.Resources); err != nil {
			return fmt.Errorf("%w: hosting resource intent is malformed", ErrInvalidRequest)
		}
	case OperationEnsureDocumentRoot:
		if request.EnsureDocumentRoot == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: ensureDocumentRoot payload is required", ErrInvalidRequest)
		}
		if err := validateHostingIdentityMutation(request.Correlation, request.EnsureDocumentRoot.Identity); err != nil {
			return err
		}
		normalized, err := hostingpath.NormalizeDocumentRoot(request.EnsureDocumentRoot.RelativePath)
		if err != nil || normalized != request.EnsureDocumentRoot.RelativePath ||
			ValidateDocumentRootAccess(request.EnsureDocumentRoot.Access) != nil {
			return fmt.Errorf("%w: document root is malformed", ErrInvalidRequest)
		}
	case OperationReconcileNGINXBaseline:
		if request.ReconcileNGINXBaseline == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: reconcileNginxBaseline payload is required", ErrInvalidRequest)
		}
		if request.Correlation.AccountID != "" {
			return fmt.Errorf("%w: global NGINX mutation must not carry accountId", ErrInvalidRequest)
		}
	case OperationActivateNGINXSites:
		if request.ActivateNGINXSites == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: activateNginxSites payload is required", ErrInvalidRequest)
		}
		activation := request.ActivateNGINXSites
		if err := validateHostingIdentityMutation(request.Correlation, activation.Identity); err != nil {
			return err
		}
		if !validCanonicalUUIDv7(activation.DesiredStateRevisionID) {
			return fmt.Errorf("%w: desiredStateRevisionId is malformed", ErrInvalidRequest)
		}
		if _, err := nginxconfig.RenderSpecs(activation.Identity, activation.Domains, activation.Options); err != nil {
			return fmt.Errorf("%w: NGINX site intent is malformed", ErrInvalidRequest)
		}
	case OperationReconcileACMEHTTP01:
		if request.ReconcileACMEHTTP01 == nil || requestPayloadCount(request) != 1 ||
			request.Correlation.AccountID == "" || acmehttp01.Validate(request.ReconcileACMEHTTP01.Intent) != nil {
			return fmt.Errorf("%w: ACME HTTP-01 intent is malformed", ErrInvalidRequest)
		}
	case OperationStageTLSCertificate:
		if request.StageTLSCertificate == nil || requestPayloadCount(request) != 1 ||
			request.Correlation.AccountID == "" || tlsartifact.Validate(request.StageTLSCertificate.Bundle) != nil {
			return fmt.Errorf("%w: TLS certificate bundle is malformed", ErrInvalidRequest)
		}
	case OperationInspectPHPPools:
		if request.InspectPHPPools == nil || requestPayloadCount(request) != 1 ||
			validatePHPPoolInspectRequest(*request.InspectPHPPools) != nil {
			return fmt.Errorf("%w: PHP pool inspection is malformed", ErrInvalidRequest)
		}
	case OperationReconcilePHPPools:
		if request.ReconcilePHPPools == nil || requestPayloadCount(request) != 1 ||
			request.Correlation.AccountID == "" ||
			validateHostingIdentityMutation(request.Correlation, request.ReconcilePHPPools.Pools.Identity) != nil ||
			phpruntime.Validate(request.ReconcilePHPPools.Pools) != nil {
			return fmt.Errorf("%w: PHP pool intent is malformed", ErrInvalidRequest)
		}
	case OperationProvisionDatabase:
		if request.ProvisionDatabase == nil || requestPayloadCount(request) != 1 ||
			validateDatabaseProvisionRequest(request.Correlation, *request.ProvisionDatabase) != nil {
			return fmt.Errorf("%w: managed database provisioning intent is malformed", ErrInvalidRequest)
		}
	case OperationRotateDatabasePassword:
		if request.RotateDatabasePassword == nil || requestPayloadCount(request) != 1 ||
			validateDatabasePasswordRotateRequest(request.Correlation, *request.RotateDatabasePassword) != nil {
			return fmt.Errorf("%w: managed database password rotation intent is malformed", ErrInvalidRequest)
		}
	case OperationDropDatabase:
		if request.DropDatabase == nil || requestPayloadCount(request) != 1 ||
			validateDatabaseDropRequest(request.Correlation, *request.DropDatabase) != nil {
			return fmt.Errorf("%w: managed database deletion intent is malformed", ErrInvalidRequest)
		}
	case OperationReconcileScheduledJob:
		if request.ReconcileScheduledJob == nil || requestPayloadCount(request) != 1 ||
			validateScheduledJobRequest(request.Correlation, *request.ReconcileScheduledJob) != nil {
			return fmt.Errorf("%w: scheduled job intent is malformed", ErrInvalidRequest)
		}
	case OperationPrepareOCIImage:
		if request.PrepareOCIImage == nil || requestPayloadCount(request) != 1 ||
			validateOCIImagePrepareRequest(request.Correlation, *request.PrepareOCIImage) != nil {
			return fmt.Errorf("%w: OCI image preparation intent is malformed", ErrInvalidRequest)
		}
	case OperationReconcileOCIResources:
		if request.ReconcileOCIResources == nil || requestPayloadCount(request) != 1 ||
			validateOCIResourceReconcileRequest(request.Correlation, *request.ReconcileOCIResources) != nil {
			return fmt.Errorf("%w: OCI private-resource intent is malformed", ErrInvalidRequest)
		}
	case OperationReconcileOCIDeployment:
		if request.ReconcileOCIDeployment == nil || requestPayloadCount(request) != 1 ||
			validateOCIDeploymentRequest(request.Correlation, *request.ReconcileOCIDeployment) != nil {
			return fmt.Errorf("%w: OCI deployment intent is malformed", ErrInvalidRequest)
		}
	case OperationReadOCIApplicationLogs:
		if request.ReadOCIApplicationLogs == nil || requestPayloadCount(request) != 1 ||
			validateOCIApplicationLogReadRequest(*request.ReadOCIApplicationLogs) != nil {
			return fmt.Errorf("%w: OCI application log request is malformed", ErrInvalidRequest)
		}
	case OperationInspectPlatformUpdate:
		if request.InspectPlatformUpdate == nil || requestPayloadCount(request) != 1 {
			return fmt.Errorf("%w: platform update inspection payload is required", ErrInvalidRequest)
		}
	case OperationStartPlatformUpdate:
		if request.StartPlatformUpdate == nil || requestPayloadCount(request) != 1 ||
			validatePlatformUpdateStartRequest(request.Correlation, *request.StartPlatformUpdate) != nil {
			return fmt.Errorf("%w: platform update request is malformed", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: operation payload policy is missing", ErrInvalidRequest)
	}
	return nil
}

func ValidateResponse(response Response, requestID string, expectedOperation Operation) error {
	if response.ProtocolVersion != WireVersion || response.RequestID != requestID ||
		!validBoundedIdentifier(response.RequestID) {
		return errors.New("agent protocol response correlation failed")
	}
	if err := validateCapabilityUnion(response, expectedOperation); err != nil {
		return err
	}
	if response.Error != nil {
		if !validErrorCode(response.Error.Code) ||
			response.Error.Message == "" || len(response.Error.Message) > 256 {
			return errors.New("agent protocol error response is malformed")
		}
		if response.Error.Code == ErrorQuotaUnavailable || response.Error.Code == ErrorResourceControlUnavailable ||
			response.Error.Code == ErrorOCIRuntimeUnavailable ||
			response.Error.Code == ErrorOCIImageUnavailable ||
			response.Error.Code == ErrorOCIResourceUnavailable ||
			response.Error.Code == ErrorOCIDeploymentUnavailable ||
			response.Error.Code == ErrorNGINXUnavailable || response.Error.Code == ErrorPHPUnavailable ||
			response.Error.Code == ErrorScheduledJobUnavailable || response.Error.Code == ErrorCacheUnavailable {
			if response.Error.Capability == nil || validateCapability(*response.Error.Capability) != nil ||
				response.Error.Capability.Status == CapabilityAvailable {
				return errors.New("agent capability error detail is malformed")
			}
		} else if response.Error.Capability != nil {
			return errors.New("agent error carries an unexpected capability")
		}
		return nil
	}
	if response.Capabilities != nil {
		return ValidateCapabilityReport(*response.Capabilities)
	}
	if response.HostingIdentity != nil {
		return validateHostingIdentityResponse(*response.HostingIdentity, expectedOperation)
	}
	if response.HostingFilesystem != nil {
		return validateHostingFilesystemResponse(*response.HostingFilesystem, expectedOperation)
	}
	if response.FileListing != nil {
		return validateFileListResponse(*response.FileListing, expectedOperation)
	}
	if response.HostingLogs != nil {
		return validateHostingLogReadResponse(*response.HostingLogs, expectedOperation)
	}
	if response.WAFEvents != nil {
		return validateWAFEventReadResponse(*response.WAFEvents, expectedOperation)
	}
	if response.CacheMetrics != nil {
		return validateCacheMetricsResponse(*response.CacheMetrics, expectedOperation)
	}
	if response.CachePurge != nil {
		return validateCachePurgeResponse(*response.CachePurge, expectedOperation)
	}
	if response.HostingResources != nil {
		return validateHostingResourcesResponse(*response.HostingResources, expectedOperation)
	}
	if response.DocumentRoot != nil {
		return validateDocumentRootResponse(*response.DocumentRoot, expectedOperation)
	}
	if response.NGINXBaseline != nil {
		return validateNGINXBaselineResponse(*response.NGINXBaseline, expectedOperation)
	}
	if response.NGINXActivation != nil {
		return validateNGINXActivationResponse(*response.NGINXActivation, expectedOperation)
	}
	if response.ACMEHTTP01 != nil {
		return validateACMEHTTP01Response(*response.ACMEHTTP01, expectedOperation)
	}
	if response.TLSCertificate != nil {
		return validateTLSCertificateStageResponse(*response.TLSCertificate, expectedOperation)
	}
	if response.PHPPoolInspection != nil {
		return validatePHPPoolInspectResponse(*response.PHPPoolInspection, expectedOperation)
	}
	if response.PHPPools != nil {
		return validatePHPPoolSetResponse(*response.PHPPools, expectedOperation)
	}
	if response.Database != nil {
		return validateDatabaseProvisionResponse(*response.Database, expectedOperation)
	}
	if response.DatabasePasswordRotation != nil {
		return validateDatabasePasswordRotateResponse(*response.DatabasePasswordRotation, expectedOperation)
	}
	if response.DatabaseDrop != nil {
		return validateDatabaseDropResponse(*response.DatabaseDrop, expectedOperation)
	}
	if response.ScheduledJob != nil {
		return validateScheduledJobResponse(*response.ScheduledJob, expectedOperation)
	}
	if response.OCIImage != nil {
		return validateOCIImagePrepareResponse(*response.OCIImage, expectedOperation)
	}
	if response.OCIResources != nil {
		return validateOCIResourceReconcileResponse(*response.OCIResources, expectedOperation)
	}
	if response.OCIDeployment != nil {
		return validateOCIDeploymentResponse(*response.OCIDeployment, expectedOperation)
	}
	if response.OCIApplicationLogs != nil {
		return validateOCIApplicationLogReadResponse(*response.OCIApplicationLogs, expectedOperation)
	}
	if response.PlatformUpdateStart != nil {
		return validatePlatformUpdateStartResponse(*response.PlatformUpdateStart, expectedOperation)
	}
	if response.PlatformUpdateStatus != nil {
		return validatePlatformUpdateStatusResponse(*response.PlatformUpdateStatus, expectedOperation)
	}
	if response.Handshake.AgentMinimumVersion < 1 ||
		response.Handshake.AgentMaximumVersion < response.Handshake.AgentMinimumVersion ||
		response.Handshake.AgentMaximumVersion > 1_000_000 ||
		response.Handshake.SelectedVersion < response.Handshake.AgentMinimumVersion ||
		response.Handshake.SelectedVersion > response.Handshake.AgentMaximumVersion ||
		len(response.Handshake.SupportedOperations) == 0 ||
		len(response.Handshake.SupportedOperations) > 128 {
		return errors.New("agent protocol handshake response is malformed")
	}
	if err := validateBuildInfo(response.Handshake.AgentBuild); err != nil {
		return err
	}
	seenOperations := make(map[Operation]struct{}, len(response.Handshake.SupportedOperations))
	for _, operation := range response.Handshake.SupportedOperations {
		if _, exists := policyForOperation(operation); !exists {
			return errors.New("agent protocol response advertised an unknown operation")
		}
		if _, exists := seenOperations[operation]; exists {
			return errors.New("agent protocol response advertised a duplicate operation")
		}
		seenOperations[operation] = struct{}{}
	}
	if _, exists := seenOperations[OperationHandshake]; !exists {
		return errors.New("agent protocol response omitted the handshake operation")
	}
	return nil
}

// SupportedOperations returns a deterministic copy of the operations with an
// explicit access and audit policy.
func SupportedOperations() []Operation {
	operations := make([]Operation, 0, len(operationPolicies))
	for _, policy := range operationPolicies {
		operations = append(operations, policy.operation)
	}
	return operations
}

// RequiresAuditCorrelation reports whether an operation may mutate privileged
// host state and must therefore carry an AuditCorrelation.
func RequiresAuditCorrelation(operation Operation) bool {
	policy, exists := policyForOperation(operation)
	return exists && policy.access == operationPrivilegedMutation
}

// ValidateAuditCorrelation checks the correlation independently so a typed API
// client can fail before sending a future mutation request.
func ValidateAuditCorrelation(correlation AuditCorrelation) error {
	if !validCanonicalUUIDv7(correlation.OperationID) {
		return fmt.Errorf("%w: correlation operationId is malformed", ErrInvalidRequest)
	}
	if correlation.AccountID != "" && !validCanonicalUUIDv7(correlation.AccountID) {
		return fmt.Errorf("%w: correlation accountId is malformed", ErrInvalidRequest)
	}
	switch correlation.ActorKind {
	case ActorIdentity:
		if !validCanonicalUUIDv7(correlation.ActorID) {
			return fmt.Errorf("%w: correlation actorId is malformed", ErrInvalidRequest)
		}
	case ActorSystem:
		if correlation.ActorID != "" {
			return fmt.Errorf("%w: system correlation must not carry actorId", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: correlation actorKind is malformed", ErrInvalidRequest)
	}
	return nil
}

func policyForOperation(operation Operation) (operationPolicy, bool) {
	for _, policy := range operationPolicies {
		if policy.operation == operation {
			return policy, validOperationAccess(policy.access)
		}
	}
	return operationPolicy{}, false
}

func validOperationAccess(access operationAccess) bool {
	return access == operationProtocol || access == operationReadOnly ||
		access == operationPrivilegedMutation
}

func validateCorrelationRequirement(required bool, correlation *AuditCorrelation) error {
	if required && correlation == nil {
		return fmt.Errorf("%w: privileged mutation requires audit correlation", ErrInvalidRequest)
	}
	if !required && correlation != nil {
		return fmt.Errorf("%w: audit correlation is not allowed for this operation", ErrInvalidRequest)
	}
	if correlation != nil {
		return ValidateAuditCorrelation(*correlation)
	}
	return nil
}

func validCanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}

func DecodeRequest(reader io.Reader) (Request, error) {
	var request Request
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("%w: decode JSON: %w", ErrInvalidRequest, err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return Request{}, err
	}
	if err := ValidateRequest(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

func DecodeResponse(reader io.Reader, requestID string, expectedOperation Operation) (Response, error) {
	var response Response
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("decode agent protocol response: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return Response{}, err
	}
	if err := ValidateResponse(response, requestID, expectedOperation); err != nil {
		return Response{}, err
	}
	return response, nil
}

func SemanticDigest(request Request) ([sha256.Size]byte, error) {
	semantic := request
	semantic.RequestID = ""
	semantic.IdempotencyKey = ""
	encoded, err := json.Marshal(semantic)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode semantic agent request: %w", err)
	}
	defer clear(encoded)
	return sha256.Sum256(encoded), nil
}

func validBoundedIdentifier(value string) bool {
	return boundedIdentifierPattern.MatchString(value)
}

func validErrorCode(code ErrorCode) bool {
	switch code {
	case ErrorInvalidRequest, ErrorRequestTooLarge, ErrorUnsupportedMediaType,
		ErrorIncompatibleProtocol, ErrorUnsupportedOperation,
		ErrorIdempotencyConflict, ErrorIdentityConflict, ErrorOCIRuntimeUnavailable, ErrorArchiveRequired,
		ErrorFilesystemConflict, ErrorFilesystemMigration, ErrorQuotaUnavailable,
		ErrorFileNotFound, ErrorFileConflict, ErrorFileUnavailable,
		ErrorFileDownloadUnavailable, ErrorFileDownloadTooLarge,
		ErrorFileRangeNotSatisfiable, ErrorFileDownloadBusy,
		ErrorHostingLogNotFound, ErrorHostingLogConflict, ErrorHostingLogUnavailable,
		ErrorResourceControlConflict, ErrorResourceControlUnavailable,
		ErrorNGINXConflict, ErrorNGINXUnavailable, ErrorNGINXValidation,
		ErrorNGINXActivation, ErrorNGINXHealthCheck,
		ErrorACMEHTTP01Conflict, ErrorACMEHTTP01Unavailable,
		ErrorTLSCertificateConflict, ErrorTLSCertificateUnavailable,
		ErrorPHPConflict, ErrorPHPUnavailable, ErrorPHPValidation, ErrorPHPActivation,
		ErrorDatabaseConflict, ErrorDatabaseUnavailable, ErrorDatabaseValidation, ErrorDatabaseMutation,
		ErrorScheduledJobInvalid, ErrorScheduledJobNotFound, ErrorScheduledJobConflict, ErrorScheduledJobUnavailable,
		ErrorCacheConflict, ErrorCacheUnavailable,
		ErrorOCIImageInvalid, ErrorOCIImageUnavailable, ErrorOCIImageRejected,
		ErrorOCIResourceInvalid, ErrorOCIResourceConflict, ErrorOCIResourceUnavailable,
		ErrorOCIDeploymentInvalid, ErrorOCIDeploymentConflict, ErrorOCIDeploymentUnhealthy, ErrorOCIDeploymentUnavailable,
		ErrorPlatformUpdateInvalid, ErrorPlatformUpdateConflict, ErrorPlatformUpdateUnavailable,
		ErrorMutationFailed, ErrorInternal:
		return true
	default:
		return false
	}
}

func requestPayloadCount(request Request) int {
	count := 0
	for _, present := range []bool{
		request.Handshake != nil, request.InspectCapabilities != nil,
		request.ReconcileIdentity != nil, request.DeleteIdentity != nil,
		request.ReconcileFilesystem != nil, request.ListFiles != nil, request.ReadHostingLogs != nil,
		request.ReadWAFEvents != nil,
		request.InspectCacheMetrics != nil, request.PurgeCache != nil,
		request.EnsureDocumentRoot != nil,
		request.ReconcileResources != nil,
		request.ReconcileNGINXBaseline != nil,
		request.ActivateNGINXSites != nil,
		request.ReconcileACMEHTTP01 != nil,
		request.StageTLSCertificate != nil,
		request.InspectPHPPools != nil,
		request.ReconcilePHPPools != nil,
		request.ProvisionDatabase != nil,
		request.RotateDatabasePassword != nil,
		request.DropDatabase != nil,
		request.ReconcileScheduledJob != nil,
		request.PrepareOCIImage != nil,
		request.ReconcileOCIResources != nil,
		request.ReconcileOCIDeployment != nil,
		request.ReadOCIApplicationLogs != nil,
		request.InspectPlatformUpdate != nil,
		request.StartPlatformUpdate != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func validateHostingIdentityMutation(correlation *AuditCorrelation, spec hostingidentity.Spec) error {
	if correlation == nil || correlation.AccountID == "" || correlation.AccountID != spec.AccountID {
		return fmt.Errorf("%w: audit accountId must match the hosting identity", ErrInvalidRequest)
	}
	if err := hostingidentity.Validate(spec); err != nil {
		return fmt.Errorf("%w: hosting identity is malformed", ErrInvalidRequest)
	}
	return nil
}

func validateBuildInfo(info buildinfo.Info) error {
	values := []string{info.Version, info.Commit, info.BuildDate}
	for _, value := range values {
		if len(value) == 0 || len(value) > 128 {
			return fmt.Errorf("%w: build metadata is invalid", ErrInvalidRequest)
		}
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("%w: trailing JSON: %w", ErrInvalidRequest, err)
		}
		return fmt.Errorf("%w: trailing JSON is not allowed", ErrInvalidRequest)
	}
	return nil
}
