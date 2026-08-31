// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"golang.org/x/sys/unix"
)

type linuxDownloader struct{ executable string }

func platformDownloadExecutable() string { return "/proc/self/exe" }

func newPlatformDownloader(executable string) platformDownloader {
	return &linuxDownloader{executable: executable}
}

func (downloader *linuxDownloader) Open(ctx context.Context, request agentprotocol.FileDownloadRequest) (Download, error) {
	if downloader == nil || downloader.executable == "" {
		return Download{}, ErrUnavailable
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > agentprotocol.MaxFileDownloadRequestBytes {
		return Download{}, ErrInvalid
	}
	command := exec.CommandContext(ctx, downloader.executable, DownloadHelperArgument) // #nosec G204 -- executable is the running agent or qualification-owned binary.
	command.Stdin = bytes.NewReader(encoded)
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: request.Identity.UID, Gid: request.Identity.GID, NoSetGroups: true,
	}, Pdeathsig: syscall.SIGKILL}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Download{}, ErrUnavailable
	}
	if err := command.Start(); err != nil {
		return Download{}, ErrUnavailable
	}
	reader := bufio.NewReaderSize(stdout, agentprotocol.MaximumDownloadFrameBytes+1)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > agentprotocol.MaximumDownloadFrameBytes || len(line) < 2 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return Download{}, ErrUnavailable
	}
	frame, err := agentprotocol.DecodeFileDownloadFrame(bytes.NewReader(line))
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return Download{}, ErrUnavailable
	}
	if frame.Error != nil {
		if err := command.Wait(); err != nil {
			return Download{}, ErrUnavailable
		}
		return Download{TotalSize: frame.TotalSize}, downloadErrorFromCode(frame.Error.Code)
	}
	body := &downloadProcessBody{reader: reader, command: command, remaining: frame.Length}
	return Download{
		TotalSize: frame.TotalSize, Offset: frame.Offset, Length: frame.Length,
		ModifiedAt: frame.ModifiedAt.Unix(), Partial: frame.Partial, Body: body,
	}, nil
}

type downloadProcessBody struct {
	readMu    sync.Mutex
	stateMu   sync.Mutex
	reader    io.Reader
	command   *exec.Cmd
	remaining uint64
	closed    bool
}

func (body *downloadProcessBody) Read(buffer []byte) (int, error) {
	body.readMu.Lock()
	defer body.readMu.Unlock()
	body.stateMu.Lock()
	if body.closed || body.remaining == 0 {
		body.stateMu.Unlock()
		return 0, io.EOF
	}
	if uint64(len(buffer)) > body.remaining {
		buffer = buffer[:body.remaining]
	}
	body.stateMu.Unlock()
	count, err := body.reader.Read(buffer)
	body.stateMu.Lock()
	// #nosec G115 -- io.Reader guarantees a non-negative count no greater than len(buffer), which was capped to remaining.
	body.remaining -= uint64(count)
	remaining := body.remaining
	body.stateMu.Unlock()
	if errors.Is(err, io.EOF) && remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return count, err
}

