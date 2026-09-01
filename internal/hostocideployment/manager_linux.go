// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostocideployment

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostingoci"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/google/uuid"
)

const manifestSchema = 1

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type capabilityInspector interface {
	InspectOCIRuntime(context.Context) (agentprotocol.OCIRuntimeCapabilities, error)
}

type linuxManager struct {
	mu           sync.Mutex
	commands     commandRunner
	capabilities capabilityInspector
	stateRoot    string
	stateUID     uint32
	stateGID     uint32
	probe        func(context.Context, ocideployment.Spec) error
}

type replayManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	RequestDigest string               `json:"requestDigest"`
	Result        ocideployment.Result `json:"result"`
}

func NewManager() Manager {
	manager := &linuxManager{commands: agentexec.NewRunner(), capabilities: hostcapabilities.NewInspector(),
		stateRoot: ocideployment.DeploymentStateRoot, stateUID: 0, stateGID: 0}
	manager.probe = manager.healthProbe
	return manager
}

func (manager *linuxManager) Reconcile(ctx context.Context, operationID string,
	request ocideployment.Request) (ocideployment.LifecycleResult, error) {
	if manager == nil || manager.commands == nil || manager.capabilities == nil || manager.probe == nil ||
		ocideployment.ValidateRequest(request) != nil || !canonicalOperationID(operationID) {
		return ocideployment.LifecycleResult{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	runtime, err := manager.capabilities.InspectOCIRuntime(ctx)
	if err != nil {
		return ocideployment.LifecycleResult{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-deployment-capability-inspection-failed"}}
	}
	if capability := firstUnavailableCapability(runtime); capability.Status != agentprotocol.CapabilityAvailable {
		return ocideployment.LifecycleResult{}, &CapabilityError{Capability: capability}
	}
	switch request.Action {
	case ocideployment.ActionDeploy, ocideployment.ActionRollback:
		return manager.deploy(ctx, request)
	case ocideployment.ActionSuspend:
		return manager.suspend(ctx, request)
	case ocideployment.ActionResume:
		return manager.resume(ctx, request)
	case ocideployment.ActionRemove:
		return manager.remove(ctx, request)
	default:
		return ocideployment.LifecycleResult{}, ErrInvalid
	}
}

func (manager *linuxManager) deploy(ctx context.Context, request ocideployment.Request) (ocideployment.LifecycleResult, error) {
	quadlet, err := ocideployment.RenderQuadlet(request.Spec)
	if err != nil {
		return ocideployment.LifecycleResult{}, ErrInvalid
	}
	values, _ := ocideployment.InvocationValues(request.Spec)
	for _, value := range request.Values {
		invocationValues := append(append([]string(nil), values...), value.ValueID, value.Environment,
			strconv.FormatInt(value.Generation, 10))
		secretInput := []byte(value.Value)
		result, runErr := manager.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfilePodmanSecretCreate,
			Values: invocationValues, Input: secretInput})
		clear(secretInput)
		if runErr != nil || result.ExitCode != 0 {
			return ocideployment.LifecycleResult{}, ErrMutation
		}
	}
	previous, existed, err := readQuadlet(quadlet.Path, request.Spec.Identity.UID)
	if err != nil {
		return ocideployment.LifecycleResult{}, err
	}
	changed := !existed || !bytes.Equal(previous, quadlet.Content)
	wasActive := manager.isActive(ctx, values)
	if changed {
		if err := writeQuadlet(quadlet.Path, request.Spec.Identity.UID, quadlet.Content); err != nil {
			return ocideployment.LifecycleResult{}, err
		}
	}
	if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserDaemonReload, values); err == nil {
		err = manager.runUnit(ctx, agentexec.ProfileSystemdUserRestart, values)
	}
	if err == nil {
		err = manager.probe(ctx, request.Spec)
	}
	if err != nil {
		_ = manager.restore(ctx, request.Spec, values, previous, existed, wasActive)
		if errors.Is(err, ErrUnhealthy) {
			return ocideployment.LifecycleResult{}, ErrUnhealthy
		}
		return ocideployment.LifecycleResult{}, ErrMutation
	}
	deployment, err := ocideployment.ResultFor(request.Spec, changed || !wasActive)
	if err != nil {
		return ocideployment.LifecycleResult{}, ErrInvalid
	}
	deployment.Reused = !deployment.Changed
	result := ocideployment.LifecycleResult{Action: request.Action, State: ocideployment.StateActive,
		Deployment: &deployment, Healthy: true, Changed: deployment.Changed, Reused: deployment.Reused}
	manifestDeployment := deployment
	manifestDeployment.Changed, manifestDeployment.Reused = false, false
	if err := manager.writeManifest(request.Spec, replayManifest{SchemaVersion: manifestSchema,
		RequestDigest: deployment.DeploymentDigest, Result: manifestDeployment}); err != nil {
		return ocideployment.LifecycleResult{}, err
	}
	return result, nil
}

