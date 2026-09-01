// SPDX-License-Identifier: AGPL-3.0-or-later

package ocideployment

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

func TestRenderQuadletIsFixedRootlessLoopbackOnly(t *testing.T) {
	spec := deploymentTestSpec(t)
	quadlet, err := RenderQuadlet(spec)
	if err != nil {
		t.Fatal(err)
	}
	content := string(quadlet.Content)
	for _, required := range []string{
		"Image=sha256:" + strings.Repeat("a", 64),
		"PublishPort=127.0.0.1:20042:8080/tcp", "Network=stackfort-private",
		"NoNewPrivileges=true", "DropCapability=all", "ReadOnly=true", "Pull=never",
		"Secret=sf-019d2eaa52d07f528ac70aeb932455db-g3,type=env,target=TOKEN",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("quadlet lacks %q:\n%s", required, content)
		}
	}
	for _, forbidden := range []string{"0.0.0.0", "PodmanArgs", "AddCapability", "AddDevice", "Network=host", "Environment="} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("quadlet contains forbidden surface %q", forbidden)
		}
	}
	if quadlet.UnitName != "stackfort-019d2eaa52d07f528ac70aeb932455da.service" ||
		quadlet.Path != "/etc/containers/systemd/users/200123/"+quadlet.FileName {
		t.Fatalf("derived identity = %#v", quadlet)
	}
	second, _ := RenderQuadlet(spec)
	if quadlet.Digest != second.Digest || string(quadlet.Content) != string(second.Content) {
		t.Fatal("renderer is not deterministic")
	}
}

func TestDeploymentRejectsUnboundOrMismatchedPlaintext(t *testing.T) {
	spec := deploymentTestSpec(t)
	values := []EnvironmentValue{{ValueID: spec.EnvironmentReferences[0].ValueID,
		Environment: "TOKEN", Generation: 3, Value: "private"}}
	if ValidateValues(spec, values) != nil {
		t.Fatal("valid values rejected")
	}
	values[0].Environment = "OTHER"
	if ValidateValues(spec, values) == nil {
		t.Fatal("mismatched value accepted")
	}
	spec.LoopbackPort = 80
	if Validate(spec) == nil {
		t.Fatal("public/system port accepted")
	}
}

func deploymentTestSpec(t *testing.T) Spec {
	t.Helper()
	accountID := "019d2eaa-52d0-7f52-8ac7-0aeb932455d9"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	spec, err := Normalize(Spec{
		Identity:      hostingidentity.Spec{AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home},
		ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455da", Revision: 1,
		ImageDigest: "sha256:" + strings.Repeat("a", 64), ResourceDigest: "sha256:" + strings.Repeat("b", 64),
		InternalPort: 8080, LoopbackPort: 20042,
		Health:                ociapps.HealthCheck{Kind: ociapps.HealthHTTP, Path: "/health", IntervalSeconds: 10, TimeoutSeconds: 2, Retries: 3},
		EnvironmentReferences: []EnvironmentReference{{ValueID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Environment: "TOKEN", Generation: 3}},
		Volumes:               []ociapps.VolumeMount{{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", ContainerPath: "/data"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ociresources.NetworkName != "stackfort-private" {
		t.Fatal("unexpected network contract")
	}
	return spec
}
