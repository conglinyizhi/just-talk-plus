// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build linux

package main

import (
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
func TestSystemdUnitEscapesPath(t *testing.T) {
	unit := systemdUnit("/opt/my app/just-talk-plus")
	if !strings.Contains(unit, "/opt/my\\x20app/just-talk-plus --gui") {
		t.Errorf("路径未转义空格:\n%s", unit)
	}
}
