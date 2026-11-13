// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package asrcfg

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/conglinyizhi/just-talk-plus/config"
)

func TestEngineTypeDefault(t *testing.T) {
	if got := EngineType(config.VoiceConfig{}); got != "doubao-stream" {
		t.Fatalf("default engine = %q", got)
	}
}

func TestConfigMapLegacyDoubao(t *testing.T) {
	m := ConfigMap(config.VoiceConfig{
		Engine:     "doubao-stream",
		AppKey:     "app123",
		AccessKey:  "secret",
		ResourceID: "volc.custom.resource",
	})
	if m["app_id"] != "app123" {
		t.Fatalf("app_id = %q", m["app_id"])
	}
	if m["access_key"] != "secret" {
		t.Fatalf("access_key = %q", m["access_key"])
	}
	if m["resource_id"] != "volc.custom.resource" {
		t.Fatalf("resource_id = %q", m["resource_id"])
	}
	// 废弃的全局 language 不再注入引擎配置
	if got, ok := m["language"]; ok {
		t.Fatalf("global language must not be injected into engine config, got %q", got)
	}
}

func TestConfigMapEngineConfigWins(t *testing.T) {
	// 语言只来自当前引擎的分组配置
	m := ConfigMap(config.VoiceConfig{
		Engine: "auralwise",
		EngineConfigs: map[string]map[string]string{
			"auralwise": {"api_key": "k1", "language": "ja"},
		},
	})
	if m["api_key"] != "k1" {
		t.Fatalf("api_key = %q", m["api_key"])
	}
	if m["language"] != "ja" {
		t.Fatalf("language = %q", m["language"])
	}
	// 旧版平铺 engine_config 对 doubao-stream 仍是回退来源
	m2 := ConfigMap(config.VoiceConfig{
		Engine: "doubao-stream",
		EngineConfig: map[string]string{
			"api_key":  "k1",
			"language": "ja",
		},
	})
	if m2["api_key"] != "k1" || m2["language"] != "ja" {
		t.Fatalf("flat engine_config should be read for doubao-stream: %v", m2)
	}
}

func TestConfigMapIgnoresDeprecatedGlobalLanguage(t *testing.T) {
	// 旧版全局 [voice].language 已废弃：只在 config.Load 迁移进分组，
	// 引擎构建不再从全局字段取语言（否则等于把一门语言强加给所有引擎）。
	for _, engine := range []string{"doubao-stream", "auralwise", "openai", "siliconflow", "dashscope", "mimo-asr"} {
		m := ConfigMap(config.VoiceConfig{
			Engine:   engine,
			Language: "zh-CN",
			EngineConfigs: map[string]map[string]string{
				engine: {"api_key": "k"},
			},
		})
		if got, ok := m["language"]; ok {
			t.Fatalf("%s 从废弃的全局 language 拿到了语言: %q", engine, got)
		}
	}
}

func TestConfigMapDoubaoLegacyFallsBackToEngineConfigAPIKey(t *testing.T) {
	// 新凭据走 engine_config.api_key，不应被旧字段覆盖
	m := ConfigMap(config.VoiceConfig{
		Engine: "doubao-stream",
		AppKey: "old-app",
		EngineConfig: map[string]string{
			"api_key": "new-key",
		},
	})
	if m["api_key"] != "new-key" {
		t.Fatalf("api_key = %q", m["api_key"])
	}
	if _, ok := m["app_id"]; ok {
		t.Fatal("legacy app_id should not be injected when api_key is set")
	}
}

