// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package tui

import (
	"strings"
	"testing"

	"github.com/c/just-talk-go/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func fieldKeys(fields []field) map[string]bool {
	keys := make(map[string]bool, len(fields))
	for _, f := range fields {
		keys[f.key] = true
	}
	return keys
}

func fieldByKey(fields []field, key string) *field {
	for i := range fields {
		if fields[i].key == key {
			return &fields[i]
		}
	}
	return nil
}

func TestBuildFieldsDoubao(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "doubao-stream"
	cfg.Voice.AppKey = "legacy-app"
	fields := buildFields(cfg)

	keys := fieldKeys(fields)
	// 主表单只保留公共字段 + 渠道配置入口
	for _, want := range []string{"enabled", "engine", "language", "channel_config", "push_to_talk", "auto_submit", "stop_delay_ms"} {
		if !keys[want] {
			t.Fatalf("missing field %q in %v", want, keys)
		}
	}
	for _, hidden := range []string{"ec.model_version", "ec.stream_mode", "ec.api_key", "ec.app_id", "ec.access_key", "ec.base_host", "hotwords"} {
		if keys[hidden] {
			t.Fatalf("channel-specific field %q should live in the overlay: %v", hidden, keys)
		}
	}
	// 渠道配置入口是 action 类型
	if f := fieldByKey(fields, "channel_config"); f.fType != fAction {
		t.Fatalf("channel_config fType = %v, want fAction", f.fType)
	}
}

func TestChannelConfigFieldsDoubao(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "doubao-stream"
	cfg.Voice.AppKey = "legacy-app"
	fields := channelConfigFields(cfg.Voice, testTI)

	keys := fieldKeys(fields)
	// 弹层包含全部渠道专属设置：模型版本、流式模式、凭据、热词
	for _, want := range []string{"ec.model_version", "ec.stream_mode", "ec.api_key", "ec.app_id", "ec.access_key", "ec.base_host", "hotwords"} {
		if !keys[want] {
			t.Fatalf("missing overlay field %q in %v", want, keys)
		}
	}
	// 旧版平铺 app_key 迁移显示到 ec.app_id
	if f := fieldByKey(fields, "ec.app_id"); f.input.Value() != "legacy-app" {
		t.Fatalf("app_id = %q, want legacy-app", f.input.Value())
	}
	// 模型版本默认 1.0（bigasr）
	if f := fieldByKey(fields, "ec.model_version"); f.selectValue() != "1.0" {
		t.Fatalf("model_version = %q, want 1.0", f.selectValue())
	}
}

func TestBuildFieldsOpenai(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "openai"
	cfg.Voice.EngineConfig = map[string]string{"api_key": "sk-1", "model": "whisper-small"}
	fields := buildFields(cfg)

	keys := fieldKeys(fields)
	// 主表单统一：无任何渠道专属字段
	for _, hidden := range []string{"ec.model_version", "ec.model", "ec.api_key", "ec.base_url", "hotwords"} {
		if keys[hidden] {
			t.Fatalf("channel-specific field %q should live in the overlay: %v", hidden, keys)
		}
	}
	if !keys["channel_config"] {
		t.Fatalf("main form should have channel_config entry: %v", keys)
	}
	// 弹层含模型设置
	ov := channelConfigFields(cfg.Voice, testTI)
	if f := fieldByKey(ov, "ec.model"); f.input.Value() != "whisper-small" {
		t.Fatalf("model = %q", f.input.Value())
	}
	if f := fieldByKey(ov, "ec.api_key"); f.input.Value() != "sk-1" {
		t.Fatalf("api_key = %q", f.input.Value())
	}
}

