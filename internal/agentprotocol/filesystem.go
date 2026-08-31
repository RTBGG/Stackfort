// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"slices"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

type HostingFilesystemRequest struct {
	Storage hostingstorage.Spec `json:"storage"`
}

type HostingFilesystemResponse struct {
	ProjectID          uint32     `json:"projectId"`
	ProjectAssigned    bool       `json:"projectAssigned"`
	DirectoriesCreated []string   `json:"directoriesCreated"`
	QuotaApplied       bool       `json:"quotaApplied"`
	Capability         Capability `json:"capability"`
}

type DocumentRootAccess string

const (
	DocumentRootAccessStatic DocumentRootAccess = "static"
	DocumentRootAccessPHP    DocumentRootAccess = "php"
)

type DocumentRootRequest struct {
	Identity     hostingidentity.Spec `json:"identity"`
	RelativePath string               `json:"relativePath"`
	Access       DocumentRootAccess   `json:"access"`
}

func ValidateDocumentRootAccess(access DocumentRootAccess) error {
	if access != DocumentRootAccessStatic && access != DocumentRootAccessPHP {
		return errors.New("document root access is invalid")
	}
	return nil
}

type DocumentRootResponse struct {
	RelativePath string `json:"relativePath"`
	Created      bool   `json:"created"`
}

func validateHostingFilesystemResponse(response HostingFilesystemResponse, operation Operation) error {
	if operation != OperationReconcileFilesystem || response.ProjectID < hostingidentity.MinimumID ||
		response.ProjectID > hostingidentity.MaximumID || !response.QuotaApplied ||
		response.Capability.Status != CapabilityAvailable || response.Capability.ReasonCode != "" {
		return errors.New("agent hosting filesystem response is malformed")
	}
	allowed := map[string]struct{}{
		"public_html": {}, "domains": {}, "applications": {}, "backups": {}, "tmp": {}, "logs": {},
	}
	if response.DirectoriesCreated == nil || len(response.DirectoriesCreated) > len(allowed) ||
		!slices.IsSorted(response.DirectoriesCreated) {
		return errors.New("agent hosting filesystem directory result is malformed")
	}
	seen := make(map[string]struct{}, len(response.DirectoriesCreated))
	for _, directory := range response.DirectoriesCreated {
		if _, ok := allowed[directory]; !ok {
			return errors.New("agent hosting filesystem directory result is malformed")
		}
		if _, duplicate := seen[directory]; duplicate {
			return errors.New("agent hosting filesystem directory result contains duplicates")
		}
		seen[directory] = struct{}{}
	}
	return nil
}

func validateDocumentRootResponse(response DocumentRootResponse, operation Operation) error {
	if operation != OperationEnsureDocumentRoot {
		return errors.New("agent document root response operation is unknown")
	}
	normalized, err := hostingpath.NormalizeDocumentRoot(response.RelativePath)
	if err != nil || normalized != response.RelativePath {
		return errors.New("agent document root response is malformed")
	}
	return nil
}
