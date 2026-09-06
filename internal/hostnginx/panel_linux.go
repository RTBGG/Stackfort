// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/panelconfig"
	"golang.org/x/sys/unix"
)

type panelManager struct {
	root   string
	spec   nginxbaseline.Spec
	runner commandRunner
	roots  *x509.CertPool
	health func(context.Context, panelconfig.Config, []byte) error
	issue  panelIssueFunc
}

type panelJournal struct {
	SchemaVersion int    `json:"schemaVersion"`
	Previous      string `json:"previous"`
	Next          string `json:"next"`
	Intermediate  string `json:"intermediate,omitempty"`
	Token         string `json:"token,omitempty"`
}

func managePanel(ctx context.Context, request PanelRequest) (PanelStatus, error) {
	if os.Geteuid() != 0 {
		return PanelStatus{}, errors.New("panel configuration requires root")
	}
	platform := hostcapabilities.NewInspector().InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return PanelStatus{}, ErrConflict
	}
	spec, err := nginxbaseline.ForDistribution(platform.DistributionID)
	if err != nil {
		return PanelStatus{}, err
	}
	manager := &panelManager{root: "/", spec: spec, runner: agentexec.NewRunner(), health: panelHostnameHealth}
	return manager.manage(ctx, request)
}

func (manager *panelManager) path(path string) string {
	return filepath.Join(manager.root, strings.TrimPrefix(path, "/"))
}

// Walk from a trusted root descriptor without following ANY symlinks. This
// also bounds reads and rejects hard links, special files, writable parents,
// non-root ownership, and overly broad key permissions before reading bytes.
func (manager *panelManager) read(path string, private bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrConflict
	}
	descriptor, err := unix.Open(manager.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(descriptor) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		last := index == len(parts)-1
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if !last {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(descriptor, part, flags, 0)
		if openErr != nil {
			return nil, openErr
		}
		_ = unix.Close(descriptor)
		descriptor = next
		var stat unix.Stat_t
		if unix.Fstat(descriptor, &stat) != nil || stat.Uid != 0 || stat.Mode&0o022 != 0 {
			return nil, ErrConflict
		}
		if last {
			if stat.Gid != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size > panelconfig.MaximumFile ||
				(private && stat.Mode&0o077 != 0) {
				return nil, ErrConflict
			}
			file := os.NewFile(uintptr(descriptor), path)
			descriptor = -1
			defer file.Close()
			content, readErr := io.ReadAll(io.LimitReader(file, panelconfig.MaximumFile+1))
			if readErr != nil || len(content) > panelconfig.MaximumFile {
				return nil, ErrConflict
			}
			return content, nil
		}
	}
	return nil, ErrConflict
}

func (manager *panelManager) current() (panelconfig.Config, []byte, error) {
	content, err := manager.read(panelconfig.ConfigurationPath, false)
	if errors.Is(err, os.ErrNotExist) {
		return panelconfig.Config{}, nil, nil
	}
	if err != nil {
		return panelconfig.Config{}, nil, err
	}
	config, err := panelconfig.Parse(manager.spec, content)
	return config, content, err
}