func TestSwitchEnginePreservesValues(t *testing.T) {
	cfg := config.Default() // engine = doubao-stream
	m := New(cfg)
	for i := range m.fields {
		if m.fields[i].key == "push_to_talk" {
			m.fields[i].input.SetValue("F9")
		}
	}
	m.switchEngine("openai")

	if f := fieldByKey(m.fields, "engine"); f.selectValue() != "openai" {
		t.Fatalf("engine = %q, want openai", f.selectValue())
	}
	if f := fieldByKey(m.fields, "push_to_talk"); f.input.Value() != "F9" {
		t.Fatalf("hotkey lost: %q", f.input.Value())
	}
	// 渠道配置入口始终存在
	if fieldByKey(m.fields, "channel_config") == nil {
		t.Fatal("channel_config entry should exist for openai")
	}
}

func TestSwitchEngineBackToDoubaoKeepsModelVersion(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.EngineConfig = map[string]string{"model_version": "2.0"}
	if f := fieldByKey(channelConfigFields(cfg.Voice, testTI), "ec.model_version"); f.selectValue() != "2.0" {
		t.Fatalf("model_version = %q, want 2.0", f.selectValue())
	}
}

func testTI(v string) textinput.Model {
	ti := textinput.New()
	ti.SetValue(v)
	return ti
}

func TestChannelConfigFieldsAuralwise(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "auralwise"
	cfg.Voice.EngineConfig = map[string]string{"api_key": "asr_x", "optimize": "true"}
	// 主表单无渠道专属字段
	keys := fieldKeys(buildFields(cfg))
	if keys["ec.api_key"] || keys["ec.base_url"] || keys["ec.optimize"] {
		t.Fatalf("auralwise settings should live in the overlay: %v", keys)
	}
	// 弹层字段
	ov := channelConfigFields(cfg.Voice, testTI)
	ovKeys := fieldKeys(ov)
	for _, want := range []string{"ec.api_key", "ec.base_url", "ec.optimize"} {
		if !ovKeys[want] {
			t.Fatalf("missing overlay field %q in %v", want, ovKeys)
		}
	}
	if f := fieldByKey(ov, "ec.optimize"); f.selectValue() != "true" {
		t.Fatalf("optimize = %q, want true", f.selectValue())
	}
}

func TestEngineChannelsDescriptions(t *testing.T) {
	byName := map[string]pickerItem{}
	for _, it := range engineChannels {
		byName[it.name] = it
	}
	for _, name := range []string{"doubao-stream", "auralwise", "openai"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing channel %q", name)
		}
	}
	// doubao-stream：提示选定后再选择 1.0 / 2.0 模型版本
	if !strings.Contains(byName["doubao-stream"].subtitle, "1.0") || !strings.Contains(byName["doubao-stream"].subtitle, "2.0") {
		t.Fatalf("doubao-stream subtitle should mention 1.0/2.0 model version: %q", byName["doubao-stream"].subtitle)
	}
	// openai：给出来源方向链接
	if !strings.Contains(byName["openai"].subtitle, "https://www.qianwenai.com/models?search=asr") {
		t.Fatalf("openai subtitle should include qianwenai link: %q", byName["openai"].subtitle)
	}
}

func TestEnginePickerNavigationAndConfirm(t *testing.T) {
	cfg := config.Default() // engine = doubao-stream
	m := New(cfg)
	m.w, m.h = 100, 40

	// 打开弹层，光标定位到当前引擎 doubao-stream
	m.openEnginePicker()
	if m.picker == nil || m.picker.idx != 0 {
		t.Fatalf("picker = %+v", m.picker)
	}

	// 导航到 openai 并确认
	m.handlePickerKey("down")
	m.handlePickerKey("down")
	if got := m.picker.items[m.picker.idx].name; got != "openai" {
		t.Fatalf("picker idx -> %q", got)
	}
	m.handlePickerKey("enter")
	if m.picker != nil {
		t.Fatal("picker should close after confirm")
	}
	for _, f := range m.fields {
		if f.key == "engine" && f.selectValue() != "openai" {
			t.Fatalf("engine = %q, want openai", f.selectValue())
		}
	}
	// 主表单统一重建，渠道配置入口仍在
	if fieldByKey(m.fields, "channel_config") == nil {
		t.Fatal("channel_config entry should survive engine switch")
	}
}

