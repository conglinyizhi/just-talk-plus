// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

// Package asrcfg 把 config.VoiceConfig 翻译成语音引擎配置，并负责引擎注册。
// voice 插件与 doctor 共用，避免两边各自拼配置。
package asrcfg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/internal/speech"

	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/auralwise"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/dashscope"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/deepseek"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/doubao"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/mimo"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/mosi"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/openai"
	_ "github.com/conglinyizhi/just-talk-plus/internal/speech/siliconflow"
)

// DefaultEngine 未配置 engine 时使用的引擎（保持旧版行为）。
const DefaultEngine = "doubao-stream"

// userEngines 面向用户的引擎类型，与 README / doctor 文案保持一致。
// null 是内部兜底（Build 空类型 / 显式 "null" 时返回），不列在候选里。
var userEngines = []string{"doubao-stream", "auralwise", "openai", "dashscope", "mimo-asr", "siliconflow", "mosi", "deepseek"}

// Types 面向用户的引擎类型（排序），供 TUI/设置页选择。
func Types() []string {
	out := make([]string, len(userEngines))
	copy(out, userEngines)
	sort.Strings(out)
	return out
}

// EngineType 返回引擎类型；空 → doubao（向后兼容）。
func EngineType(vc config.VoiceConfig) string {
	typ := strings.TrimSpace(vc.Engine)
	if typ == "" {
		return DefaultEngine
	}
	return typ
}

// ConfigMap 把 VoiceConfig 转成引擎配置 map：
// 按引擎分组存储 engine_configs[engine] 优先，其次旧版平铺字段（doubao app_key 等）。
// 语言（language）只来自当前引擎的分组配置——它是引擎相关的设置，
// 旧版全局 [voice].language 由 config 迁移进分组，这里不再注入。
func ConfigMap(vc config.VoiceConfig) map[string]string {
	typ := EngineType(vc)
	per := vc.EngineConfigFor(typ)
	m := make(map[string]string, len(per)+6)
	for k, v := range per {
		m[k] = v
	}
	if typ == "doubao-stream" {
		// 新版 api_key 时不合并旧版平铺字段：顶层 resource_id 会钉死 1.0 的 bigasr 资源，
		// 导致新版 key（通常绑定 2.0/seedasr）握手 403。旧版凭据仍按旧行为合并。
		if m["api_key"] != "" {
			return m
		}
		if m["app_id"] == "" && strings.TrimSpace(vc.AppKey) != "" {
			m["app_id"] = vc.AppKey
		}
		if m["access_key"] == "" && strings.TrimSpace(vc.AccessKey) != "" {
			m["access_key"] = vc.AccessKey
		}
		if m["resource_id"] == "" && strings.TrimSpace(vc.ResourceID) != "" {
			m["resource_id"] = vc.ResourceID
		}
	}
	return m
}

// Build 按配置构造引擎；未注册类型或配置非法时返回可读错误。
func Build(vc config.VoiceConfig) (speech.Engine, error) {
	typ := EngineType(vc)
	if typ == "null" {
		return speech.Null{}, nil
	}
	eng, err := speech.Build(typ, ConfigMap(vc))
	if err != nil {
		return nil, fmt.Errorf("语音引擎 %q 配置错误: %w", typ, err)
	}
	return eng, nil
}

// StreamParams 从当前引擎的私有配置解析会话参数：
//   - Lang 取分组里的 language（doubao-stream 可回退旧版平铺 engine_config）
//   - Hotwords 取分组里的 hotwords，逗号分隔，去空白、丢空项
//
// 语言与热词都按引擎隔离存放，voice 插件不自己解析这些字符串。
func StreamParams(vc config.VoiceConfig) speech.StreamParams {
	ec := vc.EngineConfigFor(EngineType(vc))
	return speech.StreamParams{
		Lang:     strings.TrimSpace(ec["language"]),
		Hotwords: splitHotwords(ec["hotwords"]),
	}
}

// splitHotwords 解析逗号分隔的热词字符串（中英文逗号、换行都算分隔符）；
// 去空白、丢空项、保序去重。
func splitHotwords(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}
