// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package diag

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const maxBytes = 512 * 1024

// MemoryBuffer keeps the latest structured log output in memory.
type MemoryBuffer struct {
	mu      sync.Mutex
	enabled bool
	buf     []byte
}

func NewMemoryBuffer() *MemoryBuffer {
	return &MemoryBuffer{enabled: true, buf: make([]byte, 0, maxBytes)}
}

// NewDisabledMemoryBuffer creates a lightweight writer that drops log records
// until diagnostics are explicitly enabled. It does not allocate the snapshot
// storage while disabled.
func NewDisabledMemoryBuffer() *MemoryBuffer {
	return &MemoryBuffer{}
}

func (b *MemoryBuffer) Enabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

func (b *MemoryBuffer) SetEnabled(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if enabled && !b.enabled {
		b.buf = make([]byte, 0, maxBytes)
	}
	if !enabled {
		b.buf = nil
	}
	b.enabled = enabled
}

func (b *MemoryBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.enabled {
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	if len(b.buf) > maxBytes {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-maxBytes:]...)
	}
	return len(p), nil
}

func (b *MemoryBuffer) Snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}

// OpenInKate writes a point-in-time diagnostic snapshot and opens it in Kate.
// The file is intentionally retained so it remains available for copying.
func OpenInKate(b *MemoryBuffer) (string, error) {
	f, err := os.CreateTemp("", "just-talk-plus-diagnostics-*.log")
	if err != nil {
		return "", err
	}
	path := f.Name()
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	if _, err := fmt.Fprintf(f, "just-talk-plus diagnostics\ntime=%s\ngo=%s\nos=%s/%s\n\n", time.Now().Format(time.RFC3339Nano), runtime.Version(), runtime.GOOS, runtime.GOARCH); err != nil {
		return "", err
	}
	if _, err := f.Write(b.Snapshot()); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	closed = true
	if _, err := exec.LookPath("kate"); err != nil {
		return "", fmt.Errorf("kate not found: %w", err)
	}
	if err := exec.Command("kate", "--new", path).Start(); err != nil {
		return "", err
	}
	return path, nil
}

func WriteSnapshot(w io.Writer, b *MemoryBuffer) error {
	_, err := w.Write(b.Snapshot())
	return err
}