func (manager *panelManager) manage(ctx context.Context, request PanelRequest) (PanelStatus, error) {
	if request.Action != "configure" && request.Action != "disable" && request.Action != "status" && request.Action != "recover" && request.Action != "issue" && request.Action != "renew" {
		return PanelStatus{}, ErrConflict
	}
	if request.Action == "configure" && panelconfig.Hostname(request.Hostname) != nil {
		return PanelStatus{}, panelconfig.ErrInvalid
	}
	// Verify installed, root-owned anchors before the shared site-activation lock.
	for _, path := range []string{nginxbaseline.PanelConfigurationPath, nginxbaseline.MainConfiguration, nginxbaseline.MarkerPath} {
		if _, err := manager.read(path, false); err != nil {
			return PanelStatus{}, err
		}
	}
	store := &linuxActivationStore{root: manager.root}
	workspaceValue, err := store.Begin()
	if err != nil {
		return PanelStatus{}, err
	}
	defer workspaceValue.Close()
	workspace := workspaceValue.(*linuxActivationWorkspace)
	if _, exists, err := workspace.readJournal(); err != nil || exists {
		return PanelStatus{}, errors.New("finish or recover the pending tenant NGINX operation first")
	}
	if request.Action == "status" {
		return manager.status()
	}
	if err := manager.recover(ctx); err != nil {
		return PanelStatus{}, err
	}
	if request.Action == "recover" {
		return manager.status()
	}
	if request.Action == "issue" || request.Action == "renew" {
		return manager.acme(ctx, workspace, request)
	}
	_, previous, err := manager.current()
	if err != nil {
		return PanelStatus{}, err
	}
	var config panelconfig.Config
	var bundle []byte
	var next string
	if request.Action == "configure" {
		certificate, err := manager.read(request.CertificatePath, false)
		if err != nil {
			return PanelStatus{}, fmt.Errorf("read a root-owned, non-symlink certificate file: %w", err)
		}
		key, err := manager.read(request.PrivateKeyPath, true)
		if err != nil {
			return PanelStatus{}, fmt.Errorf("read a root-owned mode-0600 non-symlink private key: %w", err)
		}
		defer clear(key)
		config, bundle, err = panelconfig.Import(request.Hostname, certificate, key, time.Now(), manager.roots)
		if err != nil {
			return PanelStatus{}, err
		}
		defer clear(bundle)
		if err := manager.checkSites(workspace, config.Hostname); err != nil {
			return PanelStatus{}, err
		}
		next, err = panelconfig.Render(manager.spec, config)
		if err != nil {
			return PanelStatus{}, err
		}
		if err := manager.storeBundle(config, bundle); err != nil {
			return PanelStatus{}, err
		}
	}
	if string(previous) == next {
		if next != "" {
			if err := manager.health(ctx, config, bundle); err != nil {
				return PanelStatus{}, err
			}
		}
		return manager.status()
	}
	journal := panelJournal{SchemaVersion: 1, Previous: string(previous), Next: next}
	if err := manager.savePanelJournal(journal); err != nil {
		return PanelStatus{}, err
	}
	if err := manager.activatePanel(ctx, next); err != nil {
		return manager.rollbackPanel(err)
	}
	if next != "" {
		if err := manager.health(ctx, config, bundle); err != nil {
			return manager.rollbackPanel(err)
		}
	}
	if err := manager.remove(panelconfig.JournalPath); err != nil {
		return PanelStatus{}, err
	}
	return manager.status()
}

func (manager *panelManager) activatePanel(ctx context.Context, next string) error {
	// Validate a complete candidate before touching the active include.
	candidate := next
	if candidate == "" {
		candidate = "# Stackfort panel hostname disabled.\n"
	}
	if err := manager.write(panelconfig.CandidatePath, []byte(candidate), 0o640); err != nil {
		return err
	}
	defer func() { _ = manager.remove(panelconfig.CandidatePath) }()
	if err := manager.write(panelconfig.CandidateMainPath, []byte(panelconfig.CandidateMain(manager.spec)), 0o640); err != nil {
		return err
	}
	defer func() { _ = manager.remove(panelconfig.CandidateMainPath) }()
	if err := manager.label(ctx); err != nil {
		return err
	}
	if err := manager.run(ctx, agentexec.ProfileNGINXTestPanelCandidate); err != nil {
		return err
	}
	if err := manager.replace(next); err != nil {
		return err
	}
	if err := manager.label(ctx); err != nil {
		return err
	}
	if err := manager.run(ctx, agentexec.ProfileNGINXTestBaseline); err != nil {
		return err
	}
	if err := manager.run(ctx, agentexec.ProfileSystemdReloadNGINX); err != nil {
		return err
	}
	return nil
}

func (manager *panelManager) savePanelJournal(journal panelJournal) error {
	content, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return manager.write(panelconfig.JournalPath, content, 0o600)
}

func (manager *panelManager) rollbackPanel(cause error) (PanelStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return PanelStatus{}, errors.Join(cause, manager.recover(ctx))
}

func (manager *panelManager) run(ctx context.Context, profile agentexec.ProfileID) error {
	result, err := manager.runner.Run(ctx, agentexec.Invocation{Profile: profile})
	if err != nil || result.ExitCode != 0 {
		return fmt.Errorf("panel host operation failed (%s); inspect the local service log", profile)
	}
	return nil
}

func (manager *panelManager) label(ctx context.Context) error {
	if manager.spec.DistributionID == "rocky" {
		return manager.run(ctx, agentexec.ProfileRestoreSELinuxPanelContext)
	}
	return nil
}