func (manager *linuxManager) suspend(ctx context.Context, request ocideployment.Request) (ocideployment.LifecycleResult, error) {
	values, _ := ocideployment.InvocationValues(request.Spec)
	active := manager.isActive(ctx, values)
	if active {
		if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserStop, values); err != nil {
			return ocideployment.LifecycleResult{}, err
		}
	}
	if manager.isActive(ctx, values) {
		return ocideployment.LifecycleResult{}, ErrMutation
	}
	return ocideployment.LifecycleResult{Action: request.Action, State: ocideployment.StateSuspended,
		Changed: active, Reused: !active}, nil
}

func (manager *linuxManager) resume(ctx context.Context, request ocideployment.Request) (ocideployment.LifecycleResult, error) {
	quadlet, err := ocideployment.RenderQuadlet(request.Spec)
	if err != nil {
		return ocideployment.LifecycleResult{}, ErrInvalid
	}
	if content, exists, readErr := readQuadlet(quadlet.Path, request.Spec.Identity.UID); readErr != nil ||
		!exists || !bytes.Equal(content, quadlet.Content) {
		return ocideployment.LifecycleResult{}, ErrConflict
	}
	values, _ := ocideployment.InvocationValues(request.Spec)
	wasActive := manager.isActive(ctx, values)
	if !wasActive {
		if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserDaemonReload, values); err != nil {
			return ocideployment.LifecycleResult{}, err
		}
		if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserStart, values); err != nil {
			return ocideployment.LifecycleResult{}, err
		}
	}
	if err := manager.probe(ctx, request.Spec); err != nil {
		_ = manager.runUnit(ctx, agentexec.ProfileSystemdUserStop, values)
		return ocideployment.LifecycleResult{}, ErrUnhealthy
	}
	deployment, _ := ocideployment.ResultFor(request.Spec, !wasActive)
	deployment.Reused = wasActive
	return ocideployment.LifecycleResult{Action: request.Action, State: ocideployment.StateActive,
		Deployment: &deployment, Healthy: true, Changed: !wasActive, Reused: wasActive}, nil
}

func (manager *linuxManager) remove(ctx context.Context, request ocideployment.Request) (ocideployment.LifecycleResult, error) {
	quadlet, err := ocideployment.RenderQuadlet(request.Spec)
	if err != nil {
		return ocideployment.LifecycleResult{}, ErrInvalid
	}
	values, _ := ocideployment.InvocationValues(request.Spec)
	changed := manager.isActive(ctx, values)
	if changed {
		if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserStop, values); err != nil {
			return ocideployment.LifecycleResult{}, err
		}
	}
	if _, exists, err := readQuadlet(quadlet.Path, request.Spec.Identity.UID); err != nil {
		return ocideployment.LifecycleResult{}, err
	} else if exists {
		if err := os.Remove(quadlet.Path); err != nil {
			return ocideployment.LifecycleResult{}, ErrMutation
		}
		changed = true
	}
	for _, reference := range request.Spec.EnvironmentReferences {
		invocationValues := append(append([]string(nil), values...), reference.ValueID, reference.Environment,
			strconv.FormatInt(reference.Generation, 10))
		result, runErr := manager.commands.Run(ctx, agentexec.Invocation{
			Profile: agentexec.ProfilePodmanSecretRemove, Values: invocationValues,
		})
		if runErr != nil || result.ExitCode != 0 {
			return ocideployment.LifecycleResult{}, ErrMutation
		}
	}
	if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserDaemonReload, values); err != nil {
		return ocideployment.LifecycleResult{}, err
	}
	return ocideployment.LifecycleResult{Action: request.Action, State: ocideployment.StateRemoved,
		Changed: changed, Reused: !changed}, nil
}

