// SPDX-License-Identifier: AGPL-3.0-or-later

// Package core implements Stackfort's control-plane records and invariants.
package core

import (
	"errors"
	"time"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

var (
	// ErrInvalidInput identifies a rejected domain value without exposing a
	// database or implementation detail to callers.
	ErrInvalidInput = errors.New("invalid core record input")
	// ErrNotFound identifies a requested record that does not exist.
	ErrNotFound = errors.New("core record not found")
	// ErrConflict identifies a uniqueness, revision, or relational conflict.
	ErrConflict = errors.New("core record conflict")
	// ErrNoOperationAvailable means no eligible operation can currently be
	// claimed for a worker's supported kinds.
	ErrNoOperationAvailable = errors.New("no operation available")
	// ErrOperationLeaseLost means a stale or expired attempt tried to mutate an
	// operation after its fencing lease was no longer authoritative.
	ErrOperationLeaseLost = errors.New("operation lease lost")
	// ErrOperationCancellationRequested asks a handler to stop at its next safe
	// cancellation boundary and acknowledge or fail cleanup explicitly.
	ErrOperationCancellationRequested = errors.New("operation cancellation requested")
	// ErrBootstrapDenied deliberately hides whether a bootstrap capability was
	// absent, expired, consumed, or incorrect.
	ErrBootstrapDenied = errors.New("administrator bootstrap denied")
	// ErrBootstrapDisabled means the first platform administrator already exists.
	ErrBootstrapDisabled = errors.New("administrator bootstrap is disabled")
	// ErrBootstrapRateLimited identifies persisted bootstrap abuse protection.
	ErrBootstrapRateLimited = errors.New("administrator bootstrap rate limited")
	// ErrAuthenticationDenied deliberately hides whether an identity exists, is
	// inactive, is blocked, or supplied an incorrect password.
	ErrAuthenticationDenied = errors.New("authentication denied")
	// ErrAuthenticationRateLimited identifies persisted login abuse protection.
	ErrAuthenticationRateLimited = errors.New("authentication rate limited")
	// ErrSessionInvalid covers absent, expired, revoked, and malformed sessions.
	ErrSessionInvalid = errors.New("session is invalid")
	// ErrCSRFInvalid means a state-changing request did not prove same-session intent.
	ErrCSRFInvalid = errors.New("CSRF token is invalid")
	// ErrAuthorizationDenied deliberately does not reveal whether a target exists
	// or which relationship, role, or account state caused the denial.
	ErrAuthorizationDenied = errors.New("authorization denied")
	// ErrRecentAuthenticationRequired means policy permits the action only after
	// the identity has authenticated again within the configured freshness window.
	ErrRecentAuthenticationRequired = errors.New("recent authentication required")
	// ErrSecretStorageUnavailable means a retrievable secret operation was
	// attempted without a configured host master key.
	ErrSecretStorageUnavailable = errors.New("secret storage is unavailable")
	// ErrMFAChallengeInvalid deliberately combines missing, expired, consumed,
	// replayed, malformed, and incorrect MFA challenge failures.
	ErrMFAChallengeInvalid = errors.New("MFA challenge is invalid")
)

type Locale string

const (
	LocaleEnglish Locale = "en"
	LocaleGerman  Locale = "de"
)

type IdentityStatus string

const (
	IdentityActive    IdentityStatus = "active"
	IdentitySuspended IdentityStatus = "suspended"
	IdentityArchived  IdentityStatus = "archived"
)

type Identity struct {
	ID              ID
	Email           string
	NormalizedEmail string
	DisplayName     string
	Locale          Locale
	Status          IdentityStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateIdentityParams struct {
	Email       string
	DisplayName string
	Locale      Locale
	ActorID     *ID
	RequestID   string
}

type PasswordCredential struct {
	IdentityID  ID
	Hash        []byte
	Salt        []byte
	MemoryKiB   int64
	Iterations  int64
	Parallelism int64
	Version     int64
	MustRotate  bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SetPasswordCredentialParams struct {
	IdentityID  ID
	Hash        []byte
	Salt        []byte
	MemoryKiB   int64
	Iterations  int64
	Parallelism int64
	Version     int64
	MustRotate  bool
	ActorID     *ID
	RequestID   string
}

type Session struct {
	ID                  ID
	IdentityID          ID
	CreatedAt           time.Time
	AuthenticatedAt     time.Time
	LastSeenAt          time.Time
	ExpiresAt           time.Time
	SourceAddress       string
	UserAgent           string
	AuthenticationLevel SessionAuthenticationLevel
	MFAAuthenticatedAt  *time.Time
}

type SessionAuthenticationLevel string

const (
	SessionAuthenticationPassword SessionAuthenticationLevel = "password"
	SessionAuthenticationTOTP     SessionAuthenticationLevel = "totp"
	SessionAuthenticationRecovery SessionAuthenticationLevel = "recovery"
)

type CreateSessionParams struct {
	IdentityID     ID
	TokenHash      []byte
	CSRFSecretHash []byte
	ExpiresAt      time.Time
	SourceAddress  string
	UserAgent      string
	RequestID      string
}

type PasswordLoginParams struct {
	Email                string
	Password             string
	SourceAddress        string
	UserAgent            string
	RequestID            string
	PreviousSessionToken string
}

// PasswordLoginResult contains raw browser secrets exactly once. Callers must
// place them only in protected cookies and must never log or persist them.
type PasswordLoginResult struct {
	Identity              Identity
	Session               Session
	SessionToken          string
	CSRFToken             string
	MFARequired           bool
	MFAChallengeToken     string
	MFAChallengeExpiresAt time.Time
}

type CompleteMFALoginParams struct {
	ChallengeToken string
	Code           string
	RequestID      string
}

type AuthenticateSessionParams struct {
	SessionToken    string
	CSRFHeaderToken string
	CSRFCookieToken string
	RequireCSRF     bool
}

type AuthenticatedSession struct {
	Identity           Identity
	Session            Session
	authorizationProof [32]byte
}

// AuthorizationSubject is server-derived session context. It is not an
// authentication mechanism and must only be built from AuthenticatedSession.
type AuthorizationSubject struct {
	identityID ID
	sessionID  ID
	proof      [32]byte
}

func (authenticated AuthenticatedSession) AuthorizationSubject() AuthorizationSubject {
	return AuthorizationSubject{
		identityID: authenticated.Identity.ID,
		sessionID:  authenticated.Session.ID,
		proof:      authenticated.authorizationProof,
	}
}

func (subject AuthorizationSubject) IdentityID() ID {
	return subject.identityID
}

func (subject AuthorizationSubject) SessionID() ID {
	return subject.sessionID
}

type AuthorizationAction string

const (
	AuthorizationPlatformView             AuthorizationAction = "platform.view"
	AuthorizationPlatformManage           AuthorizationAction = "platform.manage"
	AuthorizationPackagesView             AuthorizationAction = "packages.view"
	AuthorizationPackagesManage           AuthorizationAction = "packages.manage"
	AuthorizationAccountsCreate           AuthorizationAction = "accounts.create"
	AuthorizationAccountView              AuthorizationAction = "account.view"
	AuthorizationAccountManage            AuthorizationAction = "account.manage"
	AuthorizationAccountMembershipsView   AuthorizationAction = "account.memberships.view"
	AuthorizationAccountMembershipsManage AuthorizationAction = "account.memberships.manage"
	AuthorizationAccountPackageView       AuthorizationAction = "account.package.view"
	AuthorizationAccountPackageManage     AuthorizationAction = "account.package.manage"
	AuthorizationAccountResourcesView     AuthorizationAction = "account.resources.view"
	AuthorizationAccountResourcesManage   AuthorizationAction = "account.resources.manage"
	AuthorizationAccountFilesView         AuthorizationAction = "account.files.view"
	AuthorizationAccountFilesManage       AuthorizationAction = "account.files.manage"
	AuthorizationAccountBackupsView       AuthorizationAction = "account.backups.view"
	AuthorizationAccountBackupsManage     AuthorizationAction = "account.backups.manage"
	AuthorizationAccountBackupsRestore    AuthorizationAction = "account.backups.restore"
	AuthorizationAccountBackupsDelete     AuthorizationAction = "account.backups.delete"
	AuthorizationAccountLogsView          AuthorizationAction = "account.logs.view"
	AuthorizationAccountJobsView          AuthorizationAction = "account.jobs.view"
	AuthorizationAccountJobsManage        AuthorizationAction = "account.jobs.manage"
	AuthorizationAccountCredentialsManage AuthorizationAction = "account.credentials.manage"
	AuthorizationAccountAuditView         AuthorizationAction = "account.audit.view"
	AuthorizationAccountDestructive       AuthorizationAction = "account.destructive"
	AuthorizationIdentityProfileView      AuthorizationAction = "identity.profile.view"
	AuthorizationIdentityProfileManage    AuthorizationAction = "identity.profile.manage"
	AuthorizationIdentityFactorsView      AuthorizationAction = "identity.factors.view"
	AuthorizationIdentityFactorsManage    AuthorizationAction = "identity.factors.manage"
	AuthorizationIdentitySessionsView     AuthorizationAction = "identity.sessions.view"
	AuthorizationIdentitySessionsManage   AuthorizationAction = "identity.sessions.manage"
)

type ScheduledJobStatus string

const (
	ScheduledJobPending  ScheduledJobStatus = "pending"
	ScheduledJobActive   ScheduledJobStatus = "active"
	ScheduledJobDisabled ScheduledJobStatus = "disabled"
	ScheduledJobDeleting ScheduledJobStatus = "deleting"
	ScheduledJobError    ScheduledJobStatus = "error"
	ScheduledJobDeleted  ScheduledJobStatus = "deleted"
)

type ScheduledJob struct {
	ID              ID                     `json:"id"`
	AccountID       ID                     `json:"accountId"`
	Name            string                 `json:"name"`
	Runtime         scheduledjobs.Runtime  `json:"runtime"`
	ScriptPath      string                 `json:"scriptPath"`
	PHPVersion      string                 `json:"phpVersion,omitempty"`
	Schedule        scheduledjobs.Schedule `json:"schedule"`
	Enabled         bool                   `json:"enabled"`
	Status          ScheduledJobStatus     `json:"status"`
	Revision        int64                  `json:"revision"`
	AppliedRevision *int64                 `json:"appliedRevision,omitempty"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	RemovedAt       *time.Time             `json:"removedAt,omitempty"`
}

type PrepareScheduledJobCreateParams struct {
	AccountID      ID
	Name           string
	Runtime        scheduledjobs.Runtime
	ScriptPath     string
	PHPVersion     string
	Schedule       scheduledjobs.Schedule
	Enabled        bool
	ActorID        ID
	RequestID      string
	IdempotencyKey string
}

type PrepareScheduledJobUpdateParams struct {
	AccountID        ID
	JobID            ID
	ExpectedRevision int64
	Name             string
	Runtime          scheduledjobs.Runtime
	ScriptPath       string
	PHPVersion       string
	Schedule         scheduledjobs.Schedule
	Enabled          bool
	ActorID          ID
	RequestID        string
	IdempotencyKey   string
}

type PrepareScheduledJobDeleteParams struct {
	AccountID        ID
	JobID            ID
	ExpectedRevision int64
	ActorID          ID
	RequestID        string
	IdempotencyKey   string
}

type ScheduledJobMutationAction string

const (
	ScheduledJobMutationCreate ScheduledJobMutationAction = "create"
	ScheduledJobMutationUpdate ScheduledJobMutationAction = "update"
	ScheduledJobMutationDelete ScheduledJobMutationAction = "delete"
)

type ScheduledJobMutation struct {
	Operation       Operation
	Job             ScheduledJob
	Action          ScheduledJobMutationAction
	DesiredRevision int64
	AppliedAt       *time.Time
}

type CompleteScheduledJobMutationParams struct {
	AccountID   ID
	OperationID ID
	ActorID     *ID
	RequestID   string
}

type AuthorizeParams struct {
	Subject   AuthorizationSubject
	Action    AuthorizationAction
	AccountID *ID
}

type AuthorizationDecision struct {
	Action                       AuthorizationAction
	AccountID                    *ID
	PlatformAdministrator        bool
	MembershipRole               *MembershipRole
	RequiresRecentAuthentication bool
}

type GetAuthorizedHostingAccountParams struct {
	Subject   AuthorizationSubject
	AccountID ID
}

type AuthorizedHostingAccount struct {
	Account       HostingAccount
	Authorization AuthorizationDecision
}

type RevokeSessionParams struct {
	IdentityID    ID
	SessionID     ID
	Reason        string
	RequestID     string
	SourceAddress string
}

type TOTPStatus struct {
	Enabled                bool
	FactorID               *ID
	ActivatedAt            *time.Time
	RecoveryCodesRemaining int64
}

// TOTPEnrollment contains the provisioning secret exactly once. Callers must
// return it only over the authenticated response and must never log or persist
// the plaintext or provisioning URI.
type TOTPEnrollment struct {
	ChallengeID     ID
	Secret          string
	ProvisioningURI string
	ExpiresAt       time.Time
}

type BeginTOTPEnrollmentParams struct {
	Subject       AuthorizationSubject
	CurrentFactor string
	RequestID     string
	SourceAddress string
}

type ConfirmTOTPEnrollmentParams struct {
	Subject       AuthorizationSubject
	ChallengeID   ID
	Code          string
	RequestID     string
	SourceAddress string
}

// TOTPActivation contains fresh recovery codes exactly once. Only their
// digests are persisted.
type TOTPActivation struct {
	FactorID      ID
	ActivatedAt   time.Time
	RecoveryCodes []string
}

type DisableTOTPParams struct {
	Subject       AuthorizationSubject
	CurrentFactor string
	RequestID     string
	SourceAddress string
}

type ManagedSession struct {
	Session
	Current bool
}

type ListManagedSessionsParams struct {
	Subject AuthorizationSubject
}

type RevokeManagedSessionParams struct {
	Subject         AuthorizationSubject
	TargetSessionID ID
	RequestID       string
	SourceAddress   string
}

type RevokeAllManagedSessionsParams struct {
	Subject       AuthorizationSubject
	KeepCurrent   bool
	RequestID     string
	SourceAddress string
}

type RevokeAllManagedSessionsResult struct {
	Revoked        int64
	CurrentRevoked bool
}

// AuthenticationRateLimitError carries timing only and never identity or
// credential material.
type AuthenticationRateLimitError struct {
	RetryAfter time.Duration
}

func (e *AuthenticationRateLimitError) Error() string {
	return ErrAuthenticationRateLimited.Error()
}

func (e *AuthenticationRateLimitError) Unwrap() error {
	return ErrAuthenticationRateLimited
}

type PlatformRole string

const PlatformAdministrator PlatformRole = "platform_admin"

type GrantPlatformRoleParams struct {
	IdentityID ID
	Role       PlatformRole
	ActorID    *ID
	RequestID  string
}

// BootstrapCapability is the only result that contains a raw bootstrap token.
// It is returned only by CreateBootstrapCapability and cannot be read back.
type BootstrapCapability struct {
	ID        ID
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type CreateBootstrapCapabilityParams struct {
	TTL       time.Duration
	Replace   bool
	RequestID string
}

type BootstrapStatus struct {
	Required         bool
	CapabilityActive bool
	ExpiresAt        *time.Time
}

type BootstrapAdministratorParams struct {
	Token         string
	Email         string
	DisplayName   string
	Password      string
	Locale        Locale
	SourceAddress string
	RequestID     string
}

// BootstrapRateLimitError carries only retry timing, never supplied secrets.
type BootstrapRateLimitError struct {
	RetryAfter time.Duration
}

func (e *BootstrapRateLimitError) Error() string {
	return ErrBootstrapRateLimited.Error()
}

func (e *BootstrapRateLimitError) Unwrap() error {
	return ErrBootstrapRateLimited
}

type PackageStatus string

const (
	PackageActive   PackageStatus = "active"
	PackageArchived PackageStatus = "archived"
)

// PackageLimits is the fully resolved package policy. Optional numeric values
// use nil for unavailable/unlimited and a non-nil value for an enforced limit.
type PackageLimits struct {
	MaxDomains           int64           `json:"maxDomains"`
	MaxDatabases         int64           `json:"maxDatabases"`
	MaxDatabaseUsers     int64           `json:"maxDatabaseUsers"`
	MaxScheduledJobs     int64           `json:"maxScheduledJobs"`
	MaxOCIApplications   int64           `json:"maxOciApplications"`
	CPUQuotaPercent      *int64          `json:"cpuQuotaPercent"`
	CPUWeight            *int64          `json:"cpuWeight"`
	MemoryBytes          *int64          `json:"memoryBytes"`
	SwapBytes            *int64          `json:"swapBytes"`
	ProcessLimit         *int64          `json:"processLimit"`
	StorageBytes         *int64          `json:"storageBytes"`
	BackupStorageBytes   *int64          `json:"backupStorageBytes"`
	StorageInodes        *int64          `json:"storageInodes"`
	ReadBytesPerSecond   *int64          `json:"readBytesPerSecond"`
	WriteBytesPerSecond  *int64          `json:"writeBytesPerSecond"`
	ReadIOPS             *int64          `json:"readIops"`
	WriteIOPS            *int64          `json:"writeIops"`
	MonthlyIngressBytes  *int64          `json:"monthlyIngressBytes"`
	MonthlyEgressBytes   *int64          `json:"monthlyEgressBytes"`
	MonthlyCombinedBytes *int64          `json:"monthlyCombinedBytes"`
	AllowedPHPVersions   []string        `json:"allowedPhpVersions"`
	Features             PackageFeatures `json:"features"`
}

type PackageFeatures struct {
	OCIApplications  bool `json:"ociApplications"`
	CustomRedirects  bool `json:"customRedirects"`
	WAFExceptions    bool `json:"wafExceptions"`
	ScheduledBackups bool `json:"scheduledBackups"`
}

type OCIApplicationStatus string

const (
	OCIApplicationDraft     OCIApplicationStatus = "draft"
	OCIApplicationPending   OCIApplicationStatus = "pending"
	OCIApplicationActive    OCIApplicationStatus = "active"
	OCIApplicationSuspended OCIApplicationStatus = "suspended"
	OCIApplicationError     OCIApplicationStatus = "error"
	OCIApplicationDeleting  OCIApplicationStatus = "deleting"
	OCIApplicationDeleted   OCIApplicationStatus = "deleted"
)

type OCIApplication struct {
	ID              ID                   `json:"id"`
	AccountID       ID                   `json:"accountId"`
	Name            string               `json:"name"`
	Slug            string               `json:"slug"`
	Spec            ociapps.Spec         `json:"spec"`
	Status          OCIApplicationStatus `json:"status"`
	Revision        int64                `json:"revision"`
	AppliedRevision *int64               `json:"appliedRevision,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
	RemovedAt       *time.Time           `json:"removedAt,omitempty"`
}

type CreateOCIApplicationParams struct {
	AccountID ID
	Name      string
	Slug      string
	Spec      ociapps.Spec
	ActorID   ID
	RequestID string
}

type UpdateOCIApplicationDraftParams struct {
	AccountID        ID
	ApplicationID    ID
	ExpectedRevision int64
	Name             string
	Slug             string
	Spec             ociapps.Spec
	ActorID          ID
	RequestID        string
}

type RemoveOCIApplicationDraftParams struct {
	AccountID        ID
	ApplicationID    ID
	ExpectedRevision int64
	ActorID          ID
	RequestID        string
}

type OCIImageArtifact struct {
	ApplicationID        ID              `json:"applicationId"`
	AccountID            ID              `json:"accountId"`
	ApplicationRevision  int64           `json:"applicationRevision"`
	Result               ociimage.Result `json:"result"`
	PreparedAt           time.Time       `json:"preparedAt"`
	PreparedByIdentityID ID              `json:"preparedByIdentityId"`
}

type RecordOCIImageArtifactParams struct {
	AccountID        ID
	ApplicationID    ID
	ExpectedRevision int64
	Result           ociimage.Result
	ActorID          ID
	RequestID        string
}

type OCIResourceArtifact struct {
	ApplicationID        ID                  `json:"applicationId"`
	AccountID            ID                  `json:"accountId"`
	ApplicationRevision  int64               `json:"applicationRevision"`
	Result               ociresources.Result `json:"result"`
	PreparedAt           time.Time           `json:"preparedAt"`
	PreparedByIdentityID ID                  `json:"preparedByIdentityId"`
}

type RecordOCIResourceArtifactParams struct {
	AccountID        ID
	ApplicationID    ID
	ExpectedRevision int64
	Result           ociresources.Result
	ActorID          ID
	RequestID        string
}

type OCIDeploymentArtifact struct {
	ApplicationID        ID                   `json:"applicationId"`
	AccountID            ID                   `json:"accountId"`
	ApplicationRevision  int64                `json:"applicationRevision"`
	Result               ocideployment.Result `json:"result"`
	DeployedAt           time.Time            `json:"deployedAt"`
	DeployedByIdentityID ID                   `json:"deployedByIdentityId"`
}

type OCIApplicationUpstream struct {
	ApplicationID ID    `json:"applicationId"`
	LoopbackPort  int64 `json:"loopbackPort"`
}

type RecordOCIDeploymentArtifactParams struct {
	AccountID        ID
	ApplicationID    ID
	ExpectedRevision int64
	Result           ocideployment.Result
	ActorID          ID
	RequestID        string
}

type ChangeOCIApplicationDeploymentStatusParams struct {
	AccountID     ID
	ApplicationID ID
	Expected      OCIApplicationStatus
	Status        OCIApplicationStatus
	ActorID       ID
	OperationID   ID
	RequestID     string
}

type OCIEnvironmentSecret struct {
	ID         ID         `json:"id"`
	AccountID  ID         `json:"accountId"`
	Name       string     `json:"name"`
	Slug       string     `json:"slug"`
	Generation int64      `json:"generation"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	RemovedAt  *time.Time `json:"removedAt,omitempty"`
}

type CreateOCIEnvironmentSecretParams struct {
	AccountID ID
	Name      string
	Slug      string
	Value     []byte
	ActorID   ID
	RequestID string
}

type RotateOCIEnvironmentSecretParams struct {
	AccountID          ID
	SecretID           ID
	ExpectedGeneration int64
	Value              []byte
	ActorID            ID
	RequestID          string
}

type RemoveOCIEnvironmentSecretParams struct {
	AccountID          ID
	SecretID           ID
	ExpectedGeneration int64
	ActorID            ID
	RequestID          string
}

type OCIVolume struct {
	ID        ID         `json:"id"`
	AccountID ID         `json:"accountId"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	RemovedAt *time.Time `json:"removedAt,omitempty"`
}

type CreateOCIVolumeParams struct {
	AccountID ID
	Name      string
	Slug      string
	ActorID   ID
	RequestID string
}

type RemoveOCIVolumeParams struct {
	AccountID ID
	VolumeID  ID
	ActorID   ID
	RequestID string
}

type Package struct {
	ID              ID
	Name            string
	Slug            string
	Status          PackageStatus
	CurrentRevision int64
	Limits          PackageLimits
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreatePackageParams struct {
	Name      string
	Slug      string
	Limits    PackageLimits
	ActorID   *ID
	RequestID string
}

type UpdatePackageParams struct {
	PackageID        ID
	ExpectedRevision int64
	Name             string
	Limits           PackageLimits
	ActorID          *ID
	RequestID        string
}

type AccountStatus string

const (
	AccountActive    AccountStatus = "active"
	AccountSuspended AccountStatus = "suspended"
	AccountArchived  AccountStatus = "archived"
)

type HostingAccount struct {
	ID                         ID
	Name                       string
	Slug                       string
	Status                     AccountStatus
	CurrentPackageAssignmentID ID
	UnixIdentity               HostingUnixIdentity
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// HostingAccountSummary is the bounded administrator list representation. It
// intentionally excludes host identity internals while retaining the current
// immutable package assignment.
type HostingAccountSummary struct {
	ID                         ID
	Name                       string
	Slug                       string
	Status                     AccountStatus
	CurrentPackageAssignmentID ID
	PackageID                  ID
	PackageName                string
	PackageRevision            int64
	HostReady                  bool
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// SelfServiceAccount is the bounded account-owner workspace representation.
// It contains only the caller's active membership, the immutable effective
// package snapshot, and usage counters backed by current control-plane data.
type SelfServiceAccount struct {
	ID              ID
	Name            string
	Slug            string
	Status          AccountStatus
	MembershipRole  MembershipRole
	PackageID       ID
	PackageName     string
	PackageRevision int64
	EffectiveLimits PackageLimits
	DomainCount     int64
	HostReady       bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SelfServiceContext struct {
	PlatformAdministrator bool
	Accounts              []SelfServiceAccount
}

type GetSelfServiceContextParams struct {
	Subject AuthorizationSubject
}

type UpdateOwnProfileParams struct {
	Subject       AuthorizationSubject
	Email         string
	DisplayName   string
	Locale        Locale
	RequestID     string
	SourceAddress string
}

type HostingFilesystemStatus string

const (
	HostingFilesystemPending HostingFilesystemStatus = "pending"
	HostingFilesystemApplied HostingFilesystemStatus = "applied"
	HostingFilesystemBlocked HostingFilesystemStatus = "blocked"
)

type HostingFilesystemCapabilityStatus string

const (
	HostingFilesystemCapabilityPending     HostingFilesystemCapabilityStatus = "pending"
	HostingFilesystemCapabilityAvailable   HostingFilesystemCapabilityStatus = "available"
	HostingFilesystemCapabilityUnavailable HostingFilesystemCapabilityStatus = "unavailable"
	HostingFilesystemCapabilityUnsupported HostingFilesystemCapabilityStatus = "unsupported"
	HostingFilesystemCapabilityUnknown     HostingFilesystemCapabilityStatus = "unknown"
)

// HostingFilesystemState records both package intent and the last host state
// confirmed by a correlated privileged operation. Nil limits mean unlimited.
type HostingFilesystemState struct {
	AccountID            ID
	ProjectID            uint32
	DesiredStorageBytes  *int64
	DesiredStorageInodes *int64
	AppliedStorageBytes  *int64
	AppliedStorageInodes *int64
	Revision             int64
	Status               HostingFilesystemStatus
	CapabilityStatus     HostingFilesystemCapabilityStatus
	ReasonCode           string
	UpdatedAt            time.Time
	AppliedAt            *time.Time
	LastOperationID      *ID
}

type ConfirmHostingFilesystemAppliedParams struct {
	AccountID        ID
	ExpectedRevision int64
	OperationID      ID
	ActorID          *ID
	RequestID        string
}

type ConfirmHostingFilesystemBlockedParams struct {
	AccountID        ID
	ExpectedRevision int64
	OperationID      ID
	CapabilityStatus HostingFilesystemCapabilityStatus
	ReasonCode       string
	ActorID          *ID
	RequestID        string
}

type HostingResourceStatus string

const (
	HostingResourcePending HostingResourceStatus = "pending"
	HostingResourceApplied HostingResourceStatus = "applied"
	HostingResourceBlocked HostingResourceStatus = "blocked"
)

type HostingResourceCapabilityStatus string

const (
	HostingResourceCapabilityPending     HostingResourceCapabilityStatus = "pending"
	HostingResourceCapabilityAvailable   HostingResourceCapabilityStatus = "available"
	HostingResourceCapabilityUnavailable HostingResourceCapabilityStatus = "unavailable"
	HostingResourceCapabilityUnsupported HostingResourceCapabilityStatus = "unsupported"
	HostingResourceCapabilityUnknown     HostingResourceCapabilityStatus = "unknown"
)

// HostingResourceState records the revisioned package intent and the last
// cgroup state confirmed by a correlated privileged operation. Nil values mean
// unlimited/default; a non-nil zero is valid only for swap and disables it.
type HostingResourceState struct {
	AccountID              ID
	DesiredCPUQuotaPercent *int64
	DesiredCPUWeight       *int64
	DesiredMemoryBytes     *int64
	DesiredSwapBytes       *int64
	DesiredProcessLimit    *int64
	AppliedCPUQuotaPercent *int64
	AppliedCPUWeight       *int64
	AppliedMemoryBytes     *int64
	AppliedSwapBytes       *int64
	AppliedProcessLimit    *int64
	Revision               int64
	Status                 HostingResourceStatus
	CapabilityStatus       HostingResourceCapabilityStatus
	ReasonCode             string
	UpdatedAt              time.Time
	AppliedAt              *time.Time
	LastOperationID        *ID
}

type ConfirmHostingResourcesAppliedParams struct {
	AccountID        ID
	ExpectedRevision int64
	OperationID      ID
	ActorID          *ID
	RequestID        string
}

type ConfirmHostingResourcesBlockedParams struct {
	AccountID        ID
	ExpectedRevision int64
	OperationID      ID
	CapabilityStatus HostingResourceCapabilityStatus
	ReasonCode       string
	ActorID          *ID
	RequestID        string
}

type HostingUnixIdentityState string

const (
	HostingUnixIdentityAllocated         HostingUnixIdentityState = "allocated"
	HostingUnixIdentityReconciled        HostingUnixIdentityState = "reconciled"
	HostingUnixIdentityArchiveRequested  HostingUnixIdentityState = "archive_requested"
	HostingUnixIdentityArchived          HostingUnixIdentityState = "archived"
	HostingUnixIdentityDeletionRequested HostingUnixIdentityState = "deletion_requested"
	HostingUnixIdentityDeleted           HostingUnixIdentityState = "deleted"
)

// HostingUnixIdentity is allocated once and retained as a tombstone after
// deletion. Username, numeric IDs, and home directory are immutable.
type HostingUnixIdentity struct {
	AccountID              ID
	Username               string
	UID                    uint32
	GID                    uint32
	HomeDirectory          string
	State                  HostingUnixIdentityState
	AllocatedAt            time.Time
	ReconciledAt           *time.Time
	OCIRuntimeReconciledAt *time.Time
	ArchiveRequestedAt     *time.Time
	ArchivedAt             *time.Time
	ArchiveReference       string
	DeletionRequestedAt    *time.Time
	DeletedAt              *time.Time
}

type CreateHostingAccountParams struct {
	Name            string
	Slug            string
	OwnerIdentityID ID
	PackageID       ID
	ActorID         *ID
	RequestID       string
}

type HostingAccountLifecycleParams struct {
	AccountID   ID
	ActorID     *ID
	OperationID *ID
	RequestID   string
}

type ConfirmHostingAccountArchiveParams struct {
	AccountID        ID
	ArchiveReference string
	ActorID          *ID
	OperationID      *ID
	RequestID        string
}

type MembershipRole string

const (
	MembershipOwner   MembershipRole = "owner"
	MembershipMember  MembershipRole = "member"
	MembershipAuditor MembershipRole = "auditor"
)

type Membership struct {
	ID         ID
	AccountID  ID
	IdentityID ID
	Role       MembershipRole
	GrantedAt  time.Time
}

type AddMembershipParams struct {
	AccountID  ID
	IdentityID ID
	Role       MembershipRole
	ActorID    *ID
	RequestID  string
}

type PackageAssignment struct {
	ID              ID
	AccountID       ID
	PackageID       ID
	PackageRevision int64
	EffectiveLimits PackageLimits
	AssignedAt      time.Time
}

type AssignPackageParams struct {
	AccountID ID
	PackageID ID
	ActorID   *ID
	RequestID string
}

type DesiredStateRevision struct {
	ID        ID
	AccountID ID
	Sequence  int64
	Document  map[string]any
	Reason    string
	CreatedAt time.Time
}

type CreateDesiredStateRevisionParams struct {
	AccountID   ID
	Document    map[string]any
	Reason      string
	OperationID *ID
	ActorID     *ID
	RequestID   string
}

type OperationStatus string

const (
	OperationPending    OperationStatus = "pending"
	OperationRunning    OperationStatus = "running"
	OperationSucceeded  OperationStatus = "succeeded"
	OperationFailed     OperationStatus = "failed"
	OperationCancelling OperationStatus = "cancelling"
	OperationCancelled  OperationStatus = "cancelled"
)

type RetryClass string

const (
	RetryNone   RetryClass = "none"
	RetrySafe   RetryClass = "safe"
	RetryManual RetryClass = "manual"
)

type Operation struct {
	ID                      ID
	AccountID               *ID
	ActorID                 *ID
	Kind                    string
	Status                  OperationStatus
	Stage                   string
	ProgressPercent         int64
	RetryClass              RetryClass
	RequestID               string
	IdempotencyKey          string
	Payload                 map[string]any
	Result                  map[string]any
	ErrorCode               string
	MaxAttempts             int64
	AttemptCount            int64
	NextAttemptAt           *time.Time
	CurrentAttemptID        *ID
	WorkerInstanceID        *ID
	LeaseExpiresAt          *time.Time
	CancellationRequestedAt *time.Time
	CancellationRequestedBy *ID
	CreatedAt               time.Time
	UpdatedAt               time.Time
	StartedAt               *time.Time
	CompletedAt             *time.Time
}

type CreateOperationParams struct {
	AccountID      *ID
	ActorID        *ID
	Kind           string
	RetryClass     RetryClass
	RequestID      string
	IdempotencyKey string
	Payload        map[string]any
	MaxAttempts    int64
}

type OperationAttemptOutcome string

const (
	OperationAttemptRunning      OperationAttemptOutcome = "running"
	OperationAttemptSucceeded    OperationAttemptOutcome = "succeeded"
	OperationAttemptFailed       OperationAttemptOutcome = "failed"
	OperationAttemptCancelled    OperationAttemptOutcome = "cancelled"
	OperationAttemptLeaseExpired OperationAttemptOutcome = "lease_expired"
)

type OperationAttempt struct {
	ID               ID
	OperationID      ID
	AttemptNumber    int64
	WorkerInstanceID ID
	ClaimedAt        time.Time
	HeartbeatAt      time.Time
	LeaseExpiresAt   time.Time
	CompletedAt      *time.Time
	Outcome          OperationAttemptOutcome
	ErrorCode        string
}

type OperationEventType string

const (
	OperationEventCreated               OperationEventType = "created"
	OperationEventClaimed               OperationEventType = "claimed"
	OperationEventProgress              OperationEventType = "progress"
	OperationEventRetryScheduled        OperationEventType = "retry_scheduled"
	OperationEventCancellationRequested OperationEventType = "cancellation_requested"
	OperationEventSucceeded             OperationEventType = "succeeded"
	OperationEventFailed                OperationEventType = "failed"
	OperationEventCancelled             OperationEventType = "cancelled"
	OperationEventLeaseExpired          OperationEventType = "lease_expired"
)

type OperationEvent struct {
	ID              ID
	OperationID     ID
	Sequence        int64
	AttemptID       *ID
	Type            OperationEventType
	Stage           string
	ProgressPercent int64
	MessageCode     string
	Details         map[string]any
	OccurredAt      time.Time
}

// OperationScope distinguishes an account-owned operation from a global
// administrator operation. Callers must supply the expected scope on reads and
// user-initiated mutations.
type OperationScope struct {
	AccountID   *ID
	OperationID ID
}

type ClaimOperationParams struct {
	WorkerInstanceID ID
	Kinds            []string
	LeaseDuration    time.Duration
}

type ClaimedOperation struct {
	Operation Operation
	Attempt   OperationAttempt
}

type HeartbeatOperationParams struct {
	OperationID      ID
	AttemptID        ID
	WorkerInstanceID ID
	LeaseDuration    time.Duration
}

type CheckpointOperationParams struct {
	OperationID      ID
	AttemptID        ID
	WorkerInstanceID ID
	Stage            string
	ProgressPercent  int64
	MessageCode      string
	Details          map[string]any
}

type CompleteOperationParams struct {
	OperationID      ID
	AttemptID        ID
	WorkerInstanceID ID
	Result           map[string]any
}

type FailOperationParams struct {
	OperationID      ID
	AttemptID        ID
	WorkerInstanceID ID
	ErrorCode        string
	Result           map[string]any
	Retry            bool
}

type RequestOperationCancellationParams struct {
	Scope     OperationScope
	ActorID   *ID
	RequestID string
}

type AcknowledgeOperationCancellationParams struct {
	OperationID      ID
	AttemptID        ID
	WorkerInstanceID ID
}

type RetryOperationParams struct {
	Scope     OperationScope
	ActorID   *ID
	RequestID string
}

type ListOperationEventsParams struct {
	Scope         OperationScope
	AfterSequence int64
	Limit         int
}

type AuditResult string

const (
	AuditSuccess AuditResult = "success"
	AuditFailure AuditResult = "failure"
	AuditDenied  AuditResult = "denied"
)

type AuditEvent struct {
	Sequence      int64
	ID            ID
	OccurredAt    time.Time
	ActorID       *ID
	SessionID     *ID
	SourceAddress string
	Action        string
	TargetType    string
	TargetID      string
	AccountID     *ID
	RequestID     string
	OperationID   *ID
	Result        AuditResult
	Details       map[string]any
	PreviousHash  []byte
	EventHash     []byte
}

type AppendAuditEventParams struct {
	ActorID       *ID
	SessionID     *ID
	SourceAddress string
	Action        string
	TargetType    string
	TargetID      string
	AccountID     *ID
	RequestID     string
	OperationID   *ID
	Result        AuditResult
	Details       map[string]any
}

type ManagedDatabaseStatus string

const (
	ManagedDatabasePending  ManagedDatabaseStatus = "pending"
	ManagedDatabaseActive   ManagedDatabaseStatus = "active"
	ManagedDatabaseDeleting ManagedDatabaseStatus = "deleting"
	ManagedDatabaseError    ManagedDatabaseStatus = "error"
	ManagedDatabaseDeleted  ManagedDatabaseStatus = "deleted"
)

type DatabaseGrantPreset string

const (
	DatabaseGrantReadOnly  DatabaseGrantPreset = "read_only"
	DatabaseGrantReadWrite DatabaseGrantPreset = "read_write"
)

// ManagedDatabase and ManagedDatabaseUser are safe read models. Credentials
// are deliberately absent and may only be loaded by the internal worker or a
// separately authorized one-time reveal operation.
type ManagedDatabase struct {
	ID           ID
	AccountID    ID
	Alias        string
	PhysicalName string
	Status       ManagedDatabaseStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
	RemovedAt    *time.Time
}

type ManagedDatabaseUser struct {
	ID           ID
	AccountID    ID
	Alias        string
	PhysicalName string
	Host         string
	Status       ManagedDatabaseStatus
	Revealed     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	RemovedAt    *time.Time
}

type ManagedDatabaseGrant struct {
	ID             ID
	AccountID      ID
	DatabaseID     ID
	DatabaseUserID ID
	Preset         DatabaseGrantPreset
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RevokedAt      *time.Time
}

type DatabaseWorkspace struct {
	Databases []ManagedDatabase
	Users     []ManagedDatabaseUser
	Grants    []ManagedDatabaseGrant
}

type PrepareDatabaseWizardParams struct {
	AccountID      ID
	DatabaseAlias  string
	ExistingUserID *ID
	NewUserAlias   string
	Preset         DatabaseGrantPreset
	ActorID        ID
	RequestID      string
	IdempotencyKey string
}

type ManagedDatabaseProvisioning struct {
	Operation    Operation
	Database     ManagedDatabase
	DatabaseUser ManagedDatabaseUser
	Grant        ManagedDatabaseGrant
}

type DatabaseDeletionKind string

const (
	DatabaseDeletionDatabase DatabaseDeletionKind = "database"
	DatabaseDeletionUser     DatabaseDeletionKind = "user"
)

type PrepareDatabaseDeletionParams struct {
	AccountID      ID
	TargetKind     DatabaseDeletionKind
	TargetID       ID
	Confirmation   string
	ActorID        ID
	RequestID      string
	IdempotencyKey string
}

type ManagedDatabaseDeletion struct {
	Operation  Operation
	Kind       DatabaseDeletionKind
	Database   *ManagedDatabase
	User       *ManagedDatabaseUser
	Grants     []ManagedDatabaseGrant
	GrantUsers []ManagedDatabaseUser
}

type CompleteDatabaseDeletionParams struct {
	OperationID ID
	AccountID   ID
	ActorID     *ID
	RequestID   string
}

type CompleteDatabaseProvisioningParams struct {
	OperationID ID
	AccountID   ID
	ActorID     *ID
	RequestID   string
}

type DatabaseCredential struct {
	AccountID ID
	UserID    ID
	Username  string
	Host      string
	Password  []byte
}

type RevealDatabaseCredentialParams struct {
	Subject   AuthorizationSubject
	AccountID ID
	UserID    ID
	RequestID string
}

type RevealedDatabaseCredential struct {
	Username string
	Host     string
	Password []byte
}

type PrepareDatabaseCredentialRotationParams struct {
	Subject        AuthorizationSubject
	AccountID      ID
	DatabaseUserID ID
	RequestID      string
	IdempotencyKey string
}

type ManagedDatabaseCredentialRotation struct {
	Operation    Operation
	DatabaseUser ManagedDatabaseUser
	AppliedAt    *time.Time
}

type CompleteDatabaseCredentialRotationParams struct {
	OperationID ID
	AccountID   ID
	ActorID     *ID
	RequestID   string
}

type IssuePHPMyAdminHandoffParams struct {
	Subject        AuthorizationSubject
	AccountID      ID
	DatabaseUserID ID
	RequestID      string
}

type PHPMyAdminHandoff struct {
	Token     string
	ExpiresAt time.Time
}

type RedeemPHPMyAdminHandoffParams struct {
	Token    string
	Audience string
}

type PHPMyAdminCredential struct {
	Username string
	Host     string
	Password []byte
}

type ACMEEnvironment string

const (
	ACMELetsEncryptStaging    ACMEEnvironment = "letsencrypt-staging"
	ACMELetsEncryptProduction ACMEEnvironment = "letsencrypt-production"
)

type ACMEAccountStatus string

const (
	ACMEAccountPending     ACMEAccountStatus = "pending"
	ACMEAccountValid       ACMEAccountStatus = "valid"
	ACMEAccountDeactivated ACMEAccountStatus = "deactivated"
	ACMEAccountRevoked     ACMEAccountStatus = "revoked"
)

// ACMEAccount deliberately contains no private-key material. The encrypted
// credential is available only through the internal registration workflow.
type ACMEAccount struct {
	ID                  ID
	Environment         ACMEEnvironment
	DirectoryURL        string
	ContactEmail        string
	Status              ACMEAccountStatus
	AccountURI          string
	OrdersURL           string
	TermsURL            string
	TermsAgreedAt       time.Time
	PublicKeyThumbprint string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	RegisteredAt        *time.Time
}

type EnsureACMEAccountParams struct {
	Environment   ACMEEnvironment
	ContactEmail  string
	TermsAccepted bool
	ActorID       *ID
	OperationID   *ID
	RequestID     string
}

type CompleteACMERegistrationParams struct {
	AccountID   ID
	AccountURI  string
	OrdersURL   string
	TermsURL    string
	Status      ACMEAccountStatus
	ActorID     *ID
	OperationID *ID
	RequestID   string
}

type TLSCertificateStatus string

const (
	TLSCertificateOrdering TLSCertificateStatus = "ordering"
	TLSCertificateStaged   TLSCertificateStatus = "staged"
	TLSCertificateActive   TLSCertificateStatus = "active"
	TLSCertificateRetired  TLSCertificateStatus = "retired"
)

type TLSCertificatePurpose string

const (
	TLSCertificateIssue TLSCertificatePurpose = "issue"
	TLSCertificateRenew TLSCertificatePurpose = "renew"
)

// TLSCertificate contains certificate metadata and the public chain only.
// Its encrypted private key is exposed solely through the internal worker
// method LoadTLSCertificateSigner.
type TLSCertificate struct {
	ID                ID
	AccountID         ID
	DomainID          ID
	ACMEAccountID     ID
	Status            TLSCertificateStatus
	Names             []string
	FullChainPEM      string
	CertificateURL    string
	FingerprintSHA256 string
	Issuer            string
	SerialHex         string
	NotBefore         *time.Time
	ExpiresAt         *time.Time
	NextRenewalAt     *time.Time
	CreatedAt         time.Time
	IssuedAt          *time.Time
	ActivatedAt       *time.Time
	RetiredAt         *time.Time
}

type TLSCertificateOrder struct {
	OperationID           ID
	AccountID             ID
	DomainID              ID
	CertificateID         ID
	Purpose               TLSCertificatePurpose
	ReplacesCertificateID *ID
	OrderURL              string
	CreatedAt             time.Time
	Certificate           TLSCertificate
	ACMEAccount           ACMEAccount
}

type PrepareTLSCertificateOrderParams struct {
	AccountID             ID
	DomainID              ID
	OperationID           ID
	Environment           ACMEEnvironment
	ReplacesCertificateID *ID
	ActorID               *ID
	RequestID             string
}

type StageTLSCertificateParams struct {
	OperationID       ID
	CertificateID     ID
	FullChainPEM      string
	CertificateURL    string
	FingerprintSHA256 string
	Issuer            string
	SerialHex         string
	NotBefore         time.Time
	ExpiresAt         time.Time
	NextRenewalAt     time.Time
	ActorID           *ID
	RequestID         string
}

type ActivateTLSCertificateParams struct {
	AccountID              ID
	DomainID               ID
	CertificateID          ID
	DesiredStateRevisionID ID
	OperationID            ID
	ActorID                *ID
	RequestID              string
}

type FailTLSCertificateOrderParams struct {
	OperationID ID
	ErrorCode   string
	Final       bool
	RetryAt     *time.Time
	ActorID     *ID
	RequestID   string
}

type PendingTLSCertificateIssuance struct {
	AccountID   ID
	DomainID    ID
	Environment ACMEEnvironment
}

type DueTLSCertificateRenewal struct {
	AccountID     ID
	DomainID      ID
	CertificateID ID
	Environment   ACMEEnvironment
	NextRenewalAt time.Time
}

// NormalizedDomainName contains the stable Unicode form shown to people and
// the lowercase ASCII form used for routing, certificates, and uniqueness.
type NormalizedDomainName struct {
	Display string
	ASCII   string
}

type DomainStatus string

const (
	DomainPending   DomainStatus = "pending"
	DomainActive    DomainStatus = "active"
	DomainSuspended DomainStatus = "suspended"
	DomainRemoved   DomainStatus = "removed"
)

type CanonicalMode string

const (
	CanonicalPreferApex CanonicalMode = "prefer_apex"
	CanonicalPreferWWW  CanonicalMode = "prefer_www"
	CanonicalServeBoth  CanonicalMode = "serve_both"
)

type DocumentRootMode string

const (
	DocumentRootDefault DocumentRootMode = "default"
	DocumentRootCustom  DocumentRootMode = "custom"
	DocumentRootShared  DocumentRootMode = "shared"
)

type DomainTargetType string

const (
	DomainTargetStatic         DomainTargetType = "static"
	DomainTargetPHP            DomainTargetType = "php"
	DomainTargetOCIApplication DomainTargetType = "oci_application"
	DomainTargetRedirect       DomainTargetType = "redirect"
)

type DocumentRoot struct {
	ID             ID
	AccountID      ID
	RelativePath   string
	ReferenceCount int64
	CreatedAt      time.Time
}

type RedirectStatusCode int64

const (
	RedirectPermanent RedirectStatusCode = 301
	RedirectTemporary RedirectStatusCode = 302
)

type RedirectHostMode string

const (
	RedirectHostApexOnly RedirectHostMode = "apex_only"
	RedirectHostWWWOnly  RedirectHostMode = "www_only"
	RedirectHostBoth     RedirectHostMode = "both"
)

type DomainRedirect struct {
	ID                 ID
	StatusCode         RedirectStatusCode
	TargetURL          string
	TargetASCIIHost    string
	HostMode           RedirectHostMode
	PreservePath       bool
	PreserveQuery      bool
	WildcardSubdomains bool
	CreatedAt          time.Time
}

type RedirectSpec struct {
	StatusCode         RedirectStatusCode `json:"statusCode"`
	TargetURL          string             `json:"targetUrl"`
	HostMode           RedirectHostMode   `json:"hostMode,omitempty"`
	PreservePath       bool               `json:"preservePath"`
	PreserveQuery      bool               `json:"preserveQuery"`
	WildcardSubdomains bool               `json:"wildcardSubdomains"`
}

// DomainTargetSpec is a tagged union. Static and PHP targets use a document
// root, OCI targets use ApplicationID, and redirects use Redirect.
type DomainTargetSpec struct {
	Type               DomainTargetType `json:"type"`
	RootMode           DocumentRootMode `json:"rootMode,omitempty"`
	DocumentRoot       string           `json:"documentRoot,omitempty"`
	SharedWithDomainID *ID              `json:"sharedWithDomainId,omitempty"`
	PHPVersion         string           `json:"phpVersion,omitempty"`
	ApplicationID      *ID              `json:"applicationId,omitempty"`
	Redirect           *RedirectSpec    `json:"redirect,omitempty"`
}

type DomainTarget struct {
	ID            ID
	Type          DomainTargetType
	DocumentRoot  *DocumentRoot
	PHPVersion    string
	ApplicationID *ID
	Redirect      *DomainRedirect
	CreatedAt     time.Time
	SupersededAt  *time.Time
}

type TLSMode string

const (
	TLSModeACME     TLSMode = "acme"
	TLSModeImported TLSMode = "imported"
)

type TLSChallengeType string

const (
	TLSChallengeHTTP01   TLSChallengeType = "http-01"
	TLSChallengeDNS01    TLSChallengeType = "dns-01"
	TLSChallengeImported TLSChallengeType = "imported"
)

type TLSIssuanceStatus string

const (
	TLSDisabled TLSIssuanceStatus = "disabled"
	TLSPending  TLSIssuanceStatus = "pending"
	TLSIssuing  TLSIssuanceStatus = "issuing"
	TLSActive   TLSIssuanceStatus = "active"
	TLSRenewing TLSIssuanceStatus = "renewing"
	TLSFailed   TLSIssuanceStatus = "failed"
)

type DomainTLSState struct {
	Enabled              bool
	Mode                 TLSMode
	ChallengeType        TLSChallengeType
	IssuanceStatus       TLSIssuanceStatus
	Names                []string
	ActiveCertificateRef string
	Issuer               string
	NotBefore            *time.Time
	ExpiresAt            *time.Time
	NextRenewalAt        *time.Time
	LastErrorCode        string
	LastErrorAt          *time.Time
	UpdatedAt            time.Time
}

type WAFMode = wafconfig.Mode

const (
	WAFModeOff           = wafconfig.ModeOff
	WAFModeDetectionOnly = wafconfig.ModeDetectionOnly
	WAFModeBlockingPL1   = wafconfig.ModeBlockingPL1
)

type DomainWAFPolicy struct {
	Mode       WAFMode
	Exceptions []DomainWAFException
	UpdatedAt  time.Time
}

type CachePreset = cacheconfig.Preset

const (
	CachePresetDisabled      = cacheconfig.PresetDisabled
	CachePresetRespectOrigin = cacheconfig.PresetRespectOrigin
	CachePresetWordPress     = cacheconfig.PresetWordPress
)

type DomainCachePolicy struct {
	Preset    CachePreset
	UpdatedAt time.Time
}

type DomainWAFException struct {
	ID          ID        `json:"id"`
	AccountID   ID        `json:"-"`
	DomainID    ID        `json:"-"`
	RuleID      uint32    `json:"ruleId"`
	RequestPath string    `json:"requestPath,omitempty"`
	Parameter   string    `json:"parameter,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Domain struct {
	ID            ID
	AccountID     ID
	Name          NormalizedDomainName
	Status        DomainStatus
	CanonicalMode CanonicalMode
	Target        DomainTarget
	TLS           DomainTLSState
	WAF           DomainWAFPolicy
	Cache         DomainCachePolicy
	CreatedAt     time.Time
	UpdatedAt     time.Time
	RemovedAt     *time.Time
}

type CreateDomainParams struct {
	AccountID     ID
	DomainID      *ID
	Name          string
	CanonicalMode CanonicalMode
	Target        DomainTargetSpec
	DisableTLS    bool
	TLSMode       TLSMode
	WAFMode       WAFMode
	CachePreset   CachePreset
	OperationID   *ID
	ActorID       *ID
	RequestID     string
}

type ReplaceDomainTargetParams struct {
	AccountID   ID
	DomainID    ID
	Target      DomainTargetSpec
	OperationID *ID
	ActorID     *ID
	RequestID   string
}

type UpdateDomainParams struct {
	AccountID     ID
	DomainID      ID
	CanonicalMode *CanonicalMode
	Target        *DomainTargetSpec
	WAFMode       *WAFMode
	CachePreset   *CachePreset
	OperationID   *ID
	ActorID       *ID
	RequestID     string
}

type ChangeDomainStatusParams struct {
	AccountID   ID
	DomainID    ID
	OperationID *ID
	ActorID     *ID
	RequestID   string
}

type RemoveDomainParams struct {
	AccountID   ID
	DomainID    ID
	OperationID *ID
	ActorID     *ID
	RequestID   string
}

type CreateDomainWAFExceptionParams struct {
	AccountID   ID
	DomainID    ID
	ExceptionID ID
	RuleID      uint32
	RequestPath string
	Parameter   string
	ExpiresAt   time.Time
	OperationID ID
	ActorID     *ID
	RequestID   string
}

type RemoveDomainWAFExceptionParams struct {
	AccountID   ID
	DomainID    ID
	ExceptionID ID
	OperationID ID
	ActorID     *ID
	RequestID   string
}

type DomainActivationExpectation struct {
	DomainID ID `json:"domainId"`
	TargetID ID `json:"targetId"`
}

type ConfirmDomainActivationParams struct {
	AccountID              ID
	DesiredStateRevisionID ID
	OperationID            ID
	Expected               []DomainActivationExpectation
	ActorID                *ID
	RequestID              string
}

type AppliedStateStatus string

const (
	AppliedStateActive     AppliedStateStatus = "active"
	AppliedStateSuperseded AppliedStateStatus = "superseded"
	AppliedStateRolledBack AppliedStateStatus = "rolled_back"
)

type AppliedStateRevision struct {
	ID                     ID
	AccountID              ID
	DesiredStateRevisionID ID
	OperationID            *ID
	ConfigDigest           []byte
	Status                 AppliedStateStatus
	AppliedAt              time.Time
	SupersededAt           *time.Time
}

type RecordAppliedStateRevisionParams struct {
	AccountID              ID
	DesiredStateRevisionID ID
	OperationID            *ID
	ConfigDigest           []byte
	ActorID                *ID
	RequestID              string
}
