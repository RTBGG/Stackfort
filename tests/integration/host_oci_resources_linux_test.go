// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostociresources"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/google/uuid"
)

func TestDisposableHostOCIPrivateResources(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}

	first := disposableIdentity(t, availableManagedID(t, 249_900))
	second := disposableIdentity(t, availableManagedID(t, first.UID+1))
	for _, identity := range []hostingidentity.Spec{second, first} {
		identity := identity
		t.Cleanup(func() { cleanupIdentity(t, identity) })
	}
	for _, identity := range []hostingidentity.Spec{first, second} {
		if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
			t.Fatalf("reconcile identity %s: %v", identity.Username, err)
		}
		if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
			Identity: identity, ProjectID: identity.UID,
		}); err != nil {
			t.Fatalf("reconcile filesystem %s: %v", identity.Username, err)
		}
		if _, err := hostidentity.NewReconciler().ReconcileRuntime(t.Context(), identity); err != nil {
			t.Fatalf("reconcile OCI runtime %s: %v", identity.Username, err)
		}
	}

	applicationID := mustIntegrationUUIDv7(t)
	volumeID := mustIntegrationUUIDv7(t)
	secretID := mustIntegrationUUIDv7(t)
	spec, err := ociresources.Normalize(ociresources.Spec{
		Identity: first, ApplicationID: applicationID, Revision: 1,
		EnvironmentReferences: []ociresources.EnvironmentReference{{
			SecretID: secretID, Environment: "DATABASE_URL", Generation: 1,
		}},
		Volumes: []ociapps.VolumeMount{{
			VolumeID: volumeID, ContainerPath: "/var/lib/app",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	operationID := mustIntegrationUUIDv7(t)
	artifactRoot := filepath.Join(ociresources.ArtifactRoot, first.AccountID, applicationID)
	t.Cleanup(func() {
		_, _ = runRootlessPodman(first, "network", "rm", ociresources.NetworkName)
		if filepath.Dir(filepath.Dir(artifactRoot)) != ociresources.ArtifactRoot {
			t.Errorf("refusing unsafe OCI artifact cleanup path %q", artifactRoot)
			return
		}
		if err := os.RemoveAll(artifactRoot); err != nil {
			t.Errorf("remove disposable OCI resource artifact: %v", err)
		}
		_ = os.Remove(filepath.Dir(artifactRoot))
	})

	manager := hostociresources.NewManager()
	created, err := manager.Reconcile(t.Context(), operationID, spec)
	if err != nil || !created.Changed || created.ResourceDigest == "" || created.NetworkName != ociresources.NetworkName {
		t.Fatalf("reconcile OCI private resources = %#v, %v", created, err)
	}
	replayed, err := manager.Reconcile(t.Context(), operationID, spec)
	if err != nil || replayed.Changed || !replayed.Reused || replayed.ResourceDigest != created.ResourceDigest {
		t.Fatalf("replay OCI private resources = %#v, %v", replayed, err)
	}

	output, err := runRootlessPodman(first, "network", "inspect", "--format=json", ociresources.NetworkName)
	if err != nil {
		t.Fatalf("inspect private network: %v: %s", err, output)
	}
	var networks []struct {
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		Internal   bool              `json:"internal"`
		DNSEnabled bool              `json:"dns_enabled"`
		Labels     map[string]string `json:"labels"`
		Options    map[string]string `json:"options"`
	}
	if err := json.Unmarshal(output, &networks); err != nil || len(networks) != 1 ||
		networks[0].Name != ociresources.NetworkName || networks[0].Driver != "bridge" ||
		networks[0].Internal || !networks[0].DNSEnabled || networks[0].Options["isolate"] != "strict" ||
		networks[0].Labels[ociresources.NetworkLabelManaged] != "true" ||
		networks[0].Labels[ociresources.NetworkLabelAccount] != first.AccountID {
		t.Fatalf("private network policy = %s / %v", output, err)
	}
	if otherOutput, otherErr := runRootlessPodman(second, "network", "exists", ociresources.NetworkName); otherErr == nil {
		t.Fatalf("second account can see first account network: %s", otherOutput)
	}

	volumePath, err := ociresources.VolumePath(first, volumeID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(volumePath)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("managed volume metadata = %#v / %v", info, err)
	}
	if err := runAs(second, "/usr/bin/test", "-x", volumePath); err == nil {
		t.Fatal("second account can traverse first account volume")
	}

	t.Log("STACKFORT_QUALIFICATION oci-private-resources=passed")
}

func runRootlessPodman(identity hostingidentity.Spec, arguments ...string) ([]byte, error) {
	values := []string{
		"-u", identity.Username, "--", "/usr/bin/env",
		"HOME=" + identity.HomeDirectory, "USER=" + identity.Username, "LOGNAME=" + identity.Username,
		"XDG_RUNTIME_DIR=/run/user/" + strconv.FormatUint(uint64(identity.UID), 10), "/usr/bin/podman",
	}
	values = append(values, arguments...)
	command := exec.Command("/usr/sbin/runuser", values...)
	command.Dir = "/"
	return command.CombinedOutput()
}

func mustIntegrationUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return value.String()
}
