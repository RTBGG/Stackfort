// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostcache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"golang.org/x/sys/unix"
)

func TestCountCacheLogRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "unexpected-fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		response := agentprotocol.CacheMetricsResponse{}
		finished <- countCacheLog(t.Context(), path, "example.test", &response)
	}()
	select {
	case err := <-finished:
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("FIFO error = %v, want managed-state conflict", err)
		}
	case <-time.After(2 * time.Second):
		// Unblock the old blocking implementation before reporting a regression;
		// this test never supplies FIFO data or depends on cancellation of open.
		if writer, err := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0); err == nil {
			_ = unix.Close(writer)
		}
		t.Fatal("cache log open waited for a FIFO writer")
	}
}

func TestCountCacheLogStillCountsBoundedRegularFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "access.log")
	content := "{\"host\":\"example.test\",\"cache\":\"HIT\"}\n" +
		"{\"host\":\"www.example.test\",\"cache\":\"MISS\"}\n" +
		"{\"host\":\"example.test\",\"cache\":\"BYPASS\"}\n" +
		"{\"host\":\"other.test\",\"cache\":\"HIT\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	response := agentprotocol.CacheMetricsResponse{}
	if err := countCacheLog(t.Context(), path, "example.test", &response); err != nil {
		t.Fatal(err)
	}
	if response.Hits != 1 || response.Misses != 1 || response.Bypasses != 1 {
		t.Fatalf("unexpected regular log counters: %+v", response)
	}
}
