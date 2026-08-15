package asrcfg

import (
	"testing"

	"github.com/c/just-talk-go/config"
)

func TestEngineTypeDefault(t *testing.T) {
	if got := EngineType(config.VoiceConfig{}); got != "doubao" {
		t.Fatalf("default engine = %q", got)
	}
}

func TestConfigMapLegacyDoubao(t *testing.T) {
	m := ConfigMap(config.VoiceConfig{
		Engine:     "doubao",
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
		Engine: "doubao",
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
	eng, err := Build(config.VoiceConfig{Engine: "doubao", AppKey: "a", AccessKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if eng.Name() != "doubao-asr/1.0/async" {
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

func TestTypesIncludesEngines(t *testing.T) {
	types := Types()
	seen := map[string]bool{}
	for _, typ := range types {
		seen[typ] = true
	}
	for _, want := range []string{"doubao", "auralwise", "openai", "null"} {
		if !seen[want] {
			t.Fatalf("missing engine type %q in %v", want, types)
		}
	}
}
