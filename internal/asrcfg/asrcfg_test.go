package asrcfg

import (
	"strings"
	"testing"

	"github.com/c/just-talk-go/config"
)

func TestEngineTypeDefault(t *testing.T) {
	if got := EngineType(config.VoiceConfig{}); got != "doubao-stream" {
		t.Fatalf("default engine = %q", got)
	}
}

func TestConfigMapLegacyDoubao(t *testing.T) {
	m := ConfigMap(config.VoiceConfig{
		Engine:     "doubao-stream",
		Language:   "zh-CN",
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
	if m["language"] != "zh-CN" {
		t.Fatalf("language = %q", m["language"])
	}
}

func TestConfigMapEngineConfigWins(t *testing.T) {
	m := ConfigMap(config.VoiceConfig{
		Engine:   "auralwise",
		Language: "en",
		EngineConfig: map[string]string{
			"api_key":  "k1",
			"language": "ja",
		},
	})
	if m["api_key"] != "k1" {
		t.Fatalf("api_key = %q", m["api_key"])
	}
	// engine_config 中的 language 优先于共享字段
	if m["language"] != "ja" {
		t.Fatalf("language = %q", m["language"])
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
	for _, want := range []string{"doubao-stream", "auralwise", "openai"} {
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
	// 2.0 → seedasr
	eng, err := Build(config.VoiceConfig{EngineConfig: map[string]string{"api_key": "k", "model_version": "2.0"}})
	if err != nil {
		t.Fatal(err)
	}
	_, detail := eng.Available(nil)
	if !strings.Contains(detail, "resource_stream=volc.seedasr.sauc.duration") {
		t.Fatalf("2.0 should derive seedasr resource: %s", detail)
	}
	// 默认 1.0 → bigasr（与旧版行为一致）
	eng, err = Build(config.VoiceConfig{EngineConfig: map[string]string{"api_key": "k"}})
	if err != nil {
		t.Fatal(err)
	}
	_, detail = eng.Available(nil)
	if !strings.Contains(detail, "resource_stream=volc.bigasr.sauc.duration") {
		t.Fatalf("1.0 should derive bigasr resource: %s", detail)
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
