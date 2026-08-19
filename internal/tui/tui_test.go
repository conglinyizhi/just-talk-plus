// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package tui

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

// ansiRe 匹配 ANSI SGR 转义序列（lipgloss 输出的着色码）。
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI 去除 ANSI 着色码，便于断言电平条的实际字符。
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// fieldByKey 在全部 tab 中按 key 查找字段。
func fieldByKey(m *Model, key string) *field {
	for _, fs := range m.tabField {
		for i := range fs {
			if fs[i].key == key {
				return &fs[i]
			}
		}
	}
	return nil
}

// fieldIndex 返回指定 key 在所在 tab 字段列表中的下标（按当前 tab 查找）。
func fieldIndex(m *Model, key string) int {
	fs := m.tabField[m.tab]
	for i := range fs {
		if fs[i].key == key {
			return i
		}
	}
	return 0
}

func tabKeys(fs []field) map[string]bool {
	keys := make(map[string]bool, len(fs))
	for _, f := range fs {
		keys[f.key] = true
	}
	return keys
}

func readyModel(cfg *config.Config) *Model {
	m := New(cfg)
	m.w, m.h, m.ready = 100, 40, true
	return m
}

// ---- 字段分组 ----

func TestBuildTabFields(t *testing.T) {
	m := New(config.Default())

	general := tabKeys(m.tabField[tabGeneral])
	for _, want := range []string{"enabled", "push_to_talk", "mode", "language", "stop_delay_ms", "stop_after_silence", "diagnostics_startup_runs"} {
		if !general[want] {
			t.Fatalf("general missing %q: %v", want, general)
		}
	}
	if general["auto_submit"] {
		t.Fatal("auto_submit moved to Output tab (上屏与复制行为)")
	}
	engineTab := tabKeys(m.tabField[tabEngine])
	if !engineTab["engine"] || !engineTab["engine_config"] || !engineTab["doc_links"] {
		t.Fatalf("engine tab: %v", engineTab)
	}
	mic := tabKeys(m.tabField[tabMic])
	for _, want := range []string{"mic_test", "auto_gain", "noise_threshold", "noise_floor_auto", "noise_deadband", "diagnose"} {
		if !mic[want] {
			t.Fatalf("mic tab missing %q: %v", want, mic)
		}
	}
	for _, gone := range []string{"gain", "device"} {
		if mic[gone] {
			t.Fatalf("mic tab should not have %q (use the debug panel)", gone)
		}
	}
	output := tabKeys(m.tabField[tabOutput])
	for _, want := range []string{"omit_trailing_punct", "omit_trailing_all", "omit_trailing_ascii", "remove_cjk_space", "auto_space", "auto_submit", "write_clipboard", "restore_clipboard", "no_clipboard_on_fail"} {
		if !output[want] {
			t.Fatalf("output tab missing %q: %v", want, output)
		}
	}
	if f := fieldByKey(m, "omit_trailing_all"); f.indent != 1 {
		t.Fatalf("omit_trailing_all indent = %d", f.indent)
	}
	// 上屏与复制行为：自动上屏为主项，其余三个为其子项（缩进 1）
	if f := fieldByKey(m, "auto_submit"); f.indent != 0 {
		t.Fatalf("auto_submit indent = %d, want 0", f.indent)
	}
	for _, key := range []string{"write_clipboard", "restore_clipboard", "no_clipboard_on_fail"} {
		if f := fieldByKey(m, key); f.indent != 1 {
			t.Fatalf("%s indent = %d, want 1 (sub of 自动上屏)", key, f.indent)
		}
	}
	// 自适应增益子项：4 个参数均为缩进项（indent 1）
	for _, key := range []string{"auto_gain_target_peak", "auto_gain_max_peak", "auto_gain_max_rise", "auto_gain_max_gain"} {
		f := fieldByKey(m, key)
		if f == nil {
			t.Fatalf("mic tab missing AGC param %q", key)
		}
		if f.indent != 1 {
			t.Fatalf("%s indent = %d, want 1 (sub of 自适应增益)", key, f.indent)
		}
	}
	// 显示 tab（overlay）
	overlay := tabKeys(m.tabField[tabOverlay])
	for _, want := range []string{"overlay_enabled", "overlay_position", "overlay_idle_visible", "overlay_scale"} {
		if !overlay[want] {
			t.Fatalf("overlay tab missing %q: %v", want, overlay)
		}
	}
	// 通用 tab 的调试项
	for _, want := range []string{"debug_enabled", "debug_hotkeys"} {
		if !general[want] {
			t.Fatalf("general missing %q: %v", want, general)
		}
	}
}

// ---- tab 切换 ----