func (manager *linuxManager) restore(ctx context.Context, spec ocideployment.Spec, values []string,
	previous []byte, existed, active bool) error {
	quadlet, _ := ocideployment.RenderQuadlet(spec)
	_ = manager.runUnit(ctx, agentexec.ProfileSystemdUserStop, values)
	if existed {
		if err := writeQuadlet(quadlet.Path, spec.Identity.UID, previous); err != nil {
			return err
		}
	} else if err := os.Remove(quadlet.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrMutation
	}
	if err := manager.runUnit(ctx, agentexec.ProfileSystemdUserDaemonReload, values); err != nil {
		return err
	}
	if active && existed {
		return manager.runUnit(ctx, agentexec.ProfileSystemdUserStart, values)
	}
	return nil
}

func (manager *linuxManager) runUnit(ctx context.Context, profile agentexec.ProfileID, values []string) error {
	result, err := manager.commands.Run(ctx, agentexec.Invocation{Profile: profile, Values: values})
	if err != nil || result.ExitCode != 0 {
		return ErrMutation
	}
	return nil
}

func (manager *linuxManager) isActive(ctx context.Context, values []string) bool {
	result, err := manager.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdUserIsActive, Values: values})
	return err == nil && result.ExitCode == 0
}

func (manager *linuxManager) healthProbe(ctx context.Context, spec ocideployment.Spec) error {
	address := net.JoinHostPort("127.0.0.1", strconv.FormatInt(spec.LoopbackPort, 10))
	for attempt := int64(0); attempt < spec.Health.Retries; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.Health.TimeoutSeconds)*time.Second)
		var err error
		if spec.Health.Kind == "tcp" {
			var connection net.Conn
			connection, err = (&net.Dialer{}).DialContext(probeCtx, "tcp", address)
			if err == nil {
				err = connection.Close()
			}
		} else {
			transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{}).DialContext,
				DisableKeepAlives: true, MaxIdleConns: 1}
			client := &http.Client{Transport: transport}
			var request *http.Request
			request, err = http.NewRequestWithContext(probeCtx, http.MethodGet,
				"http://"+address+spec.Health.Path, nil)
			if err == nil {
				request.Host = "localhost"
				var response *http.Response
				response, err = client.Do(request)
				if err == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
					_ = response.Body.Close()
					if response.StatusCode < 200 || response.StatusCode >= 400 {
						err = ErrUnhealthy
					}
				}
			}
			transport.CloseIdleConnections()
		}
		cancel()
		if err == nil {
			return nil
		}
		if attempt+1 < spec.Health.Retries {
			select {
			case <-ctx.Done():
				return ErrUnhealthy
			case <-time.After(time.Second):
			}
		}
	}
	return ErrUnhealthy
}

