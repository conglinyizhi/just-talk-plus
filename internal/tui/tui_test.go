// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package tui

import (
	"strings"
	"testing"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/plugins/voice"
	tea "github.com/charmbracelet/bubbletea"
)

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
	m.list.SetWidth(96)
	m.list.SetHeight(20)
	return m
}

// ---- 字段分组 ----

func TestBuildTabFields(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)

	general := tabKeys(m.tabField[tabGeneral])
	for _, want := range []string{"enabled", "push_to_talk", "mode", "language", "auto_submit", "stop_delay_ms"} {
		if !general[want] {
			t.Fatalf("general missing %q: %v", want, general)
		}
	}
	engineTab := tabKeys(m.tabField[tabEngine])
	if !engineTab["engine"] || !engineTab["channel_config"] {
		t.Fatalf("engine tab: %v", engineTab)
	}
	mic := tabKeys(m.tabField[tabMic])
	for _, want := range []string{"mic_test", "gain", "auto_gain"} {
		if !mic[want] {
			t.Fatalf("mic tab missing %q: %v", want, mic)
		}
	}
	output := tabKeys(m.tabField[tabOutput])
	for _, want := range []string{"omit_trailing_punct", "omit_trailing_all", "omit_trailing_ascii"} {
		if !output[want] {
			t.Fatalf("output tab missing %q: %v", want, output)
		}
	}
	// 副设置缩进
	if f := fieldByKey(m, "omit_trailing_all"); f.indent != 1 {
		t.Fatalf("omit_trailing_all indent = %d", f.indent)
	}
}

// ---- tab 切换 ----

func TestSwitchTab(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabMic)
	if m.tab != tabMic {
		t.Fatalf("tab = %d", m.tab)
	}
	if len(m.list.Items()) != len(m.tabField[tabMic]) {
		t.Fatalf("list items = %d, want %d", len(m.list.Items()), len(m.tabField[tabMic]))
	}
	// 数字键切换
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.tab != tabOutput {
		t.Fatalf("digit tab = %d, want tabOutput", m.tab)
	}
}

func TestTabBar(t *testing.T) {
	m := readyModel(config.Default())
	out := m.tabBar()
	if !strings.Contains(out, "通用") || !strings.Contains(out, "历史") {
		t.Fatalf("tab bar: %q", out)
	}
}

// ---- 列表与编辑交互 ----

func TestListEditingString(t *testing.T) {
	cfg := config.Default()
	m := readyModel(cfg)
	m.switchTab(tabGeneral)
	fs := m.tabField[tabGeneral]
	idx := -1
	for i := range fs {
		if fs[i].key == "language" {
			idx = i
			break
		}
	}
	m.list.Select(idx)
	// enter 进入编辑（预填当前值，模拟清空后重新输入）
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing {
		t.Fatal("should enter editing")
	}
	m.input.SetValue("")
	for _, r := range "en-US" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(r))})
	}
	// enter 确认
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Fatal("should exit editing")
	}
	if got := fieldByKey(m, "language").input.Value(); got != "en-US" {
		t.Fatalf("language = %q", got)
	}
	// esc 取消恢复
	m.list.Select(idx)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "fr" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(r))})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := fieldByKey(m, "language").input.Value(); got != "en-US" {
		t.Fatalf("esc should restore: %q", got)
	}
}

func TestListEditingToggle(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabGeneral)
	fs := m.tabField[tabGeneral]
	idx := -1
	for i := range fs {
		if fs[i].key == "auto_submit" {
			idx = i
			break
		}
	}
	before := fs[idx].boolVal
	m.list.Select(idx)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if fs[idx].boolVal == before {
		t.Fatal("toggle should flip on enter")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if fs[idx].boolVal != before {
		t.Fatal("toggle should flip back")
	}
}

