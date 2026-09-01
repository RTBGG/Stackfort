// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/agentrpc"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/hostfiles"
)

var (
	configuredSocketPath     = agentprotocol.DefaultSocketPath
	configuredControlAPIUser = "stackfort"
	configuredControlAPIUID  = ""
	configuredControlAPIGID  = ""
)

func main() {
	if runtime.GOOS == "linux" && len(os.Args) == 2 {
		switch os.Args[1] {
		case hostfiles.DownloadHelperArgument:
			if err := hostfiles.RunDownloadHelper(context.Background(), os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		case hostfiles.WriteHelperArgument:
			if err := hostfiles.RunWriteHelper(context.Background(), os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		case hostfiles.BackupHelperArgument:
			if err := hostfiles.RunBackupHelper(context.Background(), os.Stdin, os.Stdout, os.Stderr); err != nil {
				os.Exit(1)
			}
			return
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("stackfort-agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if runtime.GOOS != "linux" {
		return errors.New("stackfort-agent requires Linux Unix-domain sockets")
	}

	socketPath := configuredSocketPath
	controlUID, controlGID, err := resolveControlAPIIdentity()
	if err != nil {
		return err
	}
	if err := prepareSocketDirectory(socketPath, controlGID); err != nil {
		return err
	}
	if err := removeStaleSocket(socketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("remove agent socket", "error", err)
		}
	}()
	if err := os.Chown(socketPath, -1, controlGID); err != nil {
		if closeErr := listener.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return fmt.Errorf("set agent socket group: %w", err)
	}
	// #nosec G302 -- the stackfort service group requires access to this Unix socket.
	if err := os.Chmod(socketPath, 0o660); err != nil {
		if closeErr := listener.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}

	server := &http.Server{
		Handler:           agentrpc.NewHandler(logger),
		ErrorLog:          agentrpc.NewRedactedHTTPErrorLogger(logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      4 * time.Minute,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("stackfort-agent listening", "socket", socketPath,
			"control_uid", controlUID, "build", buildinfo.Current())
		serverErrors <- server.Serve(agentrpc.NewPeerVerifiedListener(listener, controlUID, logger))
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func resolveControlAPIIdentity() (uint32, int, error) {
	uidText := configuredControlAPIUID
	gidText := configuredControlAPIGID
	if (uidText == "") != (gidText == "") {
		return 0, 0, errors.New("control API UID and GID overrides must be configured together")
	}
	if uidText == "" {
		account, err := user.Lookup(configuredControlAPIUser)
		if err != nil {
			return 0, 0, fmt.Errorf("resolve control API service identity %q: %w", configuredControlAPIUser, err)
		}
		uidText = account.Uid
		gidText = account.Gid
	}
	uid, err := strconv.ParseUint(uidText, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parse control API UID: %w", err)
	}
	gid, err := strconv.ParseUint(gidText, 10, 31)
	if err != nil {
		return 0, 0, fmt.Errorf("parse control API GID: %w", err)
	}
	return uint32(uid), int(gid), nil
}

func prepareSocketDirectory(socketPath string, controlGID int) error {
	if !filepath.IsAbs(socketPath) {
		return errors.New("agent socket path must be absolute")
	}
	directory := filepath.Dir(filepath.Clean(socketPath))
	if directory == filepath.VolumeName(directory)+string(filepath.Separator) {
		return errors.New("agent socket must not be placed directly in a filesystem root")
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(directory, 0o750); err != nil {
			return fmt.Errorf("create agent socket directory: %w", err)
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return fmt.Errorf("inspect agent socket directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("agent socket parent must be a real directory")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return fmt.Errorf("resolve agent socket directory: %w", err)
	}
	if filepath.Clean(resolved) != directory {
		return errors.New("agent socket directory must not traverse symbolic links")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("agent socket directory must not be writable by group or others")
	}
	if err := os.Chown(directory, -1, controlGID); err != nil {
		return fmt.Errorf("set agent socket directory group: %w", err)
	}
	// #nosec G302 -- this is a directory; owner/group require execute permission to reach the socket.
	if err := os.Chmod(directory, 0o750); err != nil {
		return fmt.Errorf("protect agent socket directory: %w", err)
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("agent socket path exists and is not a Unix socket")
	}
	return os.Remove(path)
}
