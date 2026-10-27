// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package tray

// 三级菜单（引擎 → 模型）用到的模型候选、缓存与拉取逻辑。
//
// 两条硬约束：
//   - 构建菜单这一段绝不发网络请求：只用缓存或内置兜底，菜单永远不为空；
//   - 网络拉取只在后台进行（onReady 末尾一次、用户点「刷新模型列表」一次），
//     失败只打 Warn，不影响菜单，也不影响录音。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
)

const (
	// siliconflowEngine 硅基流动的引擎类型名（与 asrcfg.Types() 中的名字一致）。
	siliconflowEngine = "siliconflow"
	// siliconflowDefaultBaseURL / siliconflowDefaultModel 与
	// internal/speech/siliconflow/engine.go 里的 defaultBaseURL / defaultModel
	// 保持一致。那边没有导出这两个常量，这里复制一份而不是跨包引用，免得
	// tray 为了两个字符串去 import 具体引擎包（asrcfg 已经空导入注册过它）。
	siliconflowDefaultBaseURL = "https://api.siliconflow.cn/v1"
	siliconflowDefaultModel   = "FunAudioLLM/SenseVoiceSmall"
	// siliconflowTeleAIModel 内置兜底候选里的第二个模型。
	siliconflowTeleAIModel = "TeleAI/TeleSpeechASR"
	// modelsCacheTTL 模型列表缓存的有效期。
	modelsCacheTTL = 24 * time.Hour
	// modelsFetchTimeout 后台拉取模型列表的超时。
	modelsFetchTimeout = 5 * time.Second
)

// modelChoice 是菜单里的一个模型候选项：ID 写进配置，Label 作为菜单提示。
type modelChoice struct {
	ID    string
	Label string
}

// builtinModelChoices 内置兜底候选：缓存缺失 / 过期 / 损坏时菜单就用这些，
// 保证模型子菜单不会空着（用户可以马上选一个能用的，而不是面对空菜单）。
func builtinModelChoices(engine string) []modelChoice {
	if engine != siliconflowEngine {
		return nil
	}
	return []modelChoice{
		{ID: siliconflowDefaultModel, Label: "默认，快"},
		{ID: siliconflowTeleAIModel, Label: "备选模型"},
	}
}

// defaultModelFor 返回引擎的默认模型（未配置 model 时引擎自己会用的那个）。
func defaultModelFor(engine string) string {
	if engine == siliconflowEngine {
		return siliconflowDefaultModel
	}
	return ""
}

// currentModel 返回当前生效的模型：engine_configs[engine].model，
// 未配置时回落到该引擎的默认模型。
func currentModel(cfg *config.Config, engine string) string {
	if cfg != nil {
		if v := strings.TrimSpace(cfg.Voice.EngineConfigFor(engine)["model"]); v != "" {
			return v
		}
	}
	return defaultModelFor(engine)
}

// modelMenuChoices 汇总菜单候选：内置兜底在前，缓存里多出来的模型按 id 排序
// 追加，最后确保当前配置的模型一定在列表里——否则勾选态没有可勾的那一项
// （用户手填了接口没返回的模型时就是这个情形）。
func modelMenuChoices(engine, current string) []modelChoice {
	builtin := builtinModelChoices(engine)
	if len(builtin) == 0 {
		// 没有内置候选的引擎不给模型子菜单（本轮只有 siliconflow 支持）。
		return nil
	}
	seen := make(map[string]bool)
	out := make([]modelChoice, 0, len(builtin)+2)
	add := func(id, label string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, modelChoice{ID: id, Label: label})
	}
	for _, c := range builtin {
		add(c.ID, c.Label)
	}
	for _, id := range cachedModels(engine, time.Now()) {
		add(id, "")
	}
	add(current, "")
	return out
}

// applyEngineModel 把 model 写进 cfg 里该引擎的私有配置，并把当前引擎切到它。
// 返回是否发生了变更。
//
// 会先复制一份配置 map 再改：EngineConfigFor 可能直接返回 engine_configs 里的
// 共享 map，原地写会污染调用方（引擎插件可能正在并发读同一份配置）。旧版
// 平铺 engine_config 作为回退来源时，复制出来的内容会迁移进该引擎的分组存储，
// 平铺 map 本身保持原样（其它引擎的分组配置也一律不碰）。
func applyEngineModel(cfg *config.Config, engine, model string) bool {
	if cfg == nil {
		return false
	}
	engine = strings.TrimSpace(engine)
	model = strings.TrimSpace(model)
	if engine == "" || model == "" {
		return false
	}
	changed := false
	if cfg.Voice.Engine != engine {
		cfg.Voice.Engine = engine
		changed = true
	}
	per := make(map[string]string, 4)
	for k, v := range cfg.Voice.EngineConfigFor(engine) {
		per[k] = v
	}
	if per["model"] != model {
		per["model"] = model
		changed = true
	}
	if cfg.Voice.SetEngineConfig(engine, per) {
		changed = true
	}
	return changed
}

