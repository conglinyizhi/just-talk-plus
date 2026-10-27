// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package tray

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
)

// useTempModelsCache 把模型缓存指向 t.TempDir()，绝不写真实用户家目录。
func useTempModelsCache(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models-cache.json")
	old := modelsCachePathFn
	modelsCachePathFn = func() string { return path }
	t.Cleanup(func() { modelsCachePathFn = old })
	return path
}

func TestFetchModelsParsesDedupesSorts(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("sub_type")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"TeleAI/TeleSpeechASR"},
			{"id":"FunAudioLLM/SenseVoiceSmall"},
			{"id":"TeleAI/TeleSpeechASR"},
			{"id":"  "},
			{"id":"FunAudioLLM/SenseVoiceSmall"}
		]}`))
	}))
	defer srv.Close()

	models, err := fetchModels(context.Background(), srv.URL+"/v1/", "sk-test")
	if err != nil {
		t.Fatalf("fetchModels error: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("请求路径 = %q, want /v1/models", gotPath)
	}
	if gotQuery != "speech-to-text" {
		t.Errorf("sub_type = %q, want speech-to-text", gotQuery)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
	want := []string{"FunAudioLLM/SenseVoiceSmall", "TeleAI/TeleSpeechASR"}
	if len(models) != len(want) {
		t.Fatalf("模型列表 = %v, want %v（去重 + 排序 + 丢弃空 id）", models, want)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("模型列表 = %v, want %v", models, want)
		}
	}
}

func TestFetchModelsDefaultBaseURL(t *testing.T) {
	// 空 base_url 回落到内置默认根地址：这里只验证不会拼出畸形 URL。
	if got := siliconflowDefaultBaseURL; got != "https://api.siliconflow.cn/v1" {
		t.Fatalf("默认 base_url = %q", got)
	}
	if _, err := fetchModels(context.Background(), "", ""); err == nil {
		t.Fatal("缺 api_key 时应返回错误")
	}
}

func TestFetchModelsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":30014,"message":"Token is invalid.","data":null}`))
	}))
	defer srv.Close()

	_, err := fetchModels(context.Background(), srv.URL+"/v1", "sk-bad")
	if err == nil {
		t.Fatal("401 时应返回错误")
	}
	for _, want := range []string{"HTTP 401", "Token is invalid.", "code=30014"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误串 %q 缺少 %q", err.Error(), want)
		}
	}
}

func TestFetchModelsHTTPErrorPlainBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream connect error"))
	}))
	defer srv.Close()

	_, err := fetchModels(context.Background(), srv.URL+"/v1", "sk-bad")
	if err == nil {
		t.Fatal("502 时应返回错误")
	}
	for _, want := range []string{"HTTP 502", "upstream connect error"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误串 %q 缺少 %q", err.Error(), want)
		}
	}
}

func TestFetchModelsEmptyListIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	if _, err := fetchModels(context.Background(), srv.URL+"/v1", "sk-test"); err == nil {
		t.Fatal("接口返回空列表时应返回错误，避免把空缓存写进去")
	}
}

func TestCachedModelsTTL(t *testing.T) {
	useTempModelsCache(t)
	fresh := []string{"a-model", "b-model"}
	if err := writeModelsCache(siliconflowEngine, fresh, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("writeModelsCache: %v", err)
	}
	got := cachedModels(siliconflowEngine, time.Now())
	if len(got) != 2 || got[0] != "a-model" || got[1] != "b-model" {
		t.Fatalf("未过期缓存 = %v, want %v", got, fresh)
	}

	// 超过 24h 的缓存视同没有：回落到内置兜底。
	if err := writeModelsCache(siliconflowEngine, []string{"stale-model"}, time.Now().Add(-25*time.Hour)); err != nil {
		t.Fatalf("writeModelsCache: %v", err)
	}
	if got := cachedModels(siliconflowEngine, time.Now()); got != nil {
		t.Fatalf("过期缓存 = %v, want nil", got)
	}
	choices := modelMenuChoices(siliconflowEngine, siliconflowDefaultModel)
	if len(choices) != len(builtinModelChoices(siliconflowEngine)) {
		t.Fatalf("过期后菜单候选 = %v, want 内置兜底 %v", choices, builtinModelChoices(siliconflowEngine))
	}
	if choices[0].ID != siliconflowDefaultModel || choices[0].Label != "默认，快" {
		t.Fatalf("内置兜底首项 = %+v", choices[0])
	}
}