func (manager *panelManager) write(path string, content []byte, mode os.FileMode) error {
	// Both parent existence/trust and any existing file must be checked before
	// atomicWrite. A missing final component is the only tolerated missing path.
	info, err := os.Lstat(manager.path(filepath.Dir(path)))
	if err != nil || !safeRootDirectory(info) {
		return ErrConflict
	}
	if _, err := manager.read(path, mode == 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(manager.path(path), content, mode, 0, 0)
}

func (manager *panelManager) remove(path string) error {
	if _, err := manager.read(path, path == panelconfig.JournalPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(manager.path(path)); err != nil {
		return err
	}
	return syncDirectory(manager.path(filepath.Dir(path)))
}

func (manager *panelManager) replace(content string) error {
	if content == "" {
		return manager.remove(panelconfig.ConfigurationPath)
	}
	if _, err := panelconfig.Parse(manager.spec, []byte(content)); err != nil {
		return err
	}
	return manager.write(panelconfig.ConfigurationPath, []byte(content), 0o640)
}

func (manager *panelManager) recover(ctx context.Context) error {
	content, err := manager.read(panelconfig.JournalPath, true)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal panelJournal
	if decodeStrictJSON(content, &journal) != nil || journal.SchemaVersion != 1 {
		return ErrConflict
	}
	for _, value := range []string{journal.Previous, journal.Next, journal.Intermediate} {
		if value != "" {
			if _, err := panelconfig.Parse(manager.spec, []byte(value)); err != nil {
				return err
			}
		}
	}
	_, current, err := manager.current()
	if err != nil || (string(current) != journal.Previous && string(current) != journal.Next && string(current) != journal.Intermediate) {
		return ErrConflict
	}
	if journal.Token != "" {
		if acmehttp01.ValidateToken(journal.Token) != nil {
			return ErrConflict
		}
		if err := manager.remove(acmehttp01.ChallengeDirectory + "/" + journal.Token); err != nil {
			return err
		}
	}
	if err := manager.replace(journal.Previous); err != nil {
		return err
	}
	if err := manager.label(ctx); err != nil {
		return err
	}
	if err := manager.run(ctx, agentexec.ProfileNGINXTestBaseline); err != nil {
		return err
	}
	if err := manager.run(ctx, agentexec.ProfileSystemdReloadNGINX); err != nil {
		return err
	}
	return manager.remove(panelconfig.JournalPath)
}

func (manager *panelManager) storeBundle(config panelconfig.Config, bundle []byte) error {
	previous, err := manager.read(config.BundlePath(), true)
	if err == nil {
		defer clear(previous)
		if !bytes.Equal(previous, bundle) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return manager.write(config.BundlePath(), bundle, 0o600)
}

func (manager *panelManager) status() (PanelStatus, error) {
	config, _, err := manager.current()
	if err != nil {
		return PanelStatus{}, err
	}
	status := PanelStatus{Enabled: config.Hostname != "" && !config.ChallengeOnly, Hostname: config.Hostname, AutoRenew: config.AutoRenew && !config.ChallengeOnly}
	if status.Enabled {
		status.URL = "https://" + config.Hostname + "/"
		bundle, err := manager.read(config.BundlePath(), true)
		if err != nil {
			return status, err
		}
		defer clear(bundle)
		digest := sha256.Sum256(bundle)
		if hex.EncodeToString(digest[:]) != config.BundleSHA256 {
			return status, ErrConflict
		}
		pair, err := tls.X509KeyPair(bundle, bundle)
		if err != nil {
			return status, panelconfig.ErrInvalid
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return status, panelconfig.ErrInvalid
		}
		status.CertificateExpiresAt = leaf.NotAfter
	}
	if _, err := manager.read(panelconfig.JournalPath, true); err == nil {
		status.RecoveryRequired = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	return status, nil
}

func (manager *panelManager) checkSites(workspace *linuxActivationWorkspace, hostname string) error {
	revision, err := workspace.currentRevision()
	if err != nil || revision == "" {
		return err
	}
	entries, err := os.ReadDir(workspace.revisionPath(revision))
	if err != nil || len(entries) > 10000 {
		return ErrConflict
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		content, err := manager.read(nginxbaseline.SiteRevisionsDirectory+"/"+revision+"/"+entry.Name(), false)
		if err != nil {
			return err
		}
		if panelconfig.Conflicts(content, hostname) {
			return errors.New("panel hostname conflicts with an active hosting domain or alias")
		}
	}
	return nil
}

func panelHostnameHealth(ctx context.Context, config panelconfig.Config, bundle []byte) error {
	pair, err := tls.X509KeyPair(bundle, bundle)
	if err != nil {
		return ErrHealthCheckFailed
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return ErrHealthCheckFailed
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: config.Hostname},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", "127.0.0.1:443")
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for attempt := 0; attempt < 10; attempt++ {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+config.Hostname+"/api/v1/health", nil)
		response, err := client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && strings.Contains(response.Header.Get("Content-Type"), "application/json") && len(body) > 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ErrHealthCheckFailed
		case <-time.After(100 * time.Millisecond):
		}
	}
	return ErrHealthCheckFailed
}