func TestEnginePickerEscCancels(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	m.openEnginePicker()
	m.handlePickerKey("esc")
	if m.picker != nil {
		t.Fatal("picker should close on esc")
	}
	for _, f := range m.fields {
		if f.key == "engine" && f.selectValue() != "doubao-stream" {
			t.Fatalf("engine changed after cancel: %q", f.selectValue())
		}
	}
}

func TestEnterOnEngineFieldOpensPicker(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	for i, f := range m.fields {
		if f.key == "engine" {
			m.cursor = i
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.picker == nil {
		t.Fatal("enter on engine field should open picker")
	}
	if m.editing {
		t.Fatal("should not enter field editing for engine")
	}
	// 弹层打开时其他按键全部被拦截
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.picker == nil {
		t.Fatal("picker should stay open while active")
	}
}

func TestRenderPicker(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	m.w, m.h = 100, 40
	m.openEnginePicker()
	out := m.renderPicker()
	if out == "" {
		t.Fatal("empty picker render")
	}
	for _, want := range []string{"doubao-stream", "auralwise", "openai", "qianwenai.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("picker render missing %q:\n%s", want, out)
		}
	}
	// doubao-stream 的 subtitle 提到 1.0 / 2.0 模型版本
	if !strings.Contains(out, "1.0") || !strings.Contains(out, "2.0") {
		t.Fatalf("picker render should mention doubao model versions:\n%s", out)
	}
}

func TestChannelConfigOverlayOpen(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "openai"
	m := New(cfg)
	m.openChannelConfig()
	if m.configOverlay == nil {
		t.Fatal("overlay should open")
	}
	if m.configOverlay.title != "[openai] 渠道配置" {
		t.Fatalf("title = %q", m.configOverlay.title)
	}
	if fieldByKey(m.configOverlay.fields, "ec.api_key") == nil || fieldByKey(m.configOverlay.fields, "ec.base_url") == nil {
		t.Fatalf("overlay fields missing: %v", fieldKeys(m.configOverlay.fields))
	}
}

func TestChannelConfigOverlayApply(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "openai"
	m := New(cfg)
	m.openChannelConfig()
	// 填 API Key 与 Base URL
	if f := fieldByKey(m.configOverlay.fields, "ec.api_key"); f != nil {
		f.input.SetValue("sk-123")
	}
	if f := fieldByKey(m.configOverlay.fields, "ec.base_url"); f != nil {
		f.input.SetValue("http://127.0.0.1:8000/v1")
	}
	m.applyConfigOverlay()
	if m.configOverlay != nil {
		t.Fatal("overlay should close after apply")
	}
	if got := cfg.Voice.EngineConfig["api_key"]; got != "sk-123" {
		t.Fatalf("api_key = %q", got)
	}
	if got := cfg.Voice.EngineConfig["base_url"]; got != "http://127.0.0.1:8000/v1" {
		t.Fatalf("base_url = %q", got)
	}
}

func TestChannelConfigOverlayKeys(t *testing.T) {
	cfg := config.Default() // doubao-stream
	m := New(cfg)
	m.openChannelConfig()
	ov := m.configOverlay

	// j 导航到下一字段
	m.handleConfigOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
	if ov.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", ov.cursor)
	}
	// enter 编辑字段
	m.handleConfigOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !ov.editing {
		t.Fatal("should enter editing")
	}
	// esc 退出编辑
	m.handleConfigOverlayKey(tea.KeyMsg{Type: tea.KeyEsc})
	if ov.editing {
		t.Fatal("should exit editing")
	}
	// esc 取消关闭（不保存）
	m.handleConfigOverlayKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.configOverlay != nil {
		t.Fatal("esc should close overlay")
	}
	if len(cfg.Voice.EngineConfig) != 0 {
		t.Fatalf("cancel must not write EngineConfig: %v", cfg.Voice.EngineConfig)
	}
}