func TestCachedModelsCorruptFileFallsBack(t *testing.T) {
	path := useTempModelsCache(t)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("写坏缓存: %v", err)
	}
	if got := cachedModels(siliconflowEngine, time.Now()); got != nil {
		t.Fatalf("损坏缓存 = %v, want nil", got)
	}
	// 损坏文件不能挡住写：重新写入后应能读回。
	if err := writeModelsCache(siliconflowEngine, []string{"FunAudioLLM/SenseVoiceSmall"}, time.Now()); err != nil {
		t.Fatalf("writeModelsCache over corrupt file: %v", err)
	}
	if got := cachedModels(siliconflowEngine, time.Now()); len(got) != 1 {
		t.Fatalf("重写后缓存 = %v", got)
	}
}

func TestWriteModelsCacheKeepsOtherEngines(t *testing.T) {
	useTempModelsCache(t)
	now := time.Now()
	if err := writeModelsCache("other-engine", []string{"x"}, now); err != nil {
		t.Fatalf("writeModelsCache: %v", err)
	}
	if err := writeModelsCache(siliconflowEngine, []string{"y"}, now); err != nil {
		t.Fatalf("writeModelsCache: %v", err)
	}
	if got := cachedModels("other-engine", now); len(got) != 1 || got[0] != "x" {
		t.Fatalf("其它引擎缓存被覆盖: %v", got)
	}
}

// TestRefreshModelsWithoutAPIKeyDoesNothing 锁住「没有凭据就不发请求、不写缓存」
// 的行为（启动时的静默刷新路径；explicit 分支会弹桌面提示，不在测试里触发）。
func TestRefreshModelsWithoutAPIKeyDoesNothing(t *testing.T) {
	path := useTempModelsCache(t)
	p := &Plugin{env: fakeEnv{cfg: config.Default()}, logger: slog.Default()}
	p.refreshModels(false)
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("没有 api_key 时不应写缓存: %v", err)
	}
}

func TestModelMenuChoicesAddsCurrentModel(t *testing.T) {
	useTempModelsCache(t)
	if err := writeModelsCache(siliconflowEngine, []string{"FunAudioLLM/SenseVoiceSmall", "Qwen/Qwen3-ASR"}, time.Now()); err != nil {
		t.Fatalf("writeModelsCache: %v", err)
	}
	choices := modelMenuChoices(siliconflowEngine, "My/CustomModel")
	seen := map[string]bool{}
	for _, c := range choices {
		seen[c.ID] = true
	}
	if !seen["Qwen/Qwen3-ASR"] {
		t.Errorf("缓存里的模型没进菜单: %v", choices)
	}
	if !seen["My/CustomModel"] {
		t.Errorf("当前配置的模型没进菜单（勾选态会无处可勾）: %v", choices)
	}
	if !seen[siliconflowDefaultModel] {
		t.Errorf("内置兜底没进菜单: %v", choices)
	}
	// 不能出现重复项。
	if len(seen) != len(choices) {
		t.Errorf("候选有重复: %v", choices)
	}
	// 其它引擎没有模型子菜单。
	if got := modelMenuChoices("openai", "whisper-1"); len(got) != 0 {
		t.Errorf("openai 不应有模型候选: %v", got)
	}
}

func TestCurrentModel(t *testing.T) {
	// 未配置 model：回落到引擎默认。
	empty := config.Default()
	if got := currentModel(empty, siliconflowEngine); got != siliconflowDefaultModel {
		t.Errorf("currentModel(空配置) = %q, want %q", got, siliconflowDefaultModel)
	}
	if got := currentModel(nil, siliconflowEngine); got != siliconflowDefaultModel {
		t.Errorf("currentModel(nil) = %q, want %q", got, siliconflowDefaultModel)
	}

	// 分组配置里的 model 优先。
	cfg := config.Default()
	cfg.Voice.EngineConfigs = map[string]map[string]string{
		siliconflowEngine: {"model": "TeleAI/TeleSpeechASR"},
	}
	if got := currentModel(cfg, siliconflowEngine); got != "TeleAI/TeleSpeechASR" {
		t.Errorf("currentModel(分组配置) = %q", got)
	}

	// 旧版平铺 engine_config 作为回退来源。
	legacy := config.Default()
	legacy.Voice.EngineConfig = map[string]string{"model": "legacy/Model"}
	if got := currentModel(legacy, siliconflowEngine); got != "legacy/Model" {
		t.Errorf("currentModel(旧版平铺) = %q, want legacy/Model", got)
	}
}

func TestApplyEngineModelWritesGroupedConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "openai"
	cfg.Voice.EngineConfigs = map[string]map[string]string{
		"openai":          {"api_key": "sk-openai", "model": "whisper-1"},
		siliconflowEngine: {"api_key": "sk-sf"},
	}
	openaiCfg := cfg.Voice.EngineConfigs["openai"]
	sfCfg := cfg.Voice.EngineConfigs[siliconflowEngine]

	if !applyEngineModel(cfg, siliconflowEngine, "TeleAI/TeleSpeechASR") {
		t.Fatal("切换模型应报告变更")
	}
	if cfg.Voice.Engine != siliconflowEngine {
		t.Errorf("Voice.Engine = %q, want siliconflow（选模型等于切引擎）", cfg.Voice.Engine)
	}
	if got := cfg.Voice.EngineConfigs[siliconflowEngine]["model"]; got != "TeleAI/TeleSpeechASR" {
		t.Errorf("siliconflow model = %q", got)
	}
	if got := cfg.Voice.EngineConfigs[siliconflowEngine]["api_key"]; got != "sk-sf" {
		t.Errorf("siliconflow api_key 被改动: %q", got)
	}
	// 其它引擎的配置不能被污染。
	if got := cfg.Voice.EngineConfigs["openai"]["model"]; got != "whisper-1" {
		t.Errorf("openai model 被改写: %q", got)
	}
	if _, mutated := sfCfg["model"]; mutated {
		// 原 map 被原地改过就说明复制没生效（这里原 map 没有 model，被写进来即污染）。
		t.Error("共享的 engine_configs map 被原地修改")
	}
	if len(openaiCfg) != 2 {
		t.Errorf("openai 配置条目数 = %d, want 2", len(openaiCfg))
	}

	// 同样的 model 再设一次：不算变更。
	if applyEngineModel(cfg, siliconflowEngine, "TeleAI/TeleSpeechASR") {
		t.Error("重复设置相同模型不应报告变更")
	}
	// 空参数不写配置。
	if applyEngineModel(cfg, siliconflowEngine, "  ") {
		t.Error("空模型不应写入")
	}
	if applyEngineModel(nil, siliconflowEngine, "x") {
		t.Error("nil 配置应安全返回 false")
	}
}

func TestApplyEngineModelPromotesLegacyFlatConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = siliconflowEngine
	cfg.Voice.EngineConfig = map[string]string{"api_key": "sk-legacy", "model": "legacy/Old"}

	if !applyEngineModel(cfg, siliconflowEngine, "TeleAI/TeleSpeechASR") {
		t.Fatal("应报告变更")
	}
	got := cfg.Voice.EngineConfigs[siliconflowEngine]
	if got["model"] != "TeleAI/TeleSpeechASR" {
		t.Errorf("分组配置 model = %q", got["model"])
	}
	if got["api_key"] != "sk-legacy" {
		t.Errorf("旧版平铺凭据没迁移进分组配置: %q", got)
	}
	// 旧版平铺 map 保持原样（不原地改、不清空）。
	if cfg.Voice.EngineConfig["model"] != "legacy/Old" {
		t.Errorf("旧版平铺 engine_config 被改动: %v", cfg.Voice.EngineConfig)
	}
}

// TestModelsCacheFileShape 锁住缓存文件形态，方便排查用户目录里的缓存。
func TestModelsCacheFileShape(t *testing.T) {
	path := useTempModelsCache(t)
	now := time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC)
	if err := writeModelsCache(siliconflowEngine, []string{"b-model", "a-model", "a-model"}, now); err != nil {
		t.Fatalf("writeModelsCache: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读缓存: %v", err)
	}
	var all map[string]struct {
		FetchedAt string   `json:"fetched_at"`
		Models    []string `json:"models"`
	}
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatalf("缓存不是合法 JSON: %v", err)
	}
	entry, ok := all[siliconflowEngine]
	if !ok {
		t.Fatalf("缓存缺少 %s 段: %s", siliconflowEngine, data)
	}
	if entry.FetchedAt != "2026-09-02T10:30:00Z" {
		t.Errorf("fetched_at = %q, want RFC3339 时间戳", entry.FetchedAt)
	}
	if len(entry.Models) != 2 || entry.Models[0] != "a-model" || entry.Models[1] != "b-model" {
		t.Errorf("models = %v, want 去重排序 [a-model b-model]", entry.Models)
	}
}
