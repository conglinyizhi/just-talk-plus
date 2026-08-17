// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsConsumeStartup(t *testing.T) {
	cfg := DiagnosticsConfig{StartupRuns: 3}
	for want := 2; want >= 0; want-- {
		if !cfg.ConsumeStartup() {
			t.Fatalf("run with %d remaining should collect diagnostics", want+1)
		}
		if cfg.StartupRuns != want {
			t.Fatalf("remaining runs=%d, want %d", cfg.StartupRuns, want)
		}
	}
	if cfg.ConsumeStartup() {
		t.Fatal("disabled diagnostics should not collect")
	}

	cfg.StartupRuns = DiagnosticsAlways
	if !cfg.ConsumeStartup() || cfg.StartupRuns != DiagnosticsAlways {
		t.Fatalf("persistent diagnostics=%d, want unchanged enabled state", cfg.StartupRuns)
	}
}

func TestSaveToDoesNotPersistDiagnosticsActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.Diagnostics.StartupRuns = 3
	cfg.Diagnostics.Active = true
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "active") {
		t.Fatalf("runtime diagnostics state must not be persisted:\n%s", data)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Diagnostics.StartupRuns != 3 || loaded.Diagnostics.Active {
		t.Fatalf("loaded diagnostics=%+v, want scheduled inactive diagnostics", loaded.Diagnostics)
	}
}
