// SPDX-License-Identifier: AGPL-3.0-or-later

package ocideployment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/RTBGG/stackfort/internal/hostingoci"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

type Quadlet struct {
	FileName string
	UnitName string
	Path     string
	Content  []byte
	Digest   string
}

// RenderQuadlet emits byte-stable, administrator-owned rootless Quadlet
// source. Every option is renderer-owned; the application can select only its
// prepared image, internal port, health policy, encrypted-value references,
// and managed volumes through the closed Spec.
func RenderQuadlet(spec Spec) (Quadlet, error) {
	if Validate(spec) != nil {
		return Quadlet{}, ErrInvalid
	}
	runtime, err := hostingoci.ForIdentity(spec.Identity)
	if err != nil {
		return Quadlet{}, ErrInvalid
	}
	fileName := QuadletFileName(spec.ApplicationID)
	unitName := UnitNameFromApplication(spec.ApplicationID)
	containerName := ContainerName(spec.ApplicationID)
	var output bytes.Buffer
	output.WriteString("# Managed by Stackfort. Do not edit.\n")
	output.WriteString("[Unit]\nDescription=Stackfort OCI application ")
	output.WriteString(spec.ApplicationID)
	output.WriteString("\n\n[Container]\nImage=")
	output.WriteString(spec.ImageDigest)
	output.WriteString("\nContainerName=")
	output.WriteString(containerName)
	output.WriteString("\nPull=never\nNetwork=")
	output.WriteString(ociresources.NetworkName)
	output.WriteString("\nPublishPort=127.0.0.1:")
	output.WriteString(strconv.FormatInt(spec.LoopbackPort, 10))
	output.WriteByte(':')
	output.WriteString(strconv.FormatInt(spec.InternalPort, 10))
	output.WriteString("/tcp\nNoNewPrivileges=true\nDropCapability=all\nReadOnly=true\nReadOnlyTmpfs=true\nRunInit=true\nPidsLimit=")
	output.WriteString(strconv.Itoa(DefaultProcessLimit))
	output.WriteString("\nLogDriver=journald\nLabel=io.stackfort.managed=true\nLabel=io.stackfort.account=")
	output.WriteString(spec.Identity.AccountID)
	output.WriteString("\nLabel=io.stackfort.application=")
	output.WriteString(spec.ApplicationID)
	output.WriteString("\n")
	for _, reference := range spec.EnvironmentReferences {
		output.WriteString("Secret=")
		output.WriteString(SecretName(reference))
		output.WriteString(",type=env,target=")
		output.WriteString(reference.Environment)
		output.WriteByte('\n')
	}
	for _, volume := range spec.Volumes {
		hostPath, pathErr := ociresources.VolumePath(spec.Identity, volume.VolumeID)
		if pathErr != nil {
			return Quadlet{}, ErrInvalid
		}
		output.WriteString("Volume=")
		output.WriteString(hostPath)
		output.WriteByte(':')
		output.WriteString(volume.ContainerPath)
		if volume.ReadOnly {
			output.WriteString(":ro")
		} else {
			output.WriteString(":rw")
		}
		output.WriteByte('\n')
	}
	output.WriteString("\n[Service]\nRestart=on-failure\nRestartSec=5s\nTimeoutStartSec=120s\nTimeoutStopSec=")
	output.WriteString(strconv.Itoa(DefaultStopTimeout))
	output.WriteString("s\n\n[Install]\nWantedBy=default.target\n")
	content := output.Bytes()
	digest := sha256.Sum256(content)
	return Quadlet{FileName: fileName, UnitName: unitName, Path: runtime.QuadletRoot + "/" + fileName,
		Content: append([]byte(nil), content...), Digest: "sha256:" + hex.EncodeToString(digest[:])}, nil
}
