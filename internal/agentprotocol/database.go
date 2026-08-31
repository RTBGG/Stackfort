// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/databaseidentity"
)

type DatabaseGrantPreset string

const (
	DatabaseGrantReadOnly  DatabaseGrantPreset = "read_only"
	DatabaseGrantReadWrite DatabaseGrantPreset = "read_write"
)

// DatabaseProvisionRequest is a closed MariaDB intent. Physical identifiers
// must match aliases derived from the correlated account. Password is binary so
// callers can clear their buffers after the local RPC returns.
type DatabaseProvisionRequest struct {
	DatabaseAlias string              `json:"databaseAlias"`
	DatabaseName  string              `json:"databaseName"`
	UserAlias     string              `json:"userAlias"`
	Username      string              `json:"username"`
	Host          string              `json:"host"`
	Password      []byte              `json:"password"`
	CreateUser    bool                `json:"createUser"`
	Preset        DatabaseGrantPreset `json:"preset"`
}

type DatabaseProvisionResponse struct {
	DatabaseName string              `json:"databaseName"`
	Username     string              `json:"username"`
	Host         string              `json:"host"`
	Preset       DatabaseGrantPreset `json:"preset"`
	Changed      bool                `json:"changed"`
	Active       bool                `json:"active"`
}

// DatabasePasswordRotateRequest changes one already-managed local principal.
// It intentionally carries neither arbitrary SQL nor a database name.
type DatabasePasswordRotateRequest struct {
	UserAlias string `json:"userAlias"`
	Username  string `json:"username"`
	Host      string `json:"host"`
	Password  []byte `json:"password"`
}

type DatabasePasswordRotateResponse struct {
	Username string `json:"username"`
	Host     string `json:"host"`
	Changed  bool   `json:"changed"`
	Active   bool   `json:"active"`
}

type DatabaseDropKind string

const (
	DatabaseDropDatabase DatabaseDropKind = "database"
	DatabaseDropUser     DatabaseDropKind = "user"
)

// DatabaseDropGrant identifies one Stackfort-owned database-level grant that
// must be revoked before a database disappears. MariaDB intentionally retains
// those grants after DROP DATABASE, so omitting this closed list would make a
// later database with the same name inherit stale privileges.
type DatabaseDropGrant struct {
	UserAlias string              `json:"userAlias"`
	Username  string              `json:"username"`
	Host      string              `json:"host"`
	Preset    DatabaseGrantPreset `json:"preset"`
}

type DatabaseDropRequest struct {
	Kind   DatabaseDropKind    `json:"kind"`
	Alias  string              `json:"alias"`
	Name   string              `json:"name"`
	Host   string              `json:"host,omitempty"`
	Grants []DatabaseDropGrant `json:"grants"`
}

type DatabaseDropResponse struct {
	Kind    DatabaseDropKind `json:"kind"`
	Name    string           `json:"name"`
	Changed bool             `json:"changed"`
	Deleted bool             `json:"deleted"`
}

func validateDatabaseProvisionRequest(correlation *AuditCorrelation, request DatabaseProvisionRequest) error {
	if correlation == nil || correlation.AccountID == "" ||
		databaseidentity.ValidateDerived(correlation.AccountID, request.DatabaseAlias, request.DatabaseName) != nil ||
		databaseidentity.ValidateDerived(correlation.AccountID, request.UserAlias, request.Username) != nil ||
		request.Host != databaseidentity.LocalHost ||
		(request.CreateUser && (len(request.Password) < 20 || len(request.Password) > 256)) ||
		(!request.CreateUser && len(request.Password) != 0) ||
		(request.Preset != DatabaseGrantReadOnly && request.Preset != DatabaseGrantReadWrite) {
		return errors.New("managed database provisioning intent is invalid")
	}
	return nil
}

func validateDatabaseProvisionResponse(response DatabaseProvisionResponse, operation Operation) error {
	if operation != OperationProvisionDatabase || response.DatabaseName == "" || response.Username == "" ||
		response.Host != databaseidentity.LocalHost || !response.Active ||
		(response.Preset != DatabaseGrantReadOnly && response.Preset != DatabaseGrantReadWrite) {
		return errors.New("managed database provisioning response is invalid")
	}
	return nil
}

func validateDatabasePasswordRotateRequest(
	correlation *AuditCorrelation,
	request DatabasePasswordRotateRequest,
) error {
	if correlation == nil || correlation.AccountID == "" ||
		databaseidentity.ValidateDerived(correlation.AccountID, request.UserAlias, request.Username) != nil ||
		request.Host != databaseidentity.LocalHost || len(request.Password) < 20 || len(request.Password) > 256 {
		return errors.New("managed database password rotation intent is invalid")
	}
	return nil
}

func validateDatabasePasswordRotateResponse(response DatabasePasswordRotateResponse, operation Operation) error {
	if operation != OperationRotateDatabasePassword || response.Username == "" ||
		response.Host != databaseidentity.LocalHost || !response.Active {
		return errors.New("managed database password rotation response is invalid")
	}
	return nil
}

func validateDatabaseDropRequest(correlation *AuditCorrelation, request DatabaseDropRequest) error {
	if correlation == nil || correlation.AccountID == "" ||
		databaseidentity.ValidateDerived(correlation.AccountID, request.Alias, request.Name) != nil ||
		len(request.Grants) > 256 {
		return errors.New("managed database deletion intent is invalid")
	}
	if request.Kind == DatabaseDropUser {
		if request.Host != databaseidentity.LocalHost || len(request.Grants) != 0 {
			return errors.New("managed database user deletion intent is invalid")
		}
		return nil
	}
	if request.Kind != DatabaseDropDatabase || request.Host != "" {
		return errors.New("managed database deletion target is invalid")
	}
	for _, grant := range request.Grants {
		if grant.Host != databaseidentity.LocalHost ||
			databaseidentity.ValidateDerived(correlation.AccountID, grant.UserAlias, grant.Username) != nil ||
			(grant.Preset != DatabaseGrantReadOnly && grant.Preset != DatabaseGrantReadWrite) {
			return errors.New("managed database deletion grant is invalid")
		}
	}
	return nil
}

func validateDatabaseDropResponse(response DatabaseDropResponse, operation Operation) error {
	if operation != OperationDropDatabase || !response.Deleted || response.Name == "" ||
		(response.Kind != DatabaseDropDatabase && response.Kind != DatabaseDropUser) {
		return errors.New("managed database deletion response is invalid")
	}
	return nil
}
