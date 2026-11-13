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

func TestEngineConfigForPerEngineWins(t *testing.T) {
	vc := VoiceConfig{
		EngineConfig:  map[string]string{"api_key": "flat-key"},
		EngineConfigs: map[string]map[string]string{"doubao-stream": {"api_key": "grouped-key"}},
	}
	if got := vc.EngineConfigFor("doubao-stream"); got["api_key"] != "grouped-key" {
		t.Fatalf("api_key = %q, want grouped value", got["api_key"])
	}
}

func TestEngineConfigForFlatFallbackOnlyForDoubaoStream(t *testing.T) {
	vc := VoiceConfig{EngineConfig: map[string]string{"api_key": "flat-key"}}
	if got := vc.EngineConfigFor("doubao-stream"); got["api_key"] != "flat-key" {
		t.Fatalf("doubao-stream should fall back to flat engine_config, got %v", got)
	}
	// 凭据隔离：豆包的平铺配置不借给别的引擎。
	for _, engine := range []string{"auralwise", "openai", "dashscope", "mimo-asr", "siliconflow", "null"} {
		if got := vc.EngineConfigFor(engine); got != nil {
			t.Fatalf("%s must not inherit flat engine_config, got %v", engine, got)
		}
	}
}

func TestEngineConfigForUnconfigured(t *testing.T) {
	var vc VoiceConfig
	if got := vc.EngineConfigFor("doubao-stream"); got != nil {
		t.Fatalf("empty flat config should yield nil, got %v", got)
	}
	// 空 map 也不算配置。
	vc.EngineConfig = map[string]string{}
	if got := vc.EngineConfigFor("doubao-stream"); got != nil {
		t.Fatalf("empty flat map should yield nil, got %v", got)
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