func TestListEditingSelect(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabGeneral)
	fs := m.tabField[tabGeneral]
	idx := -1
	for i := range fs {
		if fs[i].key == "mode" {
			idx = i
			break
		}
	}
	before := fs[idx].optIdx
	m.list.Select(idx)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing {
		t.Fatal("select should enter editing")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if fs[idx].optIdx == before {
		t.Fatal("select option should cycle on down")
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
		m.list.Select(i)
		switch fs[i].key {
		case "engine":
			m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
			if m.picker == nil {
				t.Fatal("engine enter should open picker")
			}
			m.picker = nil
		case "channel_config":
			m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
			if m.configOverlay == nil {
				t.Fatal("channel_config enter should open overlay")
			}
			m.configOverlay = nil
		}
	}
	m.switchTab(tabMic)
	m.list.Select(0) // mic_test
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
	m.refreshHistoryItems()
	// 空历史下 enter 不 panic
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	// 左键回退到上一分组（输出），而不是跳到第一个
	m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.tab != tabOutput {
		t.Fatalf("left from history should go to tabOutput, got %d", m.tab)
	}
	// 回到历史后右键回绕到第一个
	m.switchTab(tabHistory)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.tab != tabGeneral {
		t.Fatalf("right from history should wrap to tabGeneral, got %d", m.tab)
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

func TestConfigOverlayFlow(t *testing.T) {
	cfg := config.Default()
	m := readyModel(cfg)
	m.openChannelConfig()
	if m.configOverlay == nil {
		t.Fatal("overlay should open")
	}
	if m.configOverlay.title != "[doubao-stream] 渠道配置" {
		t.Fatalf("title = %q", m.configOverlay.title)
	}
	ov := m.configOverlay
	for i := range ov.fields {
		if ov.fields[i].key == "ec.model_version" {
			ov.cursor = i
			ov.fields[i].optIdx = 1 // 2.0
			break
		}
	}
	m.applyConfigOverlay()
	if m.configOverlay != nil {
		t.Fatal("overlay should close")
	}
	if got := cfg.Voice.EngineConfig["model_version"]; got != "2.0" {
		t.Fatalf("model_version = %q", got)
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
}

// ---- 字段值写回 ----

func TestApplyFieldsTo(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	fieldByKey(m, "language").input.SetValue("ja-JP")
	fieldByKey(m, "auto_submit").boolVal = false
	fieldByKey(m, "omit_trailing_punct").boolVal = true
	fieldByKey(m, "gain").input.SetValue("42")

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
	if next.Voice.Gain != 42 {
		t.Fatalf("gain = %d", next.Voice.Gain)
	}
}

// ---- 字段值渲染 ----

func TestFieldValue(t *testing.T) {
	m := readyModel(config.Default())
	if got := m.fieldValue(fieldByKey(m, "auto_submit")); !strings.Contains(got, "●") {
		t.Fatalf("toggle on: %q", got)
	}
	if got := m.fieldValue(fieldByKey(m, "engine")); !strings.Contains(got, "doubao-stream") {
		t.Fatalf("engine value: %q", got)
	}
	if got := m.fieldValue(fieldByKey(m, "mic_test")); !strings.Contains(got, "增益") {
		t.Fatalf("mic_test value: %q", got)
	}
}

// ---- 工具函数 ----

func TestRenderLevelBar(t *testing.T) {
	if got := renderLevelBar(0, 5); got != "░░░░░" {
		t.Fatalf("zero: %q", got)
	}
	if got := renderLevelBar(1, 5); got != "█████" {
		t.Fatalf("full: %q", got)
	}
	if got := renderLevelBar(0.5, 4); got != "██░░" {
		t.Fatalf("half: %q", got)
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

func TestTabBarKeepsDigitOnActive(t *testing.T) {
	m := readyModel(config.Default())
	out := m.tabBar()
	// 当前 tab（通用）高亮但数字保留
	if !strings.Contains(out, "1 通用") {
		t.Fatalf("active tab should keep its digit: %q", out)
	}
	// 其他 tab 也带数字
	for _, want := range []string{"2 引擎", "3 麦克风", "4 输出", "5 历史"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tab bar missing %q: %q", want, out)
		}
	}
}

func TestLogsKeyUppercaseL(t *testing.T) {
	m := readyModel(config.Default())
	before := m.logExpanded
	// 终端按 Shift+L 产生大写 'L' rune
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	if m.logExpanded == before {
		t.Fatal("uppercase L should toggle logs")
	}
	// 小写 l 仍是"下一个分组"
	tabBefore := m.tab
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if m.tab == tabBefore {
		t.Fatal("lowercase l should switch tab")
	}
}

func TestOverlayOnCoversInPlace(t *testing.T) {
	base := "line0\nline1\nline2\nline3\nline4"
	overlay := "OV0\nOV1"
	out := overlayOn(base, overlay, 5)
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("overlay must not change line count: %d", len(lines))
	}
	// 垂直居中：5 行中 2 行浮层 → 起始行 = (5-2)/2 = 1
	if lines[1] != "OV0" || lines[2] != "OV1" {
		t.Fatalf("overlay not centered:\n%s", out)
	}
	// 非覆盖行保留
	if lines[0] != "line0" || lines[3] != "line3" || lines[4] != "line4" {
		t.Fatalf("base lines outside overlay changed:\n%s", out)
	}
}

func TestViewOverlayDoesNotPushContent(t *testing.T) {
	cfg := config.Default()
	base := readyModel(cfg)
	withOverlay := readyModel(cfg)

	baseView := base.View()
	withOverlay.openEnginePicker()
	ovView := withOverlay.View()

	baseLines := strings.Split(baseView, "\n")
	ovLines := strings.Split(ovView, "\n")
	// 弹层不把顶部标题推走（覆盖而非追加）
	if ovLines[0] != baseLines[0] {
		t.Fatalf("overlay pushed title away: base=%q overlay=%q", baseLines[0], ovLines[0])
	}
	// 弹层内容出现在屏幕中部（而非底部追加）
	titleRow := -1
	for i, l := range ovLines {
		if strings.Contains(l, "选择识别引擎") {
			titleRow = i
			break
		}
	}
	if titleRow < 0 {
		t.Fatalf("overlay content missing:\n%s", ovView)
	}
	if titleRow < 5 || titleRow > len(ovLines)/2+2 {
		t.Fatalf("overlay not centered: title at row %d of %d", titleRow, len(ovLines))
	}
}

func TestHistoryEmptyHint(t *testing.T) {
	m := readyModel(config.Default())
	m.switchTab(tabHistory)
	out := m.View()
	for _, want := range []string{"暂无识别结果", "内存", "下次启动"} {
		if !strings.Contains(out, want) {
			t.Fatalf("history empty hint missing %q:\n%s", want, out)
		}
	}
	// 空历史时不渲染 list 的英文 "No items"
	if strings.Contains(out, "No items") {
		t.Fatalf("should not show English empty message:\n%s", out)
	}
}