func TestEnterOnChannelConfigOpensOverlay(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	for i, f := range m.fields {
		if f.key == "channel_config" {
			m.cursor = i
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.configOverlay == nil {
		t.Fatal("enter on channel_config should open overlay")
	}
	if m.picker != nil {
		t.Fatal("should not open the engine picker")
	}
}

func TestRenderConfigOverlay(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "doubao-stream"
	cfg.Voice.AppKey = "legacy-app"
	m := New(cfg)
	m.w, m.h = 100, 40
	m.openChannelConfig()
	out := m.renderConfigOverlay()
	if out == "" {
		t.Fatal("empty overlay render")
	}
	for _, want := range []string{"[doubao-stream] 渠道配置", "API Key(新版)", "App ID(旧版)", "Access Key(旧版)", "Base Host"} {
		if !strings.Contains(out, want) {
			t.Fatalf("overlay render missing %q:\n%s", want, out)
		}
	}
}

func TestChannelConfigOverlayApplyDoubao(t *testing.T) {
	cfg := config.Default() // doubao-stream
	m := New(cfg)
	m.openChannelConfig()
	// 模型版本 2.0 + 热词 + API Key
	if f := fieldByKey(m.configOverlay.fields, "ec.model_version"); f != nil {
		f.optIdx = 1 // 2.0 (seedasr)
	}
	if f := fieldByKey(m.configOverlay.fields, "hotwords"); f != nil {
		f.input.SetValue("Wayland, Sway")
	}
	if f := fieldByKey(m.configOverlay.fields, "ec.api_key"); f != nil {
		f.input.SetValue("volc-key")
	}
	m.applyConfigOverlay()
	if m.configOverlay != nil {
		t.Fatal("overlay should close after apply")
	}
	if got := cfg.Voice.EngineConfig["model_version"]; got != "2.0" {
		t.Fatalf("model_version = %q", got)
	}
	if got := cfg.Voice.EngineConfig["api_key"]; got != "volc-key" {
		t.Fatalf("api_key = %q", got)
	}
	if got := cfg.Voice.Hotwords; len(got) != 2 || got[0] != "Wayland" || got[1] != "Sway" {
		t.Fatalf("hotwords = %v", got)
	}
}

func TestChannelConfigFollowsEngineSwitch(t *testing.T) {
	cfg := config.Default() // doubao-stream
	m := New(cfg)
	// 通过渠道选择切到 openai（模拟真实操作路径）
	m.openEnginePicker()
	for i, it := range engineChannels {
		if it.name == "openai" {
			m.picker.idx = i
			break
		}
	}
	m.handlePickerKey("enter")
	if got := m.currentEngine(); got != "openai" {
		t.Fatalf("engine = %q, want openai", got)
	}
	// 打开渠道配置：必须是 openai 的字段，不能残留 doubao 的
	m.openChannelConfig()
	if m.configOverlay == nil {
		t.Fatal("overlay should open")
	}
	if m.configOverlay.title != "[openai] 渠道配置" {
		t.Fatalf("title = %q", m.configOverlay.title)
	}
	keys := fieldKeys(m.configOverlay.fields)
	if !keys["ec.model"] {
		t.Fatalf("openai overlay should show model field: %v", keys)
	}
	if keys["ec.model_version"] || keys["ec.stream_mode"] || keys["hotwords"] {
		t.Fatalf("doubao fields must not leak into openai overlay: %v", keys)
	}
}

func TestChannelConfigEntryBelowEngineField(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	var engineIdx, configIdx int
	for i, f := range m.fields {
		switch f.key {
		case "engine":
			engineIdx = i
		case "channel_config":
			configIdx = i
		}
	}
	// 渠道配置入口紧跟引擎字段之后
	if configIdx != engineIdx+1 {
		t.Fatalf("channel_config at %d, engine at %d; want immediately after", configIdx, engineIdx)
	}
}