func TestBuildDoubaoLegacy(t *testing.T) {
	eng, err := Build(config.VoiceConfig{Engine: "doubao-stream", AppKey: "a", AccessKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if eng.Name() != "doubao-stream" {
		t.Fatalf("name = %q", eng.Name())
	}
}

func TestBuildUnknownType(t *testing.T) {
	if _, err := Build(config.VoiceConfig{Engine: "no-such-engine"}); err == nil {
		t.Fatal("expected error for unknown engine")
	}
}

func TestBuildNull(t *testing.T) {
	eng, err := Build(config.VoiceConfig{Engine: "null"})
	if err != nil {
		t.Fatal(err)
	}
	if eng.Name() != "null" {
		t.Fatalf("name = %q", eng.Name())
	}
}

func TestBuildOpenaiRequiresKey(t *testing.T) {
	if _, err := Build(config.VoiceConfig{Engine: "openai"}); err == nil {
		t.Fatal("expected error for openai without api_key")
	}
}

func TestTypesUserFacing(t *testing.T) {
	types := Types()
	seen := map[string]bool{}
	for _, typ := range types {
		seen[typ] = true
	}
	// 只暴露规范引擎名，不暴露 null 与旧别名
	for _, want := range []string{"doubao-stream", "auralwise", "openai", "dashscope", "mimo-asr", "siliconflow"} {
		if !seen[want] {
			t.Fatalf("missing engine type %q in %v", want, types)
		}
	}
	for _, hidden := range []string{"null", "volc-asr", "doubao-asr", "aural-wise"} {
		if seen[hidden] {
			t.Fatalf("internal/alias engine type %q should not be user-facing: %v", hidden, types)
		}
	}
}

func TestDoubaoResourceDerivation(t *testing.T) {
	// 默认配置不应带 resource_id，避免覆盖按 model_version 推导的资源
	if cfg := config.Default(); cfg.Voice.ResourceID != "" {
		t.Fatalf("default ResourceID should be empty, got %q", cfg.Voice.ResourceID)
	}
	// 显式 2.0 → seedasr
	eng, err := Build(config.VoiceConfig{EngineConfig: map[string]string{"api_key": "k", "model_version": "2.0"}})
	if err != nil {
		t.Fatal(err)
	}
	_, detail := eng.Available(nil)
	if !strings.Contains(detail, "resource_stream=volc.seedasr.sauc.duration") {
		t.Fatalf("2.0 should derive seedasr resource: %s", detail)
	}
	// 新版 api_key 未指定 model_version → 默认 2.0（seedasr），避免 403
	eng, err = Build(config.VoiceConfig{EngineConfig: map[string]string{"api_key": "k"}})
	if err != nil {
		t.Fatal(err)
	}
	_, detail = eng.Available(nil)
	if !strings.Contains(detail, "resource_stream=volc.seedasr.sauc.duration") {
		t.Fatalf("new api_key should default to 2.0/seedasr: %s", detail)
	}
	// 旧版凭据未指定 model_version → 默认 1.0（bigasr）
	eng, err = Build(config.VoiceConfig{AppKey: "a", AccessKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	_, detail = eng.Available(nil)
	if !strings.Contains(detail, "resource_stream=volc.bigasr.sauc.duration") {
		t.Fatalf("legacy credentials should default to 1.0/bigasr: %s", detail)
	}
	// 显式 resource_id 优先
	eng, err = Build(config.VoiceConfig{EngineConfig: map[string]string{"api_key": "k", "resource_id": "volc.custom.res"}})
	if err != nil {
		t.Fatal(err)
	}
	_, detail = eng.Available(nil)
	if !strings.Contains(detail, "resource_stream=volc.custom.res") {
		t.Fatalf("explicit resource_id should win: %s", detail)
	}
}

func TestConfigMapNewAPIKeySkipsLegacyFields(t *testing.T) {
	// 新版 api_key 时，顶层旧版平铺字段（app_id/access_key/resource_id）不应注入，
	// 否则旧 resource_id 会把资源钉死在 1.0/bigasr 导致新版 key 403
	m := ConfigMap(config.VoiceConfig{
		Engine:     "doubao-stream",
		AppKey:     "legacy-app",
		AccessKey:  "legacy-secret",
		ResourceID: "volc.bigasr.sauc.duration",
		EngineConfig: map[string]string{
			"api_key": "new-key",
		},
	})
	if m["api_key"] != "new-key" {
		t.Fatalf("api_key = %q", m["api_key"])
	}
	for _, key := range []string{"app_id", "access_key", "resource_id"} {
		if _, ok := m[key]; ok {
			t.Fatalf("legacy field %q should not be injected with new api_key: %v", key, m)
		}
	}
	// 旧版凭据（无 api_key）仍按旧行为合并
	m2 := ConfigMap(config.VoiceConfig{
		Engine:     "doubao-stream",
		AppKey:     "a",
		AccessKey:  "s",
		ResourceID: "volc.custom.res",
	})
	if m2["app_id"] != "a" || m2["access_key"] != "s" || m2["resource_id"] != "volc.custom.res" {
		t.Fatalf("legacy merge broken: %v", m2)
	}
}

func TestConfigMapPerEngineWins(t *testing.T) {
	// engine_configs[engine] 优先于旧平铺 engine_config（分组存储迁移后）
	m := ConfigMap(config.VoiceConfig{
		Engine: "openai",
		EngineConfig: map[string]string{
			"api_key": "legacy-flat",
		},
		EngineConfigs: map[string]map[string]string{
			"openai": {"api_key": "per-engine"},
		},
	})
	if m["api_key"] != "per-engine" {
		t.Fatalf("api_key = %q, want per-engine value", m["api_key"])
	}
}

func TestConfigMapDoubaoStreamFallsBackToFlat(t *testing.T) {
	// 旧版平铺 engine_config 属于 doubao-stream：该引擎尚无分组配置时回退读取
	m := ConfigMap(config.VoiceConfig{
		Engine: "doubao-stream",
		EngineConfig: map[string]string{
			"api_key": "flat-key",
		},
	})
	if m["api_key"] != "flat-key" {
		t.Fatalf("api_key = %q, want flat fallback for doubao-stream", m["api_key"])
	}
}

func TestConfigMapFlatConfigNotSharedWithOtherEngines(t *testing.T) {
	// 凭据隔离：豆包的平铺配置不能漏给别的引擎，否则请求会 401 还泄漏 key
	for _, engine := range []string{"auralwise", "openai", "dashscope", "mimo-asr", "siliconflow"} {
		m := ConfigMap(config.VoiceConfig{
			Engine: engine,
			EngineConfig: map[string]string{
				"api_key": "doubao-flat-key",
			},
		})
		if got, ok := m["api_key"]; ok {
			t.Fatalf("%s 从旧版平铺 engine_config 拿到了凭据: api_key=%q", engine, got)
		}
	}
}

func TestStreamParamsPerEngine(t *testing.T) {
	vc := config.VoiceConfig{
		Engine: "doubao-stream",
		EngineConfigs: map[string]map[string]string{
			"doubao-stream": {"language": " zh-CN ", "hotwords": "Wayland, wl-copy，Sway\n , Wayland"},
			"openai":        {"language": "en", "hotwords": "Groq"},
		},
	}
	p := StreamParams(vc)
	if p.Lang != "zh-CN" {
		t.Fatalf("lang = %q, want trimmed zh-CN", p.Lang)
	}
	// 逗号分隔 → 列表：去空白、丢空项、去重
	want := []string{"Wayland", "wl-copy", "Sway"}
	if !reflect.DeepEqual(p.Hotwords, want) {
		t.Fatalf("hotwords = %v, want %v", p.Hotwords, want)
	}

	// 切引擎后各读自己的分组，互不影响
	vc.Engine = "openai"
	p = StreamParams(vc)
	if p.Lang != "en" {
		t.Fatalf("openai lang = %q, want en", p.Lang)
	}
	if !reflect.DeepEqual(p.Hotwords, []string{"Groq"}) {
		t.Fatalf("openai hotwords = %v, want [Groq]", p.Hotwords)
	}
}

func TestStreamParamsIgnoresDeprecatedGlobalFields(t *testing.T) {
	// 旧版全局 language/hotwords 不再进会话参数（迁移在 config.Load 里做）
	p := StreamParams(config.VoiceConfig{
		Engine:   "openai",
		Language: "zh-CN",
		Hotwords: []string{"Wayland"},
	})
	if p.Lang != "" || len(p.Hotwords) != 0 {
		t.Fatalf("deprecated global fields leaked into session params: %+v", p)
	}
}

func TestStreamParamsDoubaoFlatFallback(t *testing.T) {
	vc := config.VoiceConfig{
		Engine: "doubao-stream",
		EngineConfig: map[string]string{
			"api_key":  "flat-key",
			"language": "en",
			"hotwords": "Sway, wtype",
		},
	}
	p := StreamParams(vc)
	if p.Lang != "en" {
		t.Fatalf("doubao lang = %q, want flat fallback en", p.Lang)
	}
	if !reflect.DeepEqual(p.Hotwords, []string{"Sway", "wtype"}) {
		t.Fatalf("doubao hotwords = %v", p.Hotwords)
	}

	// 平铺配置只属于 doubao-stream：别的引擎拿不到它的语言与热词
	vc.Engine = "siliconflow"
	p = StreamParams(vc)
	if p.Lang != "" || len(p.Hotwords) != 0 {
		t.Fatalf("siliconflow must not inherit flat language/hotwords: %+v", p)
	}
}

func TestStreamParamsEmptyHotwords(t *testing.T) {
	p := StreamParams(config.VoiceConfig{
		Engine: "openai",
		EngineConfigs: map[string]map[string]string{
			"openai": {"hotwords": " , ， \n"},
		},
	})
	if len(p.Hotwords) != 0 {
		t.Fatalf("hotwords = %v, want empty", p.Hotwords)
	}
}

func TestStreamParamsAfterLegacyMigration(t *testing.T) {
	// 端到端：旧版全局 language/hotwords 经 config.Load 迁移后，
	// 会话参数应真的拿到该引擎的语言与热词（跨包接缝）。
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[voice]
engine = "doubao-stream"
language = "zh-CN"
hotwords = ["Wayland", "wl-copy"]

[voice.engine_configs.doubao-stream]
api_key = "k"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := StreamParams(cfg.Voice)
	if p.Lang != "zh-CN" {
		t.Fatalf("lang = %q, want migrated zh-CN", p.Lang)
	}
	if !reflect.DeepEqual(p.Hotwords, []string{"Wayland", "wl-copy"}) {
		t.Fatalf("hotwords = %v, want migrated list", p.Hotwords)
	}
}
