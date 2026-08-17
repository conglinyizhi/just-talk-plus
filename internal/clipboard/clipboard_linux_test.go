// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

//go:build linux

package clipboard

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunCmdWithInputWithRealWaylandCopy(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("Wayland session required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if _, err := runCmdWithInput(ctx, []string{"wl-copy", "--type", "text/plain;charset=utf-8"}, "real-test"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("real wl-copy took %s, want parent command completion", elapsed)
	}
}

func TestRunCmdWithInputDoesNotWaitForForkedChildOutput(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-copy")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 5 >/dev/null 2>&1 </dev/null &\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if _, err := runCmdWithInput(ctx, []string{fake}, "test"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("command waited %s for forked child, want immediate parent completion", elapsed)
	}
}
