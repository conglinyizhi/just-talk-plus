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

	"github.com/BurntSushi/toml"
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

// writeTempConfig 写一份临时 config.toml 并返回路径（绝不碰真实家目录）。
func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMigratesLegacyLanguageAndHotwordsIntoGroups(t *testing.T) {
	path := writeTempConfig(t, `
[voice]
engine = "doubao-stream"
language = "zh-CN"
hotwords = ["Wayland", "wl-copy", "Wayland", " "]

[voice.engine_configs.doubao-stream]
api_key = "doubao-key"

[voice.engine_configs.openai]
api_key = "sk-openai"
language = "ja-JP"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	vc := cfg.Voice

	doubao := vc.EngineConfigs["doubao-stream"]
	if doubao["language"] != "zh-CN" {
		t.Fatalf("doubao language = %q, want migrated zh-CN", doubao["language"])
	}
	// 去空白、丢空项、去重后落成逗号分隔字符串
	if doubao["hotwords"] != "Wayland, wl-copy" {
		t.Fatalf("doubao hotwords = %q, want %q", doubao["hotwords"], "Wayland, wl-copy")
	}
	if doubao["api_key"] != "doubao-key" {
		t.Fatalf("migration must not touch existing keys: %v", doubao)
	}

	openai := vc.EngineConfigs["openai"]
	if openai["language"] != "ja-JP" {
		t.Fatalf("existing group language must win, got %q", openai["language"])
	}
	if openai["hotwords"] != "Wayland, wl-copy" {
		t.Fatalf("openai hotwords = %q, want migrated value", openai["hotwords"])
	}

	// 迁移只为已存在的分组补键，不给没配过的引擎凭空建组
	if len(vc.EngineConfigs) != 2 {
		t.Fatalf("engine groups = %v, want only the two configured engines", vc.EngineConfigs)
	}
	if vc.Language != "" || len(vc.Hotwords) != 0 {
		t.Fatalf("legacy fields must be cleared after migration: language=%q hotwords=%v", vc.Language, vc.Hotwords)
	}
}

func TestLoadMigratesLegacyFlatEngineConfig(t *testing.T) {
	path := writeTempConfig(t, `
[voice]
language = "en"
hotwords = ["Sway"]

[voice.engine_config]
api_key = "flat-key"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	vc := cfg.Voice
	if got := vc.EngineConfig["language"]; got != "en" {
		t.Fatalf("flat language = %q, want migrated en", got)
	}
	if got := vc.EngineConfig["hotwords"]; got != "Sway" {
		t.Fatalf("flat hotwords = %q, want migrated Sway", got)
	}
	if vc.EngineConfig["api_key"] != "flat-key" {
		t.Fatalf("flat credentials must be preserved: %v", vc.EngineConfig)
	}
	// 没有分组只有平铺配置：不应创建任何引擎分组
	if len(vc.EngineConfigs) != 0 {
		t.Fatalf("migration must not create engine groups, got %v", vc.EngineConfigs)
	}
	if vc.Language != "" || len(vc.Hotwords) != 0 {
		t.Fatalf("legacy fields must be cleared: language=%q hotwords=%v", vc.Language, vc.Hotwords)
	}
}

// TestLoadMigratesSharedASRToLegacyDoubaoCreds 最老的一批配置只有顶层
// app_key/access_key/resource_id（没有分组、也没有平铺 engine_config）：
// 全局 language/hotwords 必须落到 doubao-stream 的分组里，不能静默丢弃。
func TestLoadMigratesSharedASRToLegacyDoubaoCreds(t *testing.T) {
	path := writeTempConfig(t, `
[voice]
language = "zh-CN"
hotwords = ["Wayland", "Sway"]
app_key = "legacy-app"
access_key = "legacy-token"
resource_id = "volc.bigasr.sauc.duration"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	vc := cfg.Voice
	doubao := vc.EngineConfigs["doubao-stream"]
	if doubao == nil {
		t.Fatalf("应为只有顶层凭据的老配置建 doubao-stream 分组，实际 %v", vc.EngineConfigs)
	}
	if doubao["language"] != "zh-CN" {
		t.Fatalf("doubao language = %q, want zh-CN", doubao["language"])
	}
	if doubao["hotwords"] != "Wayland, Sway" {
		t.Fatalf("doubao hotwords = %q, want %q", doubao["hotwords"], "Wayland, Sway")
	}
	// 凭据仍留在顶层字段里（asrcfg 会合并），迁移不该搬走它们
	if vc.AppKey != "legacy-app" || vc.AccessKey != "legacy-token" {
		t.Fatalf("顶层凭据被迁移动过: app_key=%q access_key=%q", vc.AppKey, vc.AccessKey)
	}
	if vc.Language != "" || len(vc.Hotwords) != 0 {
		t.Fatalf("legacy fields must be cleared: language=%q hotwords=%v", vc.Language, vc.Hotwords)
	}
}

// TestLoadMigrationSkipsLegacyDoubaoWhenFlatConfigHasKeys 平铺配置里有内容时
// 不能再建 doubao 分组：豆包那时读平铺配置，建组会盖住平铺里的 api_key。
func TestLoadMigrationSkipsLegacyDoubaoWhenFlatConfigHasKeys(t *testing.T) {
	path := writeTempConfig(t, `
[voice]
language = "zh-CN"
engine_config = { api_key = "flat-key" }
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	vc := cfg.Voice
	if _, exists := vc.EngineConfigs["doubao-stream"]; exists {
		t.Fatalf("平铺配置有 api_key 时不应建分组: %v", vc.EngineConfigs)
	}
	if vc.EngineConfig["language"] != "zh-CN" {
		t.Fatalf("language 应迁移进平铺配置: %v", vc.EngineConfig)
	}
}

func TestLoadMigrationIsIdempotent(t *testing.T) {
	path := writeTempConfig(t, `
[voice]
language = "zh-CN"
hotwords = ["Wayland"]

[voice.engine_configs.doubao-stream]
api_key = "doubao-key"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	before := cfg.Voice.EngineConfigs["doubao-stream"]["hotwords"]
	// 再调一次迁移：字段已清空，必须是 no-op，不能重复追加热词
	cfg.Voice.migrateLegacySharedASRConfig()
	if got := cfg.Voice.EngineConfigs["doubao-stream"]["hotwords"]; got != before {
		t.Fatalf("second migration changed hotwords: %q -> %q", before, got)
	}
	if got := cfg.Voice.EngineConfigs["doubao-stream"]["language"]; got != "zh-CN" {
		t.Fatalf("second migration changed language: %q", got)
	}
	// 重复 Load 同一份文件结果稳定
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Voice.EngineConfigs["doubao-stream"]["hotwords"]; got != before {
		t.Fatalf("reload hotwords = %q, want %q", got, before)
	}
}

func TestSaveToOmitsDeprecatedGlobalASRFields(t *testing.T) {
	cfg := &Config{Voice: VoiceConfig{
		Engine:        "doubao-stream",
		Language:      "zh-CN",
		Hotwords:      []string{"Wayland", "wl-copy"},
		EngineConfigs: map[string]map[string]string{"doubao-stream": {"api_key": "k"}},
	}}
	cfg.Voice.migrateLegacySharedASRConfig()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode saved config: %v\n%s", err, data)
	}
	voice, ok := decoded["voice"].(map[string]any)
	if !ok {
		t.Fatalf("saved config has no [voice]:\n%s", data)
	}
	for _, key := range []string{"language", "hotwords"} {
		if _, ok := voice[key]; ok {
			t.Fatalf("deprecated global %q must not be persisted:\n%s", key, data)
		}
	}
	groups, _ := voice["engine_configs"].(map[string]any)
	doubao, _ := groups["doubao-stream"].(map[string]any)
	if doubao["language"] != "zh-CN" || doubao["hotwords"] != "Wayland, wl-copy" {
		t.Fatalf("migrated per-engine values missing:\n%s", data)
	}
}