// ---- 缓存 ----

// modelsCachePathFn 返回模型缓存文件路径；测试里替换成 t.TempDir() 下的路径，
// 绝不写到真实用户家目录。
var modelsCachePathFn = defaultModelsCachePath

// defaultModelsCachePath 缓存落在 {用户缓存目录}/just-talk-plus/models-cache.json。
func defaultModelsCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "just-talk-plus", "models-cache.json")
}

// modelsCache 缓存文件形态：{"siliconflow":{"fetched_at":"RFC3339","models":["..."]}}。
type modelsCache map[string]modelsCacheEntry

type modelsCacheEntry struct {
	FetchedAt time.Time `json:"fetched_at"`
	Models    []string  `json:"models"`
}

// readModelsCache 读一份引擎的缓存条目；任何错误都当作没有缓存。
func readModelsCache(engine string) (modelsCacheEntry, bool) {
	path := modelsCachePathFn()
	if path == "" {
		return modelsCacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return modelsCacheEntry{}, false
	}
	var all modelsCache
	if err := json.Unmarshal(data, &all); err != nil {
		return modelsCacheEntry{}, false
	}
	entry, ok := all[engine]
	if !ok {
		return modelsCacheEntry{}, false
	}
	return entry, true
}

// cachedModels 返回未过期可用的缓存模型列表；缺失 / 过期 / 损坏 / 空列表
// 都返回 nil，由调用方回落到内置兜底。
func cachedModels(engine string, now time.Time) []string {
	entry, ok := readModelsCache(engine)
	if !ok || entry.FetchedAt.IsZero() {
		return nil
	}
	if now.Sub(entry.FetchedAt) > modelsCacheTTL {
		return nil
	}
	models := cleanModelIDs(entry.Models)
	if len(models) == 0 {
		return nil
	}
	return models
}

// writeModelsCache 写入一个引擎的模型列表，保留文件里其它引擎的条目。
// 先写临时文件再 rename，避免半截文件把缓存弄坏。
func writeModelsCache(engine string, models []string, now time.Time) error {
	path := modelsCachePathFn()
	if path == "" {
		return fmt.Errorf("无法定位用户缓存目录")
	}
	all := modelsCache{}
	if data, err := os.ReadFile(path); err == nil {
		// 旧文件损坏就重写一份，不因为读失败挡住写。
		if err := json.Unmarshal(data, &all); err != nil || all == nil {
			all = modelsCache{}
		}
	}
	all[engine] = modelsCacheEntry{FetchedAt: now.UTC(), Models: cleanModelIDs(models)}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- 拉取 ----

// fetchModels 拉取硅基流动的语音转写模型列表：
// GET {base_url}/models?sub_type=speech-to-text，Bearer 鉴权，返回 data[].id。
// 文档：https://docs.siliconflow.cn/docs/api/models-get
func fetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = siliconflowDefaultBaseURL
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, fmt.Errorf("%s: 未配置 api_key，无法拉取模型列表", siliconflowEngine)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models?sub_type=speech-to-text", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s models: %w", siliconflowEngine, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s models %s", siliconflowEngine, formatModelsHTTPError(resp.StatusCode, data))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s models json: %w", siliconflowEngine, err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	models := cleanModelIDs(ids)
	if len(models) == 0 {
		return nil, fmt.Errorf("%s models: 接口没有返回语音转写模型", siliconflowEngine)
	}
	return models, nil
}

// modelsErrBody 错误响应体形态与转写接口一致：{"code":...,"message":...}。
type modelsErrBody struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

// formatModelsHTTPError 拼可读错误：优先取响应 JSON 的 message（附 code），
// 解析不出时回退原始 body 截断。
func formatModelsHTTPError(statusCode int, data []byte) string {
	var e modelsErrBody
	if err := json.Unmarshal(data, &e); err == nil {
		if msg := strings.TrimSpace(e.Message); msg != "" {
			if code := strings.TrimSpace(fmt.Sprint(e.Code)); code != "" && code != "<nil>" {
				return fmt.Sprintf("HTTP %d: %s (code=%s)", statusCode, msg, code)
			}
			return fmt.Sprintf("HTTP %d: %s", statusCode, msg)
		}
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return fmt.Sprintf("HTTP %d: 空响应", statusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", statusCode, truncate(raw, 300))
}

// cleanModelIDs 去掉空白与空项、去重并排序，保证菜单顺序稳定。
func cleanModelIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
