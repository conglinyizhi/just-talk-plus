// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
)

// TestSystemdUnit verifies the embedded unit template substitutes the
// executable path and keeps journal logging.
func TestSystemdUnit(t *testing.T) {
	exe := "/opt/just-talk-plus/bin"
	unit := systemdUnit(exe)
	if !strings.Contains(unit, "ExecStart="+exe+" --gui") {
		t.Errorf("unit 未含可执行路径:\n%s", unit)
	}
	if strings.Contains(unit, "__EXE__") {
		t.Errorf("unit 仍含占位符 __EXE__:\n%s", unit)
	}
	if !strings.Contains(unit, "StandardOutput=journal") {
		t.Errorf("unit 缺 journal 接管:\n%s", unit)
	}
}

// TestSystemdUnitEscapesPath ensures a path containing spaces is escaped so
// systemd does not split it into separate arguments.
func TestSystemdBackendArgs(t *testing.T) {
	oldBackend, oldWayland, oldSession, oldDisplay := os.Getenv("JUST_TALK_PLUS_BACKEND"), os.Getenv("WAYLAND_DISPLAY"), os.Getenv("XDG_SESSION_TYPE"), os.Getenv("DISPLAY")
	t.Cleanup(func() {
		os.Setenv("JUST_TALK_PLUS_BACKEND", oldBackend)
		os.Setenv("WAYLAND_DISPLAY", oldWayland)
		os.Setenv("XDG_SESSION_TYPE", oldSession)
		os.Setenv("DISPLAY", oldDisplay)
	})
	os.Setenv("JUST_TALK_PLUS_BACKEND", "")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	os.Setenv("XDG_SESSION_TYPE", "wayland")
	os.Setenv("DISPLAY", "")
	if got := systemdBackendArgs(); got != " --backend wayland" {
		t.Fatalf("Wayland args = %q", got)
	}
	os.Setenv("JUST_TALK_PLUS_BACKEND", "x11")
	if got := systemdBackendArgs(); got != " --backend x11" {
		t.Fatalf("explicit X11 args = %q", got)
	}
}

func TestSystemdUnitEscapesPath(t *testing.T) {
	unit := systemdUnit("/opt/my app/just-talk-plus")
	if !strings.Contains(unit, "/opt/my\\x20app/just-talk-plus --gui") {
		t.Errorf("路径未转义空格:\n%s", unit)
	}
}