func (body *downloadProcessBody) Close() error {
	body.stateMu.Lock()
	if body.closed {
		body.stateMu.Unlock()
		return nil
	}
	body.closed = true
	remaining := body.remaining
	if remaining > 0 && body.command.Process != nil {
		_ = body.command.Process.Kill()
	}
	body.stateMu.Unlock()
	err := body.command.Wait()
	if remaining > 0 {
		return io.ErrUnexpectedEOF
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func runPlatformDownloadHelper(ctx context.Context, input io.Reader, output io.Writer) error {
	encoded, err := io.ReadAll(io.LimitReader(input, agentprotocol.MaxFileDownloadRequestBytes+1))
	if err != nil || len(encoded) > agentprotocol.MaxFileDownloadRequestBytes {
		return ErrInvalid
	}
	request, err := agentprotocol.DecodeFileDownloadRequest(bytes.NewReader(encoded))
	effectiveUID, effectiveGID := os.Geteuid(), os.Getegid()
	if err != nil || effectiveUID < 0 || effectiveGID < 0 ||
		uint32(effectiveUID) != request.Identity.UID || // #nosec G115 -- non-negative kernel identity is checked first.
		uint32(effectiveGID) != request.Identity.GID { // #nosec G115 -- non-negative kernel identity is checked first.
		return ErrInvalid
	}
	file, frame, err := openDownloadFile(ctx, request)
	if err != nil {
		code, message := downloadErrorCode(err)
		frame.Offset, frame.Length, frame.Partial, frame.ModifiedAt = 0, 0, false, time.Time{}
		frame.Error = &agentprotocol.ResponseError{Code: code, Message: message}
		return json.NewEncoder(output).Encode(frame)
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(output).Encode(frame); err != nil {
		return ErrUnavailable
	}
	if frame.Length == 0 {
		return nil
	}
	copyLength, err := checkedSignedDownloadValue(frame.Length)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(output, file, copyLength); err != nil {
		return ErrUnavailable
	}
	return nil
}

func openDownloadFile(ctx context.Context, request agentprotocol.FileDownloadRequest) (*os.File, agentprotocol.FileDownloadFrame, error) {
	if err := ctx.Err(); err != nil {
		return nil, agentprotocol.FileDownloadFrame{}, err
	}
	components := strings.Split(request.Path, "/")
	directory, device, err := openManagedDownloadRoot(request.Identity, components[:len(components)-1])
	if err != nil {
		return nil, agentprotocol.FileDownloadFrame{}, err
	}
	defer unix.Close(directory)
	descriptor, err := unix.Openat(directory, components[len(components)-1],
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
			return nil, agentprotocol.FileDownloadFrame{}, ErrNotFound
		}
		return nil, agentprotocol.FileDownloadFrame{}, ErrConflict
	}
	file := os.NewFile(uintptr(descriptor), "managed-download")
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		_ = file.Close()
		return nil, agentprotocol.FileDownloadFrame{}, ErrUnavailable
	}
	if status.Mode&unix.S_IFMT != unix.S_IFREG || uint64(status.Dev) != device ||
		status.Uid != request.Identity.UID || status.Gid != request.Identity.GID || status.Size < 0 {
		_ = file.Close()
		return nil, agentprotocol.FileDownloadFrame{}, ErrConflict
	}
	total := uint64(status.Size)
	offset, length, partial, rangeErr := resolveDownloadRange(total, request.Range)
	frame := agentprotocol.FileDownloadFrame{TotalSize: total}
	if rangeErr != nil {
		_ = file.Close()
		return nil, frame, rangeErr
	}
	if offset > 0 {
		signedOffset, err := checkedSignedDownloadValue(offset)
		if err != nil {
			_ = file.Close()
			return nil, frame, err
		}
		if _, err := file.Seek(signedOffset, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, frame, ErrUnavailable
		}
	}
	frame.Offset, frame.Length, frame.Partial = offset, length, partial
	frame.ModifiedAt = time.Unix(status.Mtim.Sec, status.Mtim.Nsec).UTC()
	return file, frame, nil
}

func openManagedDownloadRoot(identity hostingidentity.Spec, descendants []string) (int, uint64, error) {
	descriptor, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, 0, ErrUnavailable
	}
	components := append([]string{"srv", "hosting", "accounts", identity.AccountID}, descendants...)
	var device uint64
	for index, component := range components {
		next, openErr := unix.Openat(descriptor, component, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(descriptor)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) || errors.Is(openErr, unix.ENOTDIR) {
				return -1, 0, ErrNotFound
			}
			return -1, 0, ErrConflict
		}
		descriptor = next
		var status unix.Stat_t
		if err := unix.Fstat(descriptor, &status); err != nil {
			_ = unix.Close(descriptor)
			return -1, 0, ErrUnavailable
		}
		if index < 3 && (status.Uid != 0 || status.Gid != 0 || status.Mode&0o022 != 0) {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
		if index == 1 {
			device = uint64(status.Dev)
		}
		if index > 1 && uint64(status.Dev) != device {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
		if index >= 3 && (status.Uid != identity.UID || status.Gid != identity.GID) {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
	}
	return descriptor, device, nil
}
