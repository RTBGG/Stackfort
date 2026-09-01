// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostocideployment"
	"github.com/RTBGG/stackfort/internal/hostociresources"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

func TestDisposableHostOCIDeploymentLifecycle(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	identity := disposableIdentity(t, availableManagedID(t, 249_920))
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = os.RemoveAll(identity.HomeDirectory)
		if _, err := hostidentity.NewReconciler().Delete(cleanupContext, identity); err != nil {
			t.Errorf("cleanup OCI deployment identity: %v", err)
		}
	})
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID}); err != nil {
		t.Fatal(err)
	}
	if _, err := hostidentity.NewReconciler().ReconcileRuntime(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	applicationID := mustIntegrationUUIDv7(t)
	resourceSpec, err := ociresources.Normalize(ociresources.Spec{Identity: identity,
		ApplicationID: applicationID, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	resourceManager := hostociresources.NewManager()
	if _, err := resourceManager.Reconcile(t.Context(), mustIntegrationUUIDv7(t), resourceSpec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runRootlessPodman(identity, "network", "rm", ociresources.NetworkName)
		removeSafeApplicationState(t, ociresources.ArtifactRoot, identity.AccountID, applicationID)
		removeSafeApplicationState(t, ocideployment.DeploymentStateRoot, identity.AccountID, applicationID)
	})
	imageOutput, err := runRootlessPodman(identity, "pull", "--quiet", "docker.io/nginxinc/nginx-unprivileged:alpine")
	if err != nil {
		t.Fatalf("pull qualification image: %v: %s", err, imageOutput)
	}
	imageOutput, err = runRootlessPodman(identity, "image", "inspect", "--format", "{{.Id}}",
		"docker.io/nginxinc/nginx-unprivileged:alpine")
	imageDigest := strings.TrimSpace(string(imageOutput))
	if len(imageDigest) == 64 {
		imageDigest = "sha256:" + imageDigest
	}
	if err != nil || !strings.HasPrefix(imageDigest, "sha256:") || len(imageDigest) != 71 {
		t.Fatalf("inspect qualification image: %v: %q", err, imageDigest)
	}
	port := availableLoopbackPort(t)
	resourceDigest, _ := ociresources.SemanticDigest(resourceSpec)
	spec, err := ocideployment.Normalize(ocideployment.Spec{Identity: identity, ApplicationID: applicationID,
		Revision: 1, ImageDigest: imageDigest, ResourceDigest: resourceDigest,
		InternalPort: 8080, LoopbackPort: int64(port), Health: ociapps.HealthCheck{
			Kind: ociapps.HealthHTTP, Path: "/", IntervalSeconds: 10, TimeoutSeconds: 3, Retries: 10}})
	if err != nil {
		t.Fatal(err)
	}
	secretID := mustIntegrationUUIDv7(t)
	spec.EnvironmentReferences = []ocideployment.EnvironmentReference{{
		ValueID: secretID, Environment: "QUALIFICATION_TOKEN", Generation: 1,
	}}
	manager := hostocideployment.NewManager()
	removedCleanly := false
	deployRequest := ocideployment.Request{Action: ocideployment.ActionDeploy, Spec: spec,
		Values: []ocideployment.EnvironmentValue{{
			ValueID: secretID, Environment: "QUALIFICATION_TOKEN", Generation: 1, Value: "transient-qualification-value",
		}}}
	deployed, err := manager.Reconcile(t.Context(), mustIntegrationUUIDv7(t), deployRequest)
	if err != nil || deployed.State != ocideployment.StateActive || !deployed.Healthy || deployed.Deployment == nil {
		t.Fatalf("deploy = %#v / %v", deployed, err)
	}
	if _, err := runRootlessPodman(identity, "secret", "inspect", ocideployment.SecretName(spec.EnvironmentReferences[0])); err != nil {
		t.Fatalf("deployment secret is missing: %v", err)
	}
	t.Cleanup(func() {
		if removedCleanly {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := manager.Reconcile(cleanupContext, mustIntegrationUUIDv7(t),
			ocideployment.Request{Action: ocideployment.ActionRemove, Spec: spec}); err != nil {
			t.Errorf("cleanup OCI deployment: %v", err)
			quadlet, _ := ocideployment.RenderQuadlet(spec)
			_ = os.Remove(quadlet.Path)
		}
	})
	quadlet, _ := ocideployment.RenderQuadlet(spec)
	content, err := os.ReadFile(quadlet.Path)
	if err != nil || !strings.Contains(string(content), "PublishPort=127.0.0.1:"+strconv.Itoa(port)+":8080/tcp") ||
		strings.Contains(string(content), "PodmanArgs") || strings.Contains(string(content), "0.0.0.0") {
		t.Fatalf("fixed Quadlet = %v\n%s", err, content)
	}
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/") // #nosec G107 -- derived disposable loopback endpoint.
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("application status = %d", response.StatusCode)
	}
	replayed, err := manager.Reconcile(t.Context(), mustIntegrationUUIDv7(t), deployRequest)
	if err != nil || replayed.Changed || !replayed.Reused {
		t.Fatalf("deploy replay = %#v / %v", replayed, err)
	}
	logs, err := manager.ReadLogs(t.Context(), ocideployment.LogSpec{Identity: identity,
		ApplicationID: applicationID, Tail: 50})
	if err != nil || len(logs.Entries) == 0 {
		t.Fatalf("bounded application logs = %#v / %v", logs, err)
	}
	suspended, err := manager.Reconcile(t.Context(), mustIntegrationUUIDv7(t),
		ocideployment.Request{Action: ocideployment.ActionSuspend, Spec: spec})
	if err != nil || suspended.State != ocideployment.StateSuspended {
		t.Fatalf("suspend = %#v / %v", suspended, err)
	}
	if connection, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond); dialErr == nil {
		_ = connection.Close()
		t.Fatal("suspended application still accepts ingress")
	}
	resumed, err := manager.Reconcile(t.Context(), mustIntegrationUUIDv7(t),
		ocideployment.Request{Action: ocideployment.ActionResume, Spec: spec})
	if err != nil || resumed.State != ocideployment.StateActive || !resumed.Healthy {
		t.Fatalf("resume = %#v / %v", resumed, err)
	}
	rolledBack, err := manager.Reconcile(t.Context(), mustIntegrationUUIDv7(t),
		ocideployment.Request{Action: ocideployment.ActionRollback, Spec: spec, Values: deployRequest.Values})
	if err != nil || rolledBack.State != ocideployment.StateActive || !rolledBack.Healthy {
		t.Fatalf("rollback/reconverge = %#v / %v", rolledBack, err)
	}
	removal, err := manager.Reconcile(t.Context(), mustIntegrationUUIDv7(t),
		ocideployment.Request{Action: ocideployment.ActionRemove, Spec: spec})
	if err != nil || removal.State != ocideployment.StateRemoved {
		t.Fatalf("remove = %#v / %v", removal, err)
	}
	removedCleanly = true
	if _, err := os.Lstat(quadlet.Path); !os.IsNotExist(err) {
		t.Fatalf("Quadlet remains after remove: %v", err)
	}
	if _, err := runRootlessPodman(identity, "secret", "inspect", ocideployment.SecretName(spec.EnvironmentReferences[0])); err == nil {
		t.Fatal("deployment secret remains after remove")
	}
	t.Log("STACKFORT_QUALIFICATION oci-deployment-lifecycle=passed")
}

func availableLoopbackPort(t *testing.T) int {
	t.Helper()
	for port := ocideployment.MinimumLoopbackPort; port <= ocideployment.MaximumLoopbackPort; port++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port
	}
	t.Fatal("no Stackfort loopback port is available")
	return 0
}

func removeSafeApplicationState(t *testing.T, root, accountID, applicationID string) {
	t.Helper()
	applicationRoot := filepath.Join(root, accountID, applicationID)
	if filepath.Dir(filepath.Dir(applicationRoot)) != root {
		t.Errorf("refusing unsafe OCI state cleanup path %q", applicationRoot)
		return
	}
	_ = os.RemoveAll(applicationRoot)
	_ = os.Remove(filepath.Dir(applicationRoot))
}