func TestSwitchTab(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabMic)
	if m.tab != tabMic {
		t.Fatalf("tab = %d", m.tab)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.tab != tabOutput {
		t.Fatalf("digit tab = %d, want tabOutput", m.tab)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.tab != tabMic {
		t.Fatalf("left = %d, want tabMic", m.tab)
	}
}

// vim 规范：h 左 / l 右；方向键 ←/→ 等效（j/k/↑/↓ 见字段导航测试）
func TestVimHLSwitchTab(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabGeneral)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if m.tab != tabEngine {
		t.Fatalf("l = %d, want tabEngine", m.tab)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if m.tab != tabGeneral {
		t.Fatalf("h = %d, want tabGeneral", m.tab)
	}
	// 方向键等效
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.tab != tabEngine {
		t.Fatalf("right = %d, want tabEngine", m.tab)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.tab != tabGeneral {
		t.Fatalf("left = %d, want tabGeneral", m.tab)
	}
}

func TestVimJLFieldNav(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabGeneral)
	// 默认光标在第 0 行；j/↓ 下移，k/↑ 上移
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.cursor != 1 {
		t.Fatalf("j cursor = %d, want 1", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("down cursor = %d, want 2", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if m.cursor != 1 {
		t.Fatalf("k cursor = %d, want 1", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("up cursor = %d, want 0", m.cursor)
	}
}

func TestHelpLineShowsVimAndArrows(t *testing.T) {
	m := readyModel(config.Default())
	line := m.helpLine()
	for _, want := range []string{"j/k", "↑/↓", "h/l", "←/→", "q 退出"} {
		if !strings.Contains(line, want) {
			t.Fatalf("help line missing %q: %q", want, line)
		}
	}
}

func TestTabBarKeepsDigitOnActive(t *testing.T) {
	m := readyModel(config.Default())
	out := m.tabBar()
	for _, want := range []string{"1 通用", "2 引擎", "3 输入", "4 输出", "5 历史"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tab bar missing %q: %q", want, out)
		}
	}
}

// ---- 字段导航与编辑 ----

func TestFieldNavigation(t *testing.T) {
	m := readyModel(config.Default())
	if m.cursor != 0 {
		t.Fatalf("cursor = %d", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor = %d", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor = %d", m.cursor)
	}
}

func TestFieldEditingString(t *testing.T) {
	m := readyModel(config.Default())
	// 定位到语言字段（通用 tab 第 4 个）
	for m.cursor < 3 {
		m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing {
		t.Fatal("should enter editing")
	}
	lang := fieldByKey(m, "language")
	lang.input.SetValue("")
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Fatal("should exit editing")
	}
	if got := lang.input.Value(); got != "en" {
		t.Fatalf("language = %q", got)
	}
}

func TestFieldEditingToggle(t *testing.T) {
	m := readyModel(config.Default())
	// auto_submit 在输出 tab（上屏与复制行为组）
	m.switchTab(tabOutput)
	m.cursor = fieldIndex(m, "auto_submit")
	f := fieldByKey(m, "auto_submit")
	before := f.boolVal
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if f.boolVal == before {
		t.Fatal("toggle should flip on enter")
	}
}

func TestFieldEditingSelect(t *testing.T) {
	m := readyModel(config.Default())
	// 模式是第 3 个字段
	for m.cursor < 2 {
		m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	f := fieldByKey(m, "mode")
	before := f.optIdx
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing {
		t.Fatal("select should enter editing")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if f.optIdx == before {
		t.Fatal("select should cycle on down")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Fatal("enter should exit select editing")
	}
}

// ---- fAction 触发 ----

func TestTriggerAction(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabEngine)
	fs := m.tabField[tabEngine]
	for i := range fs {
		m.cursor = i
		m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		switch fs[i].key {
		case "engine":
			if m.picker == nil {
				t.Fatal("engine enter should open picker")
			}
			m.picker = nil
		case "engine_config":
			if m.configOverlay == nil {
				t.Fatal("engine_config enter should open overlay")
			}
			m.configOverlay = nil
		case "doc_links":
			if m.linkPicker == nil {
				t.Fatal("doc_links enter should open link picker")
			}
			m.linkPicker = nil
		}
	}
	m.switchTab(tabMic)
	// mic_test 不再是输入 tab 第一个字段（前面有 Android 虚拟麦克风），按 key 定位。
	micIdx := -1
	for i, f := range m.tabField[tabMic] {
		if f.key == "mic_test" {
			micIdx = i
			break
		}
	}
	if micIdx < 0 {
		t.Fatal("mic_test field not found")
	}
	m.cursor = micIdx
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.micPanel == nil {
		t.Fatal("mic_test enter should open panel")
	}
	voice.StopMicTest()
	m.micPanel = nil
}

// ---- 历史 tab ----

func TestHistoryTab(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabHistory)
	if m.tab != tabHistory {
		t.Fatal("should switch to history")
	}
	// 空历史 enter 不 panic
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	// 左键回退到输出
	m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.tab != tabOutput {
		t.Fatalf("left from history = %d, want tabOutput", m.tab)
	}
	// 右键到显示 tab，再右回绕到通用
	m.switchTab(tabHistory)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.tab != tabOverlay {
		t.Fatalf("right from history = %d, want tabOverlay", m.tab)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.tab != tabGeneral {
		t.Fatalf("right wrap from overlay = %d, want tabGeneral", m.tab)
	}
}

func TestHistoryEmptyHint(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabHistory)
	out := m.View()
	for _, want := range []string{"暂无识别结果", "运行时目录", "系统重启"} {
		if !strings.Contains(out, want) {
			t.Fatalf("history empty hint missing %q:\n%s", want, out)
		}
	}
}

// TestHistoryPageKeys 验证历史列表翻页与首尾跳转按键：
// PgUp/PgDn 翻页（步长 = 当前可见窗口条目数），Home/End 跳首尾。
func TestHistoryPageKeys(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabHistory)
	entries := make([]voice.HistoryEntry, 40)
	for i := range entries {
		entries[i] = voice.HistoryEntry{Text: fmt.Sprintf("entry %d", i)}
	}
	voice.SetHistoryForTest(entries)
	defer voice.SetHistoryForTest(nil)

	// Home → 第一条
	m.cursor = 25
	m.handleKey(tea.KeyMsg{Type: tea.KeyHome})
	if m.cursor != 0 {
		t.Fatalf("home cursor = %d, want 0", m.cursor)
	}
	// End → 最后一条
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if m.cursor != 39 {
		t.Fatalf("end cursor = %d, want 39", m.cursor)
	}
	// PgUp 从末尾回一页：步长为窗口条目数，光标应回到窗口顶部附近
	m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.cursor >= 39 {
		t.Fatalf("pgup cursor = %d, want < 39", m.cursor)
	}
	// PgDn 再翻回底部
	m.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.cursor != 39 {
		t.Fatalf("pgdown cursor = %d, want 39", m.cursor)
	}
	// 顶部 PgUp 钳制到 0
	m.cursor = 2
	m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.cursor != 0 {
		t.Fatalf("pgup clamp cursor = %d, want 0", m.cursor)
	}
	// 底部 PgDn 钳制到末尾
	m.cursor = 38
	m.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.cursor != 39 {
		t.Fatalf("pgdown clamp cursor = %d, want 39", m.cursor)
	}
}

// TestHistoryWindow 验证滚动窗口几何：光标始终可见、窗口行数不超上限、
// 头尾边界与越界钳制。这是长历史列表能正常浏览的基础。
func TestHistoryWindow(t *testing.T) {
	if s, e := historyWindow(nil, 0, 10); s != 0 || e != 0 {
		t.Fatalf("empty: (%d,%d), want (0,0)", s, e)
	}
	mk := func(n int) []voice.HistoryEntry {
		h := make([]voice.HistoryEntry, n)
		for i := range h {
			h[i] = voice.HistoryEntry{Text: fmt.Sprintf("text-%d", i)}
		}
		return h
	}
	hist := mk(50)

	// 光标在头部：窗口从 0 开始
	if s, e := historyWindow(hist, 0, 10); s != 0 || e < 1 || e > 10 {
		t.Fatalf("cursor 0: (%d,%d), want start=0 and end in (0,10]", s, e)
	}
	// 光标在中间：窗口包含光标且条数不超 maxRows
	if s, e := historyWindow(hist, 25, 10); s > 25 || e <= 25 || e-s > 10 {
		t.Fatalf("cursor 25: (%d,%d), must contain cursor within 10 rows", s, e)
	}
	// 光标在尾部：窗口到 end=50
	if s, e := historyWindow(hist, 49, 10); e != 50 || s >= 49 {
		t.Fatalf("cursor 49: (%d,%d), want end=50", s, e)
	}
	// 光标越界被钳制到最后一条
	if s, e := historyWindow(hist, 999, 10); e != 50 || s >= 49 {
		t.Fatalf("cursor 999: (%d,%d), want end=50", s, e)
	}
	// 遍历所有位置：光标必须始终在窗口内
	for c := 0; c < 50; c++ {
		if s, e := historyWindow(hist, c, 8); s > c || e <= c {
			t.Fatalf("cursor %d: window (%d,%d) misses cursor", c, s, e)
		}
	}
	// 带 LogID 的条目占 2 行：窗口按渲染行数（而非条数）限制
	logHist := make([]voice.HistoryEntry, 50)
	for i := range logHist {
		logHist[i] = voice.HistoryEntry{Text: "t"}
		if i%2 == 0 {
			logHist[i].UpstreamLogID = "L"
		}
	}
	s, e := historyWindow(logHist, 25, 10)
	rows := 0
	for i := s; i < e; i++ {
		rows++
		if logHist[i].UpstreamLogID != "" {
			rows++
		}
	}
	if rows > 10 || s > 25 || e <= 25 {
		t.Fatalf("logID cursor 25: window (%d,%d) rows=%d, want <=10 with cursor", s, e, rows)
	}
}

// TestHistoryMenuShowsIndex 验证二级菜单显示序号与时间戳，
// 让操作对象与列表条目可对照（长列表下不迷路）。
func TestHistoryMenuShowsIndex(t *testing.T) {
	m := readyModel(config.Default())
	entry := voice.HistoryEntry{Text: "hello world", Time: time.Date(2026, 1, 2, 15, 4, 5, 0, time.Local), RecordingPath: "/tmp/a.wav"}
	m.openHistoryMenu(36, entry)
	out := stripANSI(m.renderHistoryMenu())
	for _, want := range []string{"#37", "15:04:05", "hello world", "复制识别文本", "播放录音", "文件管理器"} {
		if !strings.Contains(out, want) {
			t.Fatalf("menu missing %q:\n%s", want, out)
		}
	}
}

// ---- 渠道选择弹层 ----

func TestEnginePickerFlow(t *testing.T) {
	cfg := config.Default()
	m := readyModel(cfg)
	m.openEnginePicker()
	if m.picker == nil {
		t.Fatal("picker should open")
	}
	m.handlePickerKey("down")
	m.handlePickerKey("down")
	name := m.picker.items[m.picker.idx].name
	m.handlePickerKey("enter")
	if m.picker != nil {
		t.Fatal("picker should close")
	}
	if m.cfg.Voice.Engine != name {
		t.Fatalf("engine = %q, want %q", m.cfg.Voice.Engine, name)
	}
}

// ---- 渠道配置弹层 ----

func TestDiagnosticsSettingAppliesStartupRuns(t *testing.T) {
	m := readyModel(config.Default())
	f := fieldByKey(m, "diagnostics_startup_runs")
	f.optIdx = 2 // 后续 3 次启动
	next := *m.cfg
	m.applyFieldsTo(&next)
	if got := next.Diagnostics.StartupRuns; got != 3 {
		t.Fatalf("diagnostics startup runs=%d, want 3", got)
	}

	f.optIdx = 3 // 持续开启
	m.applyFieldsTo(&next)
	if got := next.Diagnostics.StartupRuns; got != config.DiagnosticsAlways {
		t.Fatalf("persistent diagnostics=%d, want %d", got, config.DiagnosticsAlways)
	}
}

func TestConfigOverlayFlow(t *testing.T) {
	cfg := config.Default()
	m := readyModel(cfg)
	m.openChannelConfig()
	if m.configOverlay == nil {
		t.Fatal("overlay should open")
	}
	if m.configOverlay.title != "[doubao-stream] 引擎配置" {
		t.Fatalf("title = %q", m.configOverlay.title)
	}
	ov := m.configOverlay
	for i := range ov.fields {
		if ov.fields[i].key == "ec.model_version" {
			ov.cursor = i
			ov.fields[i].optIdx = 1
			break
		}
	}
	m.applyConfigOverlay()
	if m.configOverlay != nil {
		t.Fatal("overlay should close")
	}
	if got := cfg.Voice.EngineConfigs["doubao-stream"]["model_version"]; got != "2.0" {
		t.Fatalf("model_version = %q", got)
	}
}

func TestConfigOverlaySavesDoubaoTransmissionMode(t *testing.T) {
	cfg := config.Default()
	m := readyModel(cfg)
	m.openChannelConfig()
	ov := m.configOverlay
	if ov == nil {
		t.Fatal("overlay should open")
	}
	found := false
	for i := range ov.fields {
		if ov.fields[i].key != "ec.stream_mode" {
			continue
		}
		found = true
		if got := ov.fields[i].selectValue(); got != "async" {
			t.Fatalf("default stream mode = %q, want async", got)
		}
		ov.fields[i].optIdx = 1
		break
	}
	if !found {
		t.Fatal("stream mode field not found")
	}
	m.applyConfigOverlay()
	if got := cfg.Voice.EngineConfigs["doubao-stream"]["stream_mode"]; got != "nostream" {
		t.Fatalf("stream_mode = %q, want nostream", got)
	}
}

func TestConfigOverlayToggleSave(t *testing.T) {
	cfg := config.Default()
	m := readyModel(cfg)
	m.openChannelConfig()
	ov := m.configOverlay
	if ov == nil {
		t.Fatal("overlay should open")
	}
	// 光标移到 enable_lid 字段
	found := false
	for i := range ov.fields {
		if ov.fields[i].key == "ec.enable_lid" {
			ov.cursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("enable_lid field not present in doubao overlay")
	}
	if ov.fields[ov.cursor].boolVal {
		t.Fatal("enable_lid should default off")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !ov.fields[ov.cursor].boolVal {
		t.Fatal("enter should toggle enable_lid on")
	}
	m.applyConfigOverlay()
	if got := cfg.Voice.EngineConfigs["doubao-stream"]["enable_lid"]; got != "true" {
		t.Fatalf("enable_lid = %q, want true", got)
	}
}

func TestEngineConfigPerEngineIsolation(t *testing.T) {
	// 不同引擎的配置互不覆盖：doubao 的 key 与 openai 的 key 独立保存
	cfg := config.Default()
	m := readyModel(cfg)

	// 配置 doubao
	m.openChannelConfig() // currentEngine = doubao-stream
	m.applyConfigOverlay()
	if cfg.Voice.EngineConfigs["doubao-stream"] == nil {
		t.Fatal("doubao config not initialized")
	}

	// 切到 openai 并保存空配置：doubao 的配置应保持
	cfg.Voice.Engine = "openai"
	m = readyModel(cfg)
	m.openChannelConfig()
	m.applyConfigOverlay()
	if cfg.Voice.EngineConfigs["doubao-stream"] == nil {
		t.Fatal("switching engine lost doubao config")
	}
	if _, ok := cfg.Voice.EngineConfigs["openai"]; !ok {
		t.Fatal("openai config should be initialized independently")
	}
	// 两个 map 是不同对象（写入互不影响）
	cfg.Voice.EngineConfigs["openai"]["api_key"] = "sk-openai"
	cfg.Voice.EngineConfigs["doubao-stream"]["model_version"] = "2.0"
	if cfg.Voice.EngineConfigs["doubao-stream"]["api_key"] == "sk-openai" {
		t.Fatal("writing openai key leaked into doubao config")
	}
	if cfg.Voice.EngineConfigs["openai"]["model_version"] == "2.0" {
		t.Fatal("writing doubao config leaked into openai config")
	}
}

// ---- 麦克风面板 ----

func TestMicPanelKeys(t *testing.T) {
	m := readyModel(config.Default())
	m.micPanel = &micPanelState{}
	voice.StartMicTest()
	voice.SetMicGain(10)
	m.handleMicPanelKey("+")
	if got := voice.MicTestLevel().Gain; got != 12 {
		t.Fatalf("gain=%d, want 12", got)
	}
	m.handleMicPanelKey("-")
	if got := voice.MicTestLevel().Gain; got != 10 {
		t.Fatalf("gain=%d, want 10", got)
	}
	before := voice.MicTestLevel().AutoGain
	m.handleMicPanelKey(" ")
	if got := voice.MicTestLevel().AutoGain; got == before {
		t.Fatal("auto gain toggle failed")
	}
	m.handleMicPanelKey("esc")
	if m.micPanel != nil {
		t.Fatal("panel should close")
	}
	voice.StopMicTest()
}

func TestRenderMicPanel(t *testing.T) {
	m := readyModel(config.Default())
	m.micPanel = &micPanelState{}
	out := m.renderMicPanel()
	if out == "" || !strings.Contains(out, "麦克风调试") {
		t.Fatalf("mic panel render:\n%s", out)
	}
	// 面板显示设备行与切换提示，以及原始和增益后两路真实电平。
	for _, want := range []string{"设备", "[ ]", "真实:", "增益后真实:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("mic panel missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "有效:") {
		t.Fatalf("mic panel still uses the old RMS label:\n%s", out)
	}
}

func TestSwitchPanelDevice(t *testing.T) {
	m := readyModel(config.Default())
	m.devices = []string{"设备A", "设备B"}
	m.micPanel = &micPanelState{devIdx: -1} // 自动
	// ] → 设备A
	m.switchPanelDevice(+1)
	if got := voice.MicTestDevice(); got != "设备A" {
		t.Fatalf("device after ] = %q, want 设备A", got)
	}
	if m.micPanel.devIdx != 0 {
		t.Fatalf("devIdx = %d, want 0", m.micPanel.devIdx)
	}
	// ] → 设备B
	m.switchPanelDevice(+1)
	if got := voice.MicTestDevice(); got != "设备B" {
		t.Fatalf("device after second ] = %q, want 设备B", got)
	}
	// ] → Android 设备（新增档位，位于系统设备之后）
	m.switchPanelDevice(+1)
	if got := voice.MicTestDevice(); got != voice.AndroidMicDeviceName() {
		t.Fatalf("device after third ] = %q, want androidmic", got)
	}
	if !m.micPanel.androidSlot {
		t.Fatal("androidSlot should be true on Android 档")
	}
	// ] → 回绕到自动
	m.switchPanelDevice(+1)
	if got := voice.MicTestDevice(); got != "" {
		t.Fatalf("device after wrap = %q, want auto", got)
	}
	// 无设备时切换不崩溃
	m.devices = nil
	m.switchPanelDevice(+1)
	// 面板键位：] 与 [ 触发切换
	m.devices = []string{"设备A", "设备B"}
	m.micPanel.devIdx = -1
	m.handleMicPanelKey("]")
	if got := voice.MicTestDevice(); got != "设备A" {
		t.Fatalf("panel key ] device = %q, want 设备A", got)
	}
	m.handleMicPanelKey("[")
	if got := voice.MicTestDevice(); got != "" {
		t.Fatalf("panel key [ device = %q, want auto", got)
	}
	voice.SetMicTestDevice("")
}

func TestAndroidPanelStartStop(t *testing.T) {
	m := readyModel(config.Default())
	m.triggerAction("androidmic_panel")
	if !m.androidPanel {
		t.Fatal("androidmic_panel should open panel")
	}
	// 用随机空闲端口启动，避免与用户正在运行的实例（默认 54345）冲突。
	m.cfg.Voice.AndroidMicPort = freePort(t)
	defer func() { _ = voice.AndroidMicStop() }()

	// s 启动
	m.handleAndroidPanelKey("s")
	st := voice.AndroidMicStatus()
	if !st.Running || st.Port <= 0 {
		t.Fatalf("server should be running after s, got %+v", st)
	}
	// 再按 s 幂等
	m.handleAndroidPanelKey("s")
	if !voice.AndroidMicStatus().Running {
		t.Fatal("server should stay running")
	}
	// t 停止
	m.handleAndroidPanelKey("t")
	if voice.AndroidMicStatus().Running {
		t.Fatal("server should be stopped after t")
	}
	// esc 退出
	m.handleAndroidPanelKey("esc")
	if m.androidPanel {
		t.Fatal("esc should close panel")
	}
}

func TestAndroidPanelRender(t *testing.T) {
	m := readyModel(config.Default())
	m.androidPanel = true
	defer func() { _ = voice.AndroidMicStop() }()
	if err := voice.AndroidMicStart(freePort(t)); err != nil {
		t.Fatalf("start server: %v", err)
	}
	out := m.renderAndroidPanel()
	if !strings.Contains(out, "Android 虚拟麦克风") {
		t.Fatalf("panel title missing:\n%s", out)
	}
	if !strings.Contains(out, "监听中") {
		t.Fatalf("panel should show listening state:\n%s", out)
	}
}

// ---- 字段值写回 ----

func TestApplyFieldsTo(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	fieldByKey(m, "language").input.SetValue("ja-JP")
	fieldByKey(m, "auto_submit").boolVal = false
	fieldByKey(m, "omit_trailing_punct").boolVal = true
	fieldByKey(m, "androidmic_auto_start").boolVal = true

	next := *cfg
	m.applyFieldsTo(&next)
	if next.Voice.Language != "ja-JP" {
		t.Fatalf("language = %q", next.Voice.Language)
	}
	if next.Voice.AutoSubmit {
		t.Fatal("auto_submit should be false")
	}
	if !next.Voice.OmitTrailingPunct {
		t.Fatal("omit_trailing_punct should be true")
	}
	if !next.Voice.AndroidMicAutoStart {
		t.Fatal("androidmic_auto_start should be true")
	}
}

func TestApplyAGCParams(t *testing.T) {
	peak := 0.5
	cfg := config.Default()
	cfg.Voice.AutoGainTargetPeak = &peak
	m := New(cfg)

	// 初始显示值来自配置
	if got := fieldByKey(m, "auto_gain_target_peak").input.Value(); got != "0.5" {
		t.Fatalf("field init = %q, want 0.5", got)
	}
	// 默认 nil 时显示为空
	if got := fieldByKey(m, "auto_gain_max_peak").input.Value(); got != "" {
		t.Fatalf("default field = %q, want empty", got)
	}

	// 改值写回
	fieldByKey(m, "auto_gain_target_peak").input.SetValue("0.6")
	next := *cfg
	m.applyFieldsTo(&next)
	if next.Voice.AutoGainTargetPeak == nil || *next.Voice.AutoGainTargetPeak != 0.6 {
		t.Fatalf("target = %v, want 0.6", next.Voice.AutoGainTargetPeak)
	}

	// 清空 → nil（回内置默认）
	fieldByKey(m, "auto_gain_target_peak").input.SetValue("")
	next = *cfg
	m.applyFieldsTo(&next)
	if next.Voice.AutoGainTargetPeak != nil {
		t.Fatalf("empty should reset to nil, got %v", *next.Voice.AutoGainTargetPeak)
	}

	// 非法输入 → 保持原值
	fieldByKey(m, "auto_gain_target_peak").input.SetValue("oops")
	next = *cfg
	m.applyFieldsTo(&next)
	if next.Voice.AutoGainTargetPeak == nil || *next.Voice.AutoGainTargetPeak != peak {
		t.Fatalf("invalid should keep %v, got %v", peak, next.Voice.AutoGainTargetPeak)
	}

	// 整数参数
	fieldByKey(m, "auto_gain_max_gain").input.SetValue("30")
	next = *cfg
	m.applyFieldsTo(&next)
	if next.Voice.AutoGainMaxGain == nil || *next.Voice.AutoGainMaxGain != 30 {
		t.Fatalf("max gain = %v, want 30", next.Voice.AutoGainMaxGain)
	}
}

func TestApplyOverlayFields(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)

	fieldByKey(m, "overlay_enabled").boolVal = false
	fieldByKey(m, "overlay_position").optIdx = idxOf(overlayPositions, "top-right")
	fieldByKey(m, "overlay_idle_visible").boolVal = true
	fieldByKey(m, "overlay_scale").input.SetValue("1.5")
	next := *cfg
	m.applyFieldsTo(&next)
	if next.Overlay.Enabled {
		t.Fatal("overlay should be disabled")
	}
	if next.Overlay.Position != "top-right" {
		t.Fatalf("position = %q, want top-right", next.Overlay.Position)
	}
	if !next.Overlay.IdleVisible {
		t.Fatal("idle_visible should be true")
	}
	if next.Overlay.Scale != 1.5 {
		t.Fatalf("scale = %v, want 1.5", next.Overlay.Scale)
	}
}

func TestApplyDebugFields(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)

	fieldByKey(m, "debug_enabled").boolVal = true
	fieldByKey(m, "debug_hotkeys").input.SetValue("Alt+F8, F10")
	next := *cfg
	m.applyFieldsTo(&next)
	if !next.Debug.Enabled {
		t.Fatal("debug should be enabled")
	}
	if len(next.Debug.Hotkeys) != 2 || next.Debug.Hotkeys[0] != "Alt+F8" || next.Debug.Hotkeys[1] != "F10" {
		t.Fatalf("hotkeys = %v", next.Debug.Hotkeys)
	}
}

func TestApplyNoiseFields(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)

	// 默认容差 0.1 显示在字段里
	if got := fieldByKey(m, "noise_deadband").input.Value(); got != "0.1" {
		t.Fatalf("noise_deadband init = %q, want 0.1", got)
	}
	fieldByKey(m, "noise_floor_auto").boolVal = true
	fieldByKey(m, "noise_deadband").input.SetValue("0.05")
	next := *cfg
	m.applyFieldsTo(&next)
	if !next.Voice.NoiseFloorAuto {
		t.Fatal("noise_floor_auto should be true")
	}
	if next.Voice.NoiseDeadband != 0.05 {
		t.Fatalf("noise_deadband = %v, want 0.05", next.Voice.NoiseDeadband)
	}
}

func TestSyncNoiseCalibrationUpdatesFieldAndRuntime(t *testing.T) {
	m := New(config.Default())
	var reloaded *config.Config
	m.OnSave = func(cfg *config.Config) error {
		reloaded = cfg
		return nil
	}
	m.noiseCalibrationVersion = 7
	m.syncNoiseCalibrationSnapshot(8, 0.0234)

	if got := fieldByKey(m, "noise_threshold").input.Value(); got != "0.0234" {
		t.Fatalf("noise threshold field = %q", got)
	}
	if m.cfg.Voice.NoiseThreshold != 0.0234 {
		t.Fatalf("config noise threshold = %.4f", m.cfg.Voice.NoiseThreshold)
	}
	if reloaded != m.cfg {
		t.Fatal("noise calibration should reload the runtime configuration")
	}
}

// ---- 字段值渲染 ----

func TestFieldValue(t *testing.T) {
	m := readyModel(config.Default())
	// 输出 tab：上屏与复制行为开关（默认 auto_submit 开 → ● 开）
	m.switchTab(tabOutput)
	m.cursor = fieldIndex(m, "auto_submit")
	out := m.renderFields()
	if !strings.Contains(out, "自动上屏:") || !strings.Contains(out, "● 开") {
		t.Fatalf("fields render:\n%s", out)
	}
	// 麦克风 tab：正常渲染（独立「增益」字段已移除，见 TestBuildTabFields）
	m.switchTab(tabMic)
	out = m.renderFields()
	if !strings.Contains(out, "麦克风调试:") || !strings.Contains(out, "自适应增益:") {
		t.Fatalf("mic tab render:\n%s", out)
	}
	// 动作类字段：检测底噪/生成诊断报告是执行动作而非打开弹层，
	// 按钮文字用动作动词，避免与「进入」混淆。
	m.cursor = fieldIndex(m, "noise_calibrate")
	out = m.renderFields()
	if !strings.Contains(out, "检测当前底噪: ▶ 开始") {
		t.Fatalf("noise_calibrate should render 开始:\n%s", out)
	}
	m.cursor = fieldIndex(m, "diagnose")
	out = m.renderFields()
	if !strings.Contains(out, "生成诊断报告: ▶ 生成") {
		t.Fatalf("diagnose should render 生成:\n%s", out)
	}
}

func TestFieldHelpShownUnderCursor(t *testing.T) {
	// 光标所在字段下方显示 help 说明，随光标移动切换
	m := readyModel(config.Default())
	m.switchTab(tabOutput)
	// 默认光标在句尾标点省略
	out := m.renderFields()
	if !strings.Contains(out, "· ") || !strings.Contains(out, "省略") {
		t.Fatalf("help not shown for cursor field:\n%s", out)
	}
	// 移到「中文排版自动空格」：其 help 应出现（不再提及上一项/默认开）
	m.cursor = 4 // 输出 tab 第 5 项 = auto_space
	out = m.renderFields()
	if !strings.Contains(out, "中英/中数间补空格") {
		t.Fatalf("auto_space help unclear:\n%s", out)
	}
	if strings.Contains(out, "默认开") || strings.Contains(out, "不删中文间空格") {
		t.Fatalf("auto_space help should not mention default/other option:\n%s", out)
	}
	// 删除中文间空格项 help 明确默认关与互不影响
	m.cursor = 3 // remove_cjk_space
	out = m.renderFields()
	if !strings.Contains(out, "默认关") || !strings.Contains(out, "互不影响") {
		t.Fatalf("remove_cjk_space help unclear:\n%s", out)
	}
	// 非光标字段不显示 help
	m.cursor = 0
	out = m.renderFields()
	if strings.Contains(out, "中英/中数间补空格") {
		t.Fatalf("help should follow cursor only:\n%s", out)
	}
}

// ---- 弹层覆盖 ----

func TestOverlayOnCoversInPlace(t *testing.T) {
	base := "line0\nline1\nline2\nline3\nline4"
	overlay := "OV0\nOV1"
	out := overlayOn(base, overlay, 5)
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("overlay must not change line count: %d", len(lines))
	}
	// 起始行保底 2（标题/tab 可见）
	if lines[2] != "OV0" || lines[3] != "OV1" {
		t.Fatalf("overlay placement:\n%s", out)
	}
	if lines[0] != "line0" || lines[1] != "line1" || lines[4] != "line4" {
		t.Fatalf("base lines outside overlay changed:\n%s", out)
	}
}

func TestOverlayOnSmallScreen(t *testing.T) {
	base := "title\ntab\nstatus\ncontent\nhelp"
	overlay := "O0\nO1\nO2\nO3\nO4\nO5\nO6\nO7\nO8\nO9"
	out := overlayOn(base, overlay, 5)
	lines := strings.Split(out, "\n")
	if lines[0] != "title" || lines[1] != "tab" {
		t.Fatalf("header must stay visible:\n%s", out)
	}
	if lines[2] != "O0" || lines[3] != "O1" {
		t.Fatalf("truncated overlay not placed after header:\n%s", out)
	}
}

func TestViewOverlayDoesNotPushContent(t *testing.T) {
	base := readyModel(config.Default())
	withOverlay := readyModel(config.Default())

	baseView := base.View()
	withOverlay.openEnginePicker()
	ovView := withOverlay.View()

	baseLines := strings.Split(baseView, "\n")
	ovLines := strings.Split(ovView, "\n")
	if ovLines[0] != baseLines[0] {
		t.Fatalf("overlay pushed title away: base=%q overlay=%q", baseLines[0], ovLines[0])
	}
	titleRow := -1
	for i, l := range ovLines {
		if strings.Contains(l, "选择识别引擎") {
			titleRow = i
			break
		}
	}
	if titleRow < 5 || titleRow > len(ovLines)/2+2 {
		t.Fatalf("overlay not centered: title at row %d of %d", titleRow, len(ovLines))
	}
}

// ---- 按键 ----

func TestOpenDiagnosticsWithoutBufferLogsFailure(t *testing.T) {
	m := readyModel(config.Default())
	m.openDiagnostics()
	if len(m.logs) == 0 || !strings.Contains(m.logs[len(m.logs)-1], "未启用") {
		t.Fatalf("diagnostics failure should be visible in TUI logs: %v", m.logs)
	}
}

func TestLogsKeyUppercaseL(t *testing.T) {
	m := readyModel(config.Default())
	before := m.logExpanded
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	if m.logExpanded == before {
		t.Fatal("uppercase L should toggle logs")
	}
}

// ---- 工具函数 ----

func TestRenderLevelBar(t *testing.T) {
	if got := stripANSI(renderLevelBar(0, 0, 5)); got != "░░░░░" {
		t.Fatalf("zero: %q", got)
	}
	if got := stripANSI(renderLevelBar(1, 0, 5)); got != "█████" {
		t.Fatalf("full: %q", got)
	}
	if got := stripANSI(renderLevelBar(0.5, 0, 4)); got != "██░░" {
		t.Fatalf("half: %q", got)
	}
	// 峰值保持标记：hold=0.9、宽度 5 → 位置 4 绘制标记，当前电平 0.2 → 位置 0 填充。
	if got := stripANSI(renderLevelBar(0.2, 0.9, 5)); got != "█░░░▏" {
		t.Fatalf("hold marker: %q", got)
	}
}

func TestTruncateText(t *testing.T) {
	if got := truncateText("你好世界", 5); got != "你好…" {
		t.Fatalf("truncate: %q", got)
	}
	if got := truncateText("short", 10); got != "short" {
		t.Fatalf("short: %q", got)
	}
}

func TestTruncateTextTail(t *testing.T) {
	if got := truncateTextTail("你好世界", 5); got != "…世界" {
		t.Fatalf("tail truncate: %q", got)
	}
	if got := truncateTextTail("short", 10); got != "short" {
		t.Fatalf("short: %q", got)
	}
}

func TestFormatLogLineKeepsStreamingTranscriptTail(t *testing.T) {
	m := New(config.Default())
	m.w = 16
	if got := m.formatLogLine("\r🎤 这是很长很长的实时识别文本"); got != "🎤 …识别文本" {
		t.Fatalf("formatted log = %q", got)
	}
	if got := m.formatLogLine("✅ 配置已保存"); got != "✅ 配置已保存" {
		t.Fatalf("non-streaming log = %q", got)
	}
}

func TestPinBottom(t *testing.T) {
	base := "title\ntab\nfields"
	footer := "help"
	out := pinBottom(base, footer, 6)
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("lines = %d, want 6", len(lines))
	}
	// footer 固定在最后一行
	if lines[5] != footer {
		t.Fatalf("footer not at bottom: %q", lines[5])
	}
	// 内容保留在顶部
	if lines[0] != "title" || lines[1] != "tab" || lines[2] != "fields" {
		t.Fatalf("content changed:\n%s", out)
	}
}

