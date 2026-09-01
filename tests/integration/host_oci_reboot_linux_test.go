// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostocideployment"
	"github.com/RTBGG/stackfort/internal/hostociresources"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

const ociRebootStatePath = "/var/lib/stackfort-agent/integration-oci-reboot.json"

type ociRebootState struct {
	Identity   hostingidentity.Spec  `json:"identity"`
	Resources  hostingresources.Spec `json:"resources"`
	Private    ociresources.Spec     `json:"privateResources"`
	Deployment ocideployment.Spec    `json:"deployment"`
}

func TestDisposableHostOCIRebootPrepare(t *testing.T) {
	requireDisposableRoot(t)
	if _, err := os.Lstat(ociRebootStatePath); err == nil {
		t.Fatal("OCI reboot fixture already exists; run the cleanup phase first")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect OCI reboot state: %v", err)
	}

	state := ociRebootState{}
	prepared := false
	t.Cleanup(func() {
		if !prepared && state.Identity.AccountID != "" {
			cleanupOCIRebootFixture(t, state)
		}
	})
	state.Identity = disposableIdentity(t, availableManagedID(t, 249_960))
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), state.Identity); err != nil {
		t.Fatal(err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: state.Identity, ProjectID: state.Identity.UID,
	}); err != nil {
		t.Fatal(err)
	}
	state.Resources = hostingresources.Spec{
		Identity:     state.Identity,
		MemoryBytes:  hostingresources.OptionalUint64{Set: true, Value: 256 << 20},
		SwapBytes:    hostingresources.OptionalUint64{Set: true, Value: 0},
		ProcessLimit: hostingresources.OptionalUint64{Set: true, Value: 128},
	}
	boundary, err := hostresources.NewReconciler().Reconcile(t.Context(), state.Resources)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostidentity.NewReconciler().ReconcileRuntime(t.Context(), state.Identity); err != nil {
		t.Fatal(err)
	}

	state.Private, err = ociresources.Normalize(ociresources.Spec{
		Identity: state.Identity, ApplicationID: mustIntegrationUUIDv7(t), Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	privateResult, err := hostociresources.NewManager().Reconcile(
		t.Context(), mustIntegrationUUIDv7(t), state.Private,
	)
	if err != nil {
		t.Fatal(err)
	}
	imageOutput, err := runRootlessPodman(state.Identity, "pull", "--quiet",
		"docker.io/nginxinc/nginx-unprivileged:alpine")
	if err != nil {
		t.Fatalf("pull reboot image: %v: %s", err, imageOutput)
	}
	imageOutput, err = runRootlessPodman(state.Identity, "image", "inspect", "--format", "{{.Id}}",
		"docker.io/nginxinc/nginx-unprivileged:alpine")
	imageDigest := strings.TrimSpace(string(imageOutput))
	if len(imageDigest) == 64 {
		imageDigest = "sha256:" + imageDigest
	}
	if err != nil || len(imageDigest) != 71 || !strings.HasPrefix(imageDigest, "sha256:") {
		t.Fatalf("inspect reboot image: %v: %q", err, imageDigest)
	}
	port := availableLoopbackPort(t)
	state.Deployment, err = ocideployment.Normalize(ocideployment.Spec{
		Identity: state.Identity, ApplicationID: state.Private.ApplicationID, Revision: 1,
		ImageDigest: imageDigest, ResourceDigest: privateResult.ResourceDigest,
		InternalPort: 8080, LoopbackPort: int64(port), Health: ociapps.HealthCheck{
			Kind: ociapps.HealthHTTP, Path: "/", IntervalSeconds: 10, TimeoutSeconds: 3, Retries: 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := hostocideployment.NewManager().Reconcile(t.Context(), mustIntegrationUUIDv7(t),
		ocideployment.Request{Action: ocideployment.ActionDeploy, Spec: state.Deployment})
	if err != nil || deployed.State != ocideployment.StateActive || !deployed.Healthy {
		t.Fatalf("deploy reboot fixture = %#v / %v", deployed, err)
	}
	assertOCIRebootBoundary(t, state, boundary.ControlGroup)
	writeOCIRebootState(t, state)
	prepared = true
	t.Log("STACKFORT_QUALIFICATION oci-reboot-prepare=passed")
}

func TestDisposableHostOCIRebootVerify(t *testing.T) {
	requireDisposableRoot(t)
	state := readOCIRebootState(t)
	t.Cleanup(func() { cleanupOCIRebootFixture(t, state) })
	wantedAccount, _ := hostingresources.AccountControlGroup(state.Identity.UID)
	assertOCIRebootBoundary(t, state, wantedAccount)
	replayed, err := hostocideployment.NewManager().Reconcile(t.Context(), mustIntegrationUUIDv7(t),
		ocideployment.Request{Action: ocideployment.ActionDeploy, Spec: state.Deployment})
	if err != nil || replayed.State != ocideployment.StateActive || !replayed.Healthy || !replayed.Reused {
		t.Fatalf("post-reboot deployment replay = %#v / %v", replayed, err)
	}
	t.Log("STACKFORT_QUALIFICATION oci-reboot-recovery=passed")
}

func TestDisposableHostOCIRebootCleanup(t *testing.T) {
	requireDisposableRoot(t)
	content, err := os.ReadFile(ociRebootStatePath)
	if errors.Is(err, os.ErrNotExist) {
		t.Log("STACKFORT_QUALIFICATION oci-reboot-cleanup=absent")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var state ociRebootState
	if json.Unmarshal(content, &state) != nil || validateOCIRebootState(state) != nil {
		t.Fatal("refusing to clean an invalid OCI reboot state")
	}
	cleanupOCIRebootFixture(t, state)
	t.Log("STACKFORT_QUALIFICATION oci-reboot-cleanup=passed")
}

func requireDisposableRoot(t *testing.T) {
	t.Helper()
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
}

func validateOCIRebootState(state ociRebootState) error {
	if hostingidentity.Validate(state.Identity) != nil || hostingresources.Validate(state.Resources) != nil ||
		ociresources.Validate(state.Private) != nil || ocideployment.Validate(state.Deployment) != nil ||
		state.Resources.Identity != state.Identity || state.Private.Identity != state.Identity ||
		state.Deployment.Identity != state.Identity || state.Private.ApplicationID != state.Deployment.ApplicationID {
		return errors.New("invalid OCI reboot state")
	}
	return nil
}

func writeOCIRebootState(t *testing.T, state ociRebootState) {
	t.Helper()
	if err := validateOCIRebootState(state); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ociRebootStatePath), 0o750); err != nil {
		t.Fatal(err)
	}
	temporary := ociRebootStatePath + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, ociRebootStatePath); err != nil {
		_ = os.Remove(temporary)
		t.Fatal(err)
	}
}

func readOCIRebootState(t *testing.T) ociRebootState {
	t.Helper()
	content, err := os.ReadFile(ociRebootStatePath)
	if err != nil {
		t.Fatal(err)
	}
	var state ociRebootState
	if err := json.Unmarshal(content, &state); err != nil || validateOCIRebootState(state) != nil {
		t.Fatalf("invalid OCI reboot state: %v", err)
	}
	return state
}

func assertOCIRebootBoundary(t *testing.T, state ociRebootState, accountControlGroup string) {
	t.Helper()
	managerControlGroup, _ := hostingresources.UserManagerControlGroup(state.Identity.UID)
	managerUnit, _ := hostingresources.UserManagerUnitName(state.Identity.UID)
	if actual := systemdProperty(t, managerUnit, "ControlGroup"); actual != managerControlGroup {
		t.Fatalf("post-reboot user manager cgroup = %q, want %q", actual, managerControlGroup)
	}
	container := ocideployment.ContainerName(state.Deployment.ApplicationID)
	pidOutput, err := runRootlessPodman(state.Identity, "inspect", "--format", "{{.State.Pid}}", container)
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidOutput)))
	if err != nil || parseErr != nil || pid < 2 {
		t.Fatalf("inspect reboot container PID: %v / %v / %q", err, parseErr, pidOutput)
	}
	cgroup, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil || !strings.Contains(string(cgroup), "0::"+managerControlGroup+"/") ||
		!strings.HasPrefix(managerControlGroup, accountControlGroup+"/") {
		t.Fatalf("post-reboot container cgroup = %q / %v", cgroup, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.FormatInt(state.Deployment.LoopbackPort, 10) + "/") // #nosec G107 -- validated disposable loopback port.
	if err != nil {
		t.Fatalf("post-reboot private ingress: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("post-reboot application status = %d", response.StatusCode)
	}
}

func cleanupOCIRebootFixture(t *testing.T, state ociRebootState) {
	t.Helper()
	if hostingidentity.Validate(state.Identity) != nil {
		t.Errorf("refusing to clean OCI reboot fixture with invalid identity")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	deploymentValid := ocideployment.Validate(state.Deployment) == nil && state.Deployment.Identity == state.Identity
	privateValid := ociresources.Validate(state.Private) == nil && state.Private.Identity == state.Identity
	if deploymentValid {
		if _, err := hostocideployment.NewManager().Reconcile(ctx, mustIntegrationUUIDv7(t),
			ocideployment.Request{Action: ocideployment.ActionRemove, Spec: state.Deployment}); err != nil {
			t.Errorf("remove reboot deployment: %v", err)
		}
	}
	_, _ = runRootlessPodman(state.Identity, "network", "rm", ociresources.NetworkName)
	if privateValid {
		removeSafeApplicationState(t, ociresources.ArtifactRoot, state.Identity.AccountID, state.Private.ApplicationID)
	}
	if deploymentValid {
		removeSafeApplicationState(t, ocideployment.DeploymentStateRoot, state.Identity.AccountID, state.Deployment.ApplicationID)
	}
	if filepath.Dir(state.Identity.HomeDirectory) != hostingidentity.ManagedAccountsRoot {
		t.Errorf("refusing unsafe reboot account cleanup path %q", state.Identity.HomeDirectory)
		return
	}
	if err := os.RemoveAll(state.Identity.HomeDirectory); err != nil {
		t.Errorf("remove reboot account home: %v", err)
	}
	if _, err := hostidentity.NewReconciler().Delete(ctx, state.Identity); err != nil {
		t.Errorf("delete reboot identity: %v", err)
	}
	unit, _ := hostingresources.AccountSliceName(state.Identity.UID)
	cleanupResourceSlice(t, unit)
	if err := os.Remove(ociRebootStatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("remove OCI reboot state: %v", err)
	}
}
