// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostocideployment

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingoci"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"golang.org/x/sys/unix"
)

func TestHTTPHealthProbeRejectsRedirectWithoutSecondaryRequest(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var secondaryRequests atomic.Int32
			secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				secondaryRequests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer secondary.Close()
			for _, location := range []string{secondary.URL + "/private", "/secondary", "http://169.254.169.254/latest/meta-data/"} {
				var initialRequests atomic.Int32
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					initialRequests.Add(1)
					if r.URL.Path != "/health" || r.Host != "localhost" {
						secondaryRequests.Add(1)
						w.WriteHeader(http.StatusOK)
						return
					}
					w.Header().Set("Location", location)
					w.WriteHeader(status)
				}))
				spec := healthProbeTestSpec(t, origin)
				err := (&linuxManager{}).healthProbe(t.Context(), spec)
				origin.Close()
				if !errors.Is(err, ErrUnhealthy) || initialRequests.Load() != 1 || secondaryRequests.Load() != 0 {
					t.Fatalf("redirect status %d: error=%v origin requests=%d secondary requests=%d",
						status, err, initialRequests.Load(), secondaryRequests.Load())
				}
			}
		})
	}
}

func TestHTTPHealthProbeRequiresDirectSuccess(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusMultipleChoices,
		http.StatusNotModified, http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer origin.Close()
			err := (&linuxManager{}).healthProbe(t.Context(), healthProbeTestSpec(t, origin))
			if status >= 200 && status < 300 {
				if err != nil {
					t.Fatalf("direct successful response rejected: %v", err)
				}
			} else if !errors.Is(err, ErrUnhealthy) {
				t.Fatalf("non-success response accepted: %v", err)
			}
		})
	}
}

func healthProbeTestSpec(t *testing.T, server *httptest.Server) ocideployment.Spec {
	t.Helper()
	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseInt(portText, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return ocideployment.Spec{LoopbackPort: port,
		Health: ociapps.HealthCheck{Kind: ociapps.HealthHTTP, Path: "/health", TimeoutSeconds: 1, Retries: 1}}
}

// The complete deploy path intentionally retains its fixed privileged paths.
// Execute its filesystem regression only in a child with an isolated /etc,
// leaving the host's Quadlets and service manager untouched.
func TestDisposableDeploymentActivationFailurePreservesOldDeployment(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("requires an explicitly opted-in disposable Linux host as root")
	}
	if os.Getenv("STACKFORT_DEPLOYMENT_FAILURE_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		// #nosec G204 -- the executable is this test binary and arguments are fixed; no product command is executed.
		command := exec.CommandContext(t.Context(), self, "-test.v", "-test.run=^TestDisposableDeploymentActivationFailurePreservesOldDeployment$")
		command.Env = append(os.Environ(), "STACKFORT_DEPLOYMENT_FAILURE_CHILD=1")
		command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated deploy test: %v\n%s", err, output)
		}
		return
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	privateEtc := t.TempDir()
	if err := unix.Mount(privateEtc, "/etc", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	spec := deploymentManagerTestSpec(t)
	quadlet, err := ocideployment.RenderQuadlet(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(quadlet.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(quadlet.Path) != filepath.Join(hostingoci.QuadletUsersRoot, "200123") {
		t.Fatal("unexpected test fixture path")
	}
	for _, failure := range []agentexec.ProfileID{agentexec.ProfileSystemdUserDaemonReload, agentexec.ProfileSystemdUserRestart} {
		for _, commandError := range []bool{false, true} {
			t.Run(string(failure)+"/command-error="+strconv.FormatBool(commandError), func(t *testing.T) {
				previous := []byte("# previously active healthy deployment\n")
				if err := os.WriteFile(quadlet.Path, previous, 0o644); err != nil {
					t.Fatal(err)
				}
				runner := &deploymentFailureRunner{failure: failure, commandError: commandError}
				probeCalls := 0
				stateRoot := filepath.Join(t.TempDir(), "manifests")
				manager := &linuxManager{commands: runner, stateRoot: stateRoot,
					probe: func(context.Context, ocideployment.Spec) error {
						probeCalls++
						return nil // The old service remains healthy: it must not hide activation failure.
					}}
				result, err := manager.deploy(t.Context(), ocideployment.Request{Action: ocideployment.ActionDeploy, Spec: spec})
				if !errors.Is(err, ErrMutation) || !reflect.DeepEqual(result, ocideployment.LifecycleResult{}) || probeCalls != 0 {
					t.Fatalf("failed activation claimed success: result=%#v error=%v probes=%d", result, err, probeCalls)
				}
				content, err := os.ReadFile(quadlet.Path)
				if err != nil || string(content) != string(previous) {
					t.Fatalf("prior Quadlet not restored: %q, %v", content, err)
				}
				if _, err := os.Lstat(stateRoot); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed deployment persisted a success manifest: %v", err)
				}
				want := []agentexec.ProfileID{agentexec.ProfileSystemdUserIsActive, agentexec.ProfileSystemdUserDaemonReload}
				if failure == agentexec.ProfileSystemdUserRestart {
					want = append(want, agentexec.ProfileSystemdUserRestart)
				}
				want = append(want, agentexec.ProfileSystemdUserStop, agentexec.ProfileSystemdUserDaemonReload, agentexec.ProfileSystemdUserStart)
				if !reflect.DeepEqual(runner.profiles, want) {
					t.Fatalf("activation/restoration sequence = %v, want %v", runner.profiles, want)
				}
			})
		}
	}
}

type deploymentFailureRunner struct {
	failure      agentexec.ProfileID
	commandError bool
	failed       bool
	profiles     []agentexec.ProfileID
}

func (runner *deploymentFailureRunner) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	runner.profiles = append(runner.profiles, invocation.Profile)
	if invocation.Profile == runner.failure && !runner.failed {
		runner.failed = true
		if runner.commandError {
			return agentexec.Result{}, errors.New("injected activation failure")
		}
		return agentexec.Result{ExitCode: 1}, nil
	}
	return agentexec.Result{}, nil
}

func deploymentManagerTestSpec(t *testing.T) ocideployment.Spec {
	t.Helper()
	accountID := "019d2eaa-52d0-7f52-8ac7-0aeb932455d9"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	spec, err := ocideployment.Normalize(ocideployment.Spec{
		Identity:      hostingidentity.Spec{AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home},
		ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455da", Revision: 1,
		ImageDigest: "sha256:" + strings.Repeat("a", 64), ResourceDigest: "sha256:" + strings.Repeat("b", 64),
		InternalPort: 8080, LoopbackPort: 20042,
		Health: ociapps.HealthCheck{Kind: ociapps.HealthHTTP, Path: "/health", IntervalSeconds: 10, TimeoutSeconds: 1, Retries: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