func TestPinBottomTruncatesContent(t *testing.T) {
	// 内容超出时优先保留 footer（截断内容）
	base := "l0\nl1\nl2\nl3\nl4\nl5\nl6"
	out := pinBottom(base, "help", 5)
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(lines))
	}
	if lines[4] != "help" {
		t.Fatalf("footer should survive truncation: %q", lines[4])
	}
	if lines[0] != "l0" {
		t.Fatalf("content head lost: %q", lines[0])
	}
}

func TestViewHelpPinnedToBottom(t *testing.T) {
	m := readyModel(config.Default())
	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) != m.h {
		t.Fatalf("view lines = %d, want screen height %d", len(lines), m.h)
	}
	if !strings.Contains(lines[len(lines)-1], "q 退出") {
		t.Fatalf("help line should be pinned at bottom: %q", lines[len(lines)-1])
	}
	// 切 tab 后 help 仍在同一行（最后一行）
	m.switchTab(tabMic)
	out2 := m.View()
	lines2 := strings.Split(out2, "\n")
	if !strings.Contains(lines2[len(lines2)-1], "q 退出") {
		t.Fatalf("help line moved after tab switch: %q", lines2[len(lines2)-1])
	}
}

func TestChannelLinksSeparateFromPicker(t *testing.T) {
	// 渠道选择弹层不内嵌长 URL（避免排版与蓝线下划线问题），链接单独维护在 engineLinks
	out := mustRenderPicker()
	for _, it := range engineChannels {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "http") {
				t.Fatalf("channel %q picker embeds a URL: %q", it.name, line)
			}
		}
	}
	if len(engineLinks["openai"]) != 0 {
		t.Fatalf("openai links should be empty (model catalog belongs to dashscope): %+v", engineLinks["openai"])
	}
	ds := engineLinks["dashscope"]
	if len(ds) < 2 ||
		ds[0].url != "https://platform.qianwenai.com/docs/developer-guides/speech/asr-realtime" ||
		ds[1].url != "https://www.qianwenai.com/models?search=asr" {
		t.Fatalf("dashscope links = %+v", ds)
	}
	// 豆包：官方文档 + 新版控制台
	db := engineLinks["doubao-stream"]
	if len(db) < 2 || db[0].url != "https://docs.volcengine.com/docs/6561/1395846?lang=zh" ||
		db[1].url != "https://console-embedded.volcengine.com/speech/new/setting/activate" {
		t.Fatalf("doubao links = %+v", db)
	}
}