func readQuadlet(path string, accountUID uint32) ([]byte, bool, error) {
	if err := validateQuadletParent(filepath.Dir(path), accountUID); err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 || info.Size() > 1<<20 {
		return nil, false, ErrConflict
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != 0 || status.Gid != 0 {
		return nil, false, ErrConflict
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, ErrUnavailable
	}
	return content, true, nil
}

func writeQuadlet(path string, accountUID uint32, content []byte) error {
	parent := filepath.Dir(path)
	if err := validateQuadletParent(parent, accountUID); err != nil || len(content) == 0 || len(content) > 1<<20 {
		return ErrConflict
	}
	temporary, err := os.CreateTemp(parent, ".stackfort-quadlet-*")
	if err != nil {
		return ErrMutation
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if temporary.Chmod(0o644) != nil || temporary.Chown(0, 0) != nil {
		_ = temporary.Close()
		return ErrMutation
	}
	if _, err := temporary.Write(content); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return ErrMutation
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return ErrMutation
	}
	directory, err := os.Open(parent)
	if err != nil {
		return ErrMutation
	}
	err = directory.Sync()
	_ = directory.Close()
	if err != nil {
		return ErrMutation
	}
	return nil
}

func validateQuadletParent(parent string, accountUID uint32) error {
	expected := filepath.Join(hostingoci.QuadletUsersRoot, strconv.FormatUint(uint64(accountUID), 10))
	if filepath.Clean(parent) != expected {
		return ErrInvalid
	}
	info, err := os.Lstat(parent)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o755 {
		return ErrConflict
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != 0 || status.Gid != 0 {
		return ErrConflict
	}
	return nil
}

func (manager *linuxManager) writeManifest(spec ocideployment.Spec, manifest replayManifest) error {
	directory := filepath.Join(manager.stateRoot, spec.Identity.AccountID, spec.ApplicationID)
	for _, path := range []string{manager.stateRoot, filepath.Dir(directory), directory} {
		if err := ensureStateDirectory(path, manager.stateUID, manager.stateGID); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > 64<<10 {
		return ErrInvalid
	}
	path := filepath.Join(directory, "r"+strconv.FormatInt(spec.Revision, 10)+"-"+manifest.RequestDigest[7:]+".json")
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, encoded) {
			return ErrConflict
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrUnavailable
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrMutation
	}
	if file.Chown(int(manager.stateUID), int(manager.stateGID)) != nil {
		_ = file.Close()
		return ErrMutation
	}
	if _, err := file.Write(encoded); err != nil || file.Sync() != nil || file.Close() != nil {
		return ErrMutation
	}
	return nil
}

func ensureStateDirectory(path string, uid, gid uint32) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return ErrMutation
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return ErrConflict
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != uid || status.Gid != gid {
		return ErrConflict
	}
	return nil
}

func (manager *linuxManager) ReadLogs(ctx context.Context, spec ocideployment.LogSpec) (ocideployment.LogResult, error) {
	if manager == nil || manager.commands == nil || ocideployment.ValidateLogSpec(spec) != nil {
		return ocideployment.LogResult{}, ErrInvalid
	}
	values := []string{spec.Identity.AccountID, spec.Identity.Username,
		strconv.FormatUint(uint64(spec.Identity.UID), 10), strconv.FormatUint(uint64(spec.Identity.GID), 10),
		spec.Identity.HomeDirectory, spec.ApplicationID, strconv.Itoa(spec.Tail)}
	result, err := manager.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfileJournalUserUnit, Values: values})
	if err != nil || result.ExitCode != 0 {
		return ocideployment.LogResult{}, ErrUnavailable
	}
	return parseJournal(result.Stdout, spec.Tail)
}

func parseJournal(content string, limit int) (ocideployment.LogResult, error) {
	scanner := bufio.NewScanner(io.LimitReader(strings.NewReader(content), ocideployment.MaximumLogBytes+1))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	entries := make([]ocideployment.LogEntry, 0, limit)
	for scanner.Scan() {
		var record struct {
			Timestamp string `json:"__REALTIME_TIMESTAMP"`
			Message   string `json:"MESSAGE"`
		}
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		if decoder.Decode(&record) != nil || record.Timestamp == "" {
			continue
		}
		micros, err := strconv.ParseInt(record.Timestamp, 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, ocideployment.LogEntry{Timestamp: time.UnixMicro(micros).UTC().Format(time.RFC3339Nano),
			Message: sanitizeLogMessage(record.Message)})
		if len(entries) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return ocideployment.LogResult{}, ErrUnavailable
	}
	return ocideployment.LogResult{Entries: entries, Truncated: len(entries) == limit}, nil
}

func sanitizeLogMessage(value string) string {
	if len(value) > 8<<10 {
		value = value[:8<<10]
		for !utf8.ValidString(value) && len(value) > 0 {
			value = value[:len(value)-1]
		}
	}
	return strings.Map(func(character rune) rune {
		if character == '\t' || character >= ' ' && !unicode.IsControl(character) {
			return character
		}
		return unicode.ReplacementChar
	}, value)
}

func canonicalOperationID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}