func mustRenderPicker() string {
	m := readyModel(config.Default())
	m.openEnginePicker()
	return m.renderPicker()
}

func TestEngineTabDocLinks(t *testing.T) {
	// 引擎 tab 有「相关链接」主入口（不依赖所选引擎）
	m := readyModel(config.Default())
	fs := m.tabField[tabEngine]
	found := false
	for _, f := range fs {
		if f.key == "doc_links" && f.fType == fAction {
			found = true
		}
	}
	if !found {
		t.Fatalf("engine tab missing doc_links entry: %+v", fs)
	}
}

func TestLinkPickerShowsAllEngines(t *testing.T) {
	m := readyModel(config.Default())
	// 不切换引擎（默认 doubao-stream）也应能看到 openai / dashscope 的链接
	m.openLinkPicker()
	if m.linkPicker == nil {
		t.Fatal("link picker not opened")
	}
	if len(m.linkPicker.items) < 4 {
		t.Fatalf("link items should cover all engines: %+v", m.linkPicker.items)
	}
	names := make(map[string]bool, len(m.linkPicker.items))
	for _, it := range m.linkPicker.items {
		names[it.name] = true
	}
	for _, want := range []string{
		"[doubao-stream] 豆包官方文档",
		"[doubao-stream] 豆包新版控制台",
		"[dashscope] 千问实时语音识别文档",
		"[dashscope] 千问实时 ASR 模型库",
	} {
		if !names[want] {
			t.Fatalf("link picker missing %q: %+v", want, m.linkPicker.items)
		}
	}
	for _, gone := range []string{"[openai]"} {
		if names[gone] {
			t.Fatalf("link picker should not contain %q: %+v", gone, m.linkPicker.items)
		}
	}
	out := m.renderLinkPicker()
	if !strings.Contains(out, "相关链接") || !strings.Contains(out, "[dashscope]") {
		t.Fatalf("link picker render missing content:\n%s", out)
	}
	// 回车应发起打开命令（不报错）
	cmd := m.handleLinkPickerKey("enter")
	if cmd == nil {
		t.Fatal("enter on link should return a command")
	}
	// esc 关闭
	m.handleLinkPickerKey("esc")
	if m.linkPicker != nil {
		t.Fatal("esc should close link picker")
	}
}

// TestVoiceStatusNoiseCalibrating 验证底噪检测进行中，顶部状态区显示「底噪检测中」。
func TestVoiceStatusNoiseCalibrating(t *testing.T) {
	m := readyModel(config.Default())
	voice.SetNoiseCalibratingForTest(true)
	defer voice.SetNoiseCalibratingForTest(false)

	out := m.renderVoiceStatus()
	if !strings.Contains(out, "底噪检测中") {
		t.Fatalf("status should show 底噪检测中 while calibrating:\n%s", out)
	}

	// 检测结束恢复待机
	voice.SetNoiseCalibratingForTest(false)
	out = m.renderVoiceStatus()
	if strings.Contains(out, "底噪检测中") {
		t.Fatalf("status should not show 底噪检测中 after calibration:\n%s", out)
	}
}

// freePort 返回一个当前空闲的随机端口（先监听 :0 取号再释放）。
// 测试用它启动 AndroidMic 服务端，避免与用户正在运行的实例（默认 54345）争端口。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
