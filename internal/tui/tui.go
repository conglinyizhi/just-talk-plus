// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file was modified from the original Just Talk by whoamihappyhacking
// (GPL-3.0). See NOTICE for maintenance history.

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/hotkey"
	"github.com/c/just-talk-go/internal/asrcfg"
	"github.com/c/just-talk-go/plugins/voice"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	lStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	vStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	aStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	wStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	eStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	dStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	hStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).MarginTop(1)
)

type backendMsg hotkey.ProviderInfo
type devMsg struct {
	Devices []string
	Error   error
}
type fieldType int

const (
	fString fieldType = iota
	fToggle
	fSelect
	fAction // 二级入口：回车打开渠道配置弹层
)

type field struct {
	label   string
	key     string
	help    string
	fType   fieldType
	input   textinput.Model
	boolVal bool
	opts    []string
	optVals []string // select 显示值对应的写入值（缺省 = opts）
	optIdx  int
}

// selectValue 返回 select 字段当前选中的写入值。
func (f field) selectValue() string {
	if len(f.optVals) > 0 && f.optIdx >= 0 && f.optIdx < len(f.optVals) {
		return f.optVals[f.optIdx]
	}
	if f.optIdx >= 0 && f.optIdx < len(f.opts) {
		return f.opts[f.optIdx]
	}
	return ""
}

// pickerItem 渠道选择弹层中的一条候选，subtitle 说明该渠道特点。
type pickerItem struct {
	name     string
	subtitle string
}

// pickerState 渠道选择弹层状态；nil 表示未打开。
type pickerState struct {
	title string
	items []pickerItem
	idx   int
}

// engineChannels 渠道候选与特点说明（与 README / doctor 文案保持一致）。
var engineChannels = []pickerItem{
	{
		name:     "doubao-stream",
		subtitle: "火山引擎流式 ASR，边录边出字，支持热词；选定后可在下方表单中选择模型版本（1.0 / 2.0）",
	},
	{
		name:     "auralwise",
		subtitle: "异步任务转写，整段识别，无需公网 URL",
	},
	{
		name:     "openai",
		subtitle: "兼容 Whisper 接口，可指向本地 / 第三方端点；了解更多模型：https://www.qianwenai.com/models?search=asr",
	},
}

// pickerItemByName 返回指定渠道的候选项。
func pickerItemByName(name string) (pickerItem, bool) {
	for _, it := range engineChannels {
		if it.name == name {
			return it, true
		}
	}
	return pickerItem{}, false
}

// configOverlayState 渠道配置二级弹层；nil 表示未打开。
type configOverlayState struct {
	title   string
	fields  []field
	cursor  int
	editing bool
}

// micPanelState 麦克风调试面板；nil 表示未打开。
type micPanelState struct{}

// historyPanelState 识别历史面板；nil 表示未打开。
type historyPanelState struct {
	idx int
}

type Model struct {
	w, h          int
	ready         bool
	info          hotkey.ProviderInfo
	logs          []string
	devices       []string
	cfg           *config.Config
	debug         bool
	showLogs      bool
	OnSave        func(*config.Config) error
	fields        []field
	cursor        int
	editing       bool
	helpVisible   bool
	logExpanded   bool
	picker        *pickerState        // 渠道选择弹层；nil = 未打开
	configOverlay *configOverlayState // 渠道配置二级弹层；nil = 未打开
	micPanel      *micPanelState      // 麦克风调试面板；nil = 未打开
	historyPanel  *historyPanelState  // 识别历史面板；nil = 未打开
}

func New(cfg *config.Config) *Model {
	return &Model{cfg: cfg, fields: buildFields(cfg), logs: make([]string, 0, 100), cursor: -1, showLogs: true}
}

// buildFields 公共字段 + 「渠道配置」二级入口。
// 所有引擎专属设置（凭据、模型版本、热词等）收进渠道配置弹层，主表单保持统一。
func buildFields(cfg *config.Config) []field {
	vc := cfg.Voice
	ti := func(v string) textinput.Model { t := textinput.New(); t.SetValue(v); t.Cursor.Blink = false; return t }
	engines := asrcfg.Types()
	return []field{
		{label: "语音输入", key: "enabled", help: "关闭后不注册热键", fType: fToggle, boolVal: vc.Enabled},
		{label: "热键", key: "push_to_talk", help: "例: Alt+Super / F9 / Ctrl+Alt+Tab；不支持字母、数字、标点、空格等普通字符键", fType: fString, input: ti(vc.PushToTalk)},
		{label: "模式", key: "mode", help: "toggle 切换 / hold 按住", fType: fSelect, opts: []string{"toggle", "hold"}, optIdx: idxOf([]string{"toggle", "hold"}, vc.Mode)},
		{label: "引擎", key: "engine", help: "回车打开渠道选择；doubao-stream 流式 / auralwise、openai 批处理", fType: fSelect, opts: engines, optIdx: idxOf(engines, vc.Engine)},
		{label: "渠道配置", key: "channel_config", help: "回车进入 [引擎名] 渠道配置（凭据、模型版本等）", fType: fAction},
		{label: "麦克风调试", key: "mic_test", help: "回车进入；同一录音热键触发测试，实时电平与增益调整", fType: fAction},
		{label: "历史记录", key: "history", help: "回车查看最近识别结果，可选中复制到剪贴板", fType: fAction},
		{label: "语言", key: "language", help: "BCP-47，如 zh-CN；空 = 自动", fType: fString, input: ti(vc.Language)},
		{label: "自动上屏", key: "auto_submit", help: "识别后自动粘贴", fType: fToggle, boolVal: vc.AutoSubmit},
		{label: "停止延迟(ms)", key: "stop_delay_ms", help: "松手后补录毫秒", fType: fString, input: ti(fmt.Sprintf("%d", vc.StopDelayMs))},
	}
}

// channelConfigFields 构建「渠道配置」弹层里的字段：凭据 + 该引擎的全部专属设置。
func channelConfigFields(vc config.VoiceConfig, ti func(string) textinput.Model) []field {
	ec := vc.EngineConfig
	// get 优先 engine_config，其次旧版平铺字段（迁移显示）。
	get := func(name string) string {
		if v := ec[name]; v != "" {
			return v
		}
		switch name {
		case "app_id":
			return vc.AppKey
		case "access_key":
			return vc.AccessKey
		case "resource_id":
			return vc.ResourceID
		}
		return ""
	}
	def := func(v, d string) string {
		if strings.TrimSpace(v) == "" {
			return d
		}
		return v
	}

	switch asrcfg.EngineType(vc) {
	case "auralwise":
		return []field{
			{label: "API Key", key: "ec.api_key", help: "必填（asr_…）", fType: fString, input: ti(ec["api_key"])},
			{label: "Base URL", key: "ec.base_url", help: "默认 https://api.auralwise.cn/v1", fType: fString, input: ti(ec["base_url"])},
			{label: "Optimize", key: "ec.optimize", help: "auto | true | false", fType: fSelect, opts: []string{"auto", "true", "false"}, optIdx: idxOf([]string{"auto", "true", "false"}, def(ec["optimize"], "auto"))},
		}
	case "openai":
		return []field{
			{label: "API Key", key: "ec.api_key", help: "必填（sk-…）", fType: fString, input: ti(ec["api_key"])},
			{label: "Base URL", key: "ec.base_url", help: "默认 https://api.openai.com/v1", fType: fString, input: ti(ec["base_url"])},
			{label: "模型", key: "ec.model", help: "默认 whisper-1", fType: fString, input: ti(ec["model"])},
		}
	default: // doubao-stream
		mvOpts := []string{"1.0 (bigasr)", "2.0 (seedasr)"}
		mvVals := []string{"1.0", "2.0"}
		smOpts := []string{"async", "nostream"}
		// 未显式设置 model_version 时，新版 api_key 默认 2.0（seedasr），旧版凭据默认 1.0
		mvDefault := "1.0"
		if ec["api_key"] != "" {
			mvDefault = "2.0"
		}
		return []field{
			{label: "模型版本", key: "ec.model_version", help: "1.0=bigasr 旧资源 / 2.0=seedasr 新资源", fType: fSelect, opts: mvOpts, optVals: mvVals, optIdx: idxOf(mvVals, def(ec["model_version"], mvDefault))},
			{label: "流式模式", key: "ec.stream_mode", help: "默认 async", fType: fSelect, opts: smOpts, optIdx: idxOf(smOpts, def(ec["stream_mode"], "async"))},
			{label: "API Key(新版)", key: "ec.api_key", help: "新版控制台 X-Api-Key；与下方旧版凭据二选一", fType: fString, input: ti(ec["api_key"])},
			{label: "App ID(旧版)", key: "ec.app_id", help: "旧版控制台 App ID（无 api_key 时使用）", fType: fString, input: ti(get("app_id"))},
			{label: "Access Key(旧版)", key: "ec.access_key", help: "旧版 Access Token", fType: fString, input: ti(get("access_key"))},
			{label: "Base Host", key: "ec.base_host", help: "默认 openspeech.bytedance.com", fType: fString, input: ti(ec["base_host"])},
			{label: "热词", key: "hotwords", help: "逗号分隔术语", fType: fString, input: ti(strings.Join(vc.Hotwords, ", "))},
		}
	}
}

// switchEngine 切换引擎并重建动态字段，保留已编辑但未保存的字段值。
// 同时同步 m.cfg.Voice.Engine，保证渠道配置弹层等按当前引擎工作。
func (m *Model) switchEngine(engine string) {
	saved := m.fieldValues()
	m.cfg.Voice.Engine = engine
	next := *m.cfg
	m.fields = buildFields(&next)
	m.applyFieldValues(saved)
	// 引擎 select 以新值覆盖（不恢复旧选择）
	for i := range m.fields {
		if m.fields[i].key == "engine" {
			m.fields[i].optIdx = idxOf(m.fields[i].opts, engine)
			break
		}
	}
	if m.cursor < 0 {
		m.cursor = 0
	} else if m.cursor >= len(m.fields) {
		m.cursor = len(m.fields) - 1
	}
}

// currentEngine 返回主表单「引擎」字段当前选中的值。
func (m *Model) currentEngine() string {
	for _, f := range m.fields {
		if f.key == "engine" {
			return f.selectValue()
		}
	}
	return m.cfg.Voice.Engine
}

// fieldValues 收集当前所有字段值（按 key），用于切换引擎时保留未保存输入。
func (m *Model) fieldValues() map[string]string {
	out := make(map[string]string, len(m.fields))
	for _, f := range m.fields {
		switch f.fType {
		case fString:
			out[f.key] = f.input.Value()
		case fToggle:
			if f.boolVal {
				out[f.key] = "on"
			} else {
				out[f.key] = "off"
			}
		case fSelect:
			out[f.key] = f.selectValue()
		}
	}
	return out
}

func (m *Model) applyFieldValues(vals map[string]string) {
	for i := range m.fields {
		v, ok := vals[m.fields[i].key]
		if !ok {
			continue
		}
		f := &m.fields[i]
		switch f.fType {
		case fString:
			f.input.SetValue(v)
		case fToggle:
			f.boolVal = v == "on"
		case fSelect:
			idx := idxOf(f.opts, v)
			if idx == 0 && len(f.opts) > 0 && f.opts[0] != v && len(f.optVals) > 0 {
				idx = idxOf(f.optVals, v)
			}
			f.optIdx = idx
		}
	}
}

func (m *Model) SetDebug(debug bool) {
	m.debug = debug
}

func idxOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return 0
}

func (m *Model) Init() tea.Cmd { return tea.Batch(fetchDevices(), tea.EnterAltScreen, tickRefresh()) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h, m.ready = msg.Width, msg.Height, true
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case backendMsg:
		m.info = hotkey.ProviderInfo(msg)
	case refreshMsg:
		return m, tickRefresh()
	case devMsg:
		if msg.Error != nil {
			m.logf("设备: %s", msg.Error)
		} else {
			m.devices = msg.Devices
		}
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// 弹层/面板打开时，所有按键交给对应组件处理
	if m.picker != nil {
		return m.handlePickerKey(k)
	}
	if m.configOverlay != nil {
		return m.handleConfigOverlayKey(msg)
	}
	if m.micPanel != nil {
		return m.handleMicPanelKey(k)
	}
	if m.historyPanel != nil {
		return m.handleHistoryPanelKey(k)
	}

	// Editing a field
	if m.editing && m.cursor >= 0 && m.cursor < len(m.fields) {
		f := &m.fields[m.cursor]
		switch k {
		case "esc":
			m.editing = false
			f.input.Blur()
			return nil
		case "enter":
			m.editing = false
			f.input.Blur()
			m.save()
			return nil
		}
		switch f.fType {
		case fSelect:
			switch k {
			case "j", "down":
				f.optIdx++
				if f.optIdx >= len(f.opts) {
					f.optIdx = 0
				}
			case "k", "up":
				f.optIdx--
				if f.optIdx < 0 {
					f.optIdx = len(f.opts) - 1
				}
			}
		case fToggle:
			switch k {
			case " ":
				f.boolVal = !f.boolVal
			case "j", "down", "k", "up":
				m.editing = false
				m.cursor++
				if m.cursor >= len(m.fields) {
					m.cursor = 0
				}
			}
		case fString:
			var cmd tea.Cmd
			f.input, cmd = f.input.Update(msg)
			return cmd
		}
		return nil
	}

	// Navigation mode
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "s":
		m.save()
		return nil
	case "e", "i", "enter":
		if m.cursor >= 0 && m.cursor < len(m.fields) {
			switch m.fields[m.cursor].key {
			case "engine": // 引擎字段用渠道选择弹层
				m.openEnginePicker()
				return nil
			case "channel_config": // 渠道配置入口：打开二级弹层
				m.openChannelConfig()
				return nil
			case "mic_test": // 麦克风调试面板
				voice.StartMicTest()
				m.micPanel = &micPanelState{}
				return nil
			case "history": // 识别历史面板
				m.historyPanel = &historyPanelState{}
				return nil
			}
		}
		m.editing = true
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.fields[m.cursor].fType == fString {
			m.fields[m.cursor].input.Focus()
		}
	case "j", "down":
		if m.cursor < 0 {
			m.cursor = 0
		} else {
			m.cursor++
			if m.cursor >= len(m.fields) {
				m.cursor = len(m.fields) - 1
			}
		}
	case "k", "up":
		if m.cursor < 0 {
			m.cursor = 0
		} else {
			m.cursor--
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
	case "l":
		if m.showLogs {
			m.logExpanded = !m.logExpanded
		}
	case "h":
		m.helpVisible = !m.helpVisible
	}
	return nil
}

// ---- 渠道选择弹层 ----

// openEnginePicker 打开引擎渠道选择弹层，光标定位到当前引擎。
func (m *Model) openEnginePicker() {
	engine := m.currentEngine()
	idx := 0
	for i, it := range engineChannels {
		if it.name == engine {
			idx = i
			break
		}
	}
	m.picker = &pickerState{title: "选择识别引擎", items: engineChannels, idx: idx}
}

// handlePickerKey 弹层按键：j/k 导航，回车确认，esc/q 关闭。
func (m *Model) handlePickerKey(k string) tea.Cmd {
	p := m.picker
	if p == nil {
		return nil
	}
	switch k {
	case "j", "down", "tab":
		p.idx++
		if p.idx >= len(p.items) {
			p.idx = 0
		}
	case "k", "up", "shift+tab":
		p.idx--
		if p.idx < 0 {
			p.idx = len(p.items) - 1
		}
	case "enter":
		m.selectEngine(p.items[p.idx].name)
	case "esc", "q", "ctrl+c":
		m.picker = nil
	}
	return nil
}

// selectEngine 选中渠道：关闭弹层并按新引擎重建动态字段。
func (m *Model) selectEngine(name string) {
	m.picker = nil
	m.switchEngine(name)
	m.logf("引擎已切换为 %s", name)
}

// ---- 渠道配置二级弹层 ----

// openChannelConfig 打开当前引擎的「渠道配置」弹层（凭据、端点等）。
// 引擎取自主表单当前选中值（渠道选择弹层确认后可能尚未写回 m.cfg）。
func (m *Model) openChannelConfig() {
	engine := m.currentEngine()
	vc := m.cfg.Voice
	vc.Engine = engine
	ti := func(v string) textinput.Model { t := textinput.New(); t.SetValue(v); t.Cursor.Blink = false; return t }
	m.configOverlay = &configOverlayState{
		title:  fmt.Sprintf("[%s] 渠道配置", engine),
		fields: channelConfigFields(vc, ti),
		cursor: 0,
	}
}

// handleConfigOverlayKey 弹层按键：j/k 导航，e 编辑，s 保存并关闭，esc 取消。
func (m *Model) handleConfigOverlayKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	ov := m.configOverlay
	if ov == nil {
		return nil
	}

	// 编辑字段
	if ov.editing && ov.cursor >= 0 && ov.cursor < len(ov.fields) {
		f := &ov.fields[ov.cursor]
		switch k {
		case "esc", "enter":
			ov.editing = false
			f.input.Blur()
			return nil
		}
		switch f.fType {
		case fSelect:
			switch k {
			case "j", "down":
				f.optIdx++
				if f.optIdx >= len(f.opts) {
					f.optIdx = 0
				}
			case "k", "up":
				f.optIdx--
				if f.optIdx < 0 {
					f.optIdx = len(f.opts) - 1
				}
			}
		case fString:
			var cmd tea.Cmd
			f.input, cmd = f.input.Update(msg)
			return cmd
		}
		return nil
	}

	// 导航态
	switch k {
	case "j", "down":
		ov.cursor++
		if ov.cursor >= len(ov.fields) {
			ov.cursor = 0
		}
	case "k", "up":
		ov.cursor--
		if ov.cursor < 0 {
			ov.cursor = len(ov.fields) - 1
		}
	case "e", "i", "enter":
		ov.editing = true
		if ov.fields[ov.cursor].fType == fString {
			ov.fields[ov.cursor].input.Focus()
		}
	case "s":
		m.saveChannelConfig()
	case "esc", "q", "ctrl+c":
		m.configOverlay = nil
	}
	return nil
}

// saveChannelConfig 把弹层字段写回 [voice.engine_config] 并保存到磁盘。
func (m *Model) saveChannelConfig() {
	m.applyConfigOverlay()
	if err := config.Save(m.cfg); err != nil {
		m.logf("❌ 渠道配置保存失败: %s", err)
	} else {
		m.logf("✅ 渠道配置已保存")
	}
}

// applyConfigOverlay 把弹层字段合并进配置并关闭弹层（不写盘）。
func (m *Model) applyConfigOverlay() {
	ov := m.configOverlay
	if ov == nil {
		return
	}
	vc := &m.cfg.Voice
	if vc.EngineConfig == nil {
		vc.EngineConfig = map[string]string{}
	}
	for _, f := range ov.fields {
		switch {
		case f.key == "hotwords":
			vc.Hotwords = splitList(f.input.Value())
		case strings.HasPrefix(f.key, "ec."):
			name := strings.TrimPrefix(f.key, "ec.")
			val := strings.TrimSpace(f.input.Value())
			if f.fType == fSelect {
				val = f.selectValue()
			}
			if val == "" {
				delete(vc.EngineConfig, name)
			} else {
				vc.EngineConfig[name] = val
			}
		}
	}
	m.configOverlay = nil
}

// renderConfigOverlay 渲染渠道配置浮层（水平居中，字段可编辑）。
func (m *Model) renderConfigOverlay() string {
	ov := m.configOverlay
	if ov == nil {
		return ""
	}
	width := 72
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
	}
	inner := width - 4
	if inner < 40 {
		inner = 40
	}
	var lines []string
	lines = append(lines, " "+lStyle.Render(ov.title))
	for i, f := range ov.fields {
		marker := "  "
		if i == ov.cursor {
			if ov.editing {
				marker = aStyle.Render("▶ ")
			} else {
				marker = aStyle.Render("▸ ")
			}
		}
		lines = append(lines, marker+lStyle.Render(f.label+": ")+m.renderOverlayField(i, f, ov))
	}
	lines = append(lines, "")
	lines = append(lines, " "+dStyle.Render("j/k 导航  e 编辑  s 保存  esc 取消"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(width - 2).
		Render(strings.Join(lines, "\n"))

	pad := 0
	if m.w > 0 {
		pad = (m.w - lipgloss.Width(box)) / 2
		if pad < 0 {
			pad = 0
		}
	}
	bl := strings.Split(box, "\n")
	for i := range bl {
		bl[i] = strings.Repeat(" ", pad) + bl[i]
	}
	return strings.Join(bl, "\n")
}

// renderOverlayField 渲染弹层内的字段（弹层有自己的 cursor/editing 状态）。
func (m *Model) renderOverlayField(i int, f field, ov *configOverlayState) string {
	editing := ov.editing && ov.cursor == i
	switch f.fType {
	case fString:
		v := f.input.Value()
		if (f.key == "ec.api_key" || f.key == "ec.access_key") && !editing && len(v) > 6 {
			v = v[:6] + "***"
		}
		if editing {
			return f.input.View()
		}
		return vStyle.Render(v)
	case fSelect:
		v := f.opts[f.optIdx]
		if editing {
			return aStyle.Render("[" + v + " ▲▼]")
		}
		return vStyle.Render(v)
	}
	return ""
}

// ---- 麦克风调试面板 ----

// handleMicPanelKey 面板按键：+/- 细粒度调增益，空格切换自适应增益，esc 退出。
// 测试录音由同一全局录音热键触发（voice 插件侧处理）。
func (m *Model) handleMicPanelKey(k string) tea.Cmd {
	switch k {
	case "+", "=":
		voice.SetMicGain(voice.MicTestLevel().Gain + 2)
	case "-", "_":
		voice.SetMicGain(voice.MicTestLevel().Gain - 2)
	case " ":
		voice.SetMicAutoGain(!voice.MicTestLevel().AutoGain)
	case "esc", "q":
		voice.StopMicTest()
		m.micPanel = nil
	}
	return nil
}

// renderMicPanel 渲染麦克风调试面板（实时电平条 + 增益）。
func (m *Model) renderMicPanel() string {
	lvl := voice.MicTestLevel()
	width := 60
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
	}
	barW := width - 12
	if barW < 20 {
		barW = 20
	}
	peakBar := renderLevelBar(lvl.Peak, barW)
	rmsBar := renderLevelBar(lvl.RMS, barW)
	state := "待机（按录音热键开始测试）"
	if voice.MicTestTesting() {
		state = "测试中（按录音热键停止）"
	}
	ag := "○ 关"
	if lvl.AutoGain {
		ag = "● 开"
	}
	clipMark := ""
	if lvl.Clipping {
		clipMark = eStyle.Render(" ⚠ 削波！")
	}

	lines := []string{
		" " + lStyle.Render("🎤 麦克风调试"),
		"  峰值: " + vStyle.Render("["+peakBar+"]") + fmt.Sprintf(" %.2f", lvl.Peak) + clipMark,
		"  有效: " + vStyle.Render("["+rmsBar+"]") + fmt.Sprintf(" %.2f", lvl.RMS),
		"  增益: " + wStyle.Render(fmt.Sprintf("%d", lvl.Gain)) + dStyle.Render("  ( +/- 调整，范围 1-100 )"),
		"  自适应增益: " + aStyle.Render(ag) + dStyle.Render("  (空格切换)"),
		"  状态: " + vStyle.Render(state),
		"",
		" " + dStyle.Render("+/- 增益  |  空格 自适应增益  |  录音热键 开始/停止  |  esc 退出"),
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(width - 2).
		Render(strings.Join(lines, "\n"))

	pad := 0
	if m.w > 0 {
		pad = (m.w - lipgloss.Width(box)) / 2
		if pad < 0 {
			pad = 0
		}
	}
	bl := strings.Split(box, "\n")
	for i := range bl {
		bl[i] = strings.Repeat(" ", pad) + bl[i]
	}
	return strings.Join(bl, "\n")
}

// renderLevelBar 按 0..1 电平绘制字符条。
func renderLevelBar(level float64, width int) string {
	if width <= 0 {
		return ""
	}
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	filled := int(level*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// ---- 识别历史面板 ----

// handleHistoryPanelKey 面板按键：j/k 选择，回车复制到剪贴板，esc 退出。
func (m *Model) handleHistoryPanelKey(k string) tea.Cmd {
	p := m.historyPanel
	items := voice.History()
	switch k {
	case "j", "down":
		if p.idx < len(items)-1 {
			p.idx++
		}
	case "k", "up":
		if p.idx > 0 {
			p.idx--
		}
	case "enter":
		if len(items) == 0 || p.idx >= len(items) {
			return nil
		}
		if err := voice.CopyHistoryEntry(p.idx); err != nil {
			m.logf("❌ 复制失败: %s", err)
		} else {
			m.logf("📋 已复制: %s", truncateText(items[p.idx].Text, 60))
		}
	case "esc", "q":
		m.historyPanel = nil
	}
	return nil
}

// renderHistoryPanel 渲染识别历史面板（最近结果列表，可选中复制）。
func (m *Model) renderHistoryPanel() string {
	items := voice.History()
	width := 64
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
	}
	inner := width - 4
	if inner < 40 {
		inner = 40
	}
	lines := []string{" " + lStyle.Render("📋 历史记录（最近 "+itoa(len(items))+" 条）")}
	if len(items) == 0 {
		lines = append(lines, "  "+dStyle.Render("暂无识别结果"))
	} else {
		if m.historyPanel.idx >= len(items) {
			m.historyPanel.idx = len(items) - 1
		}
		for i, it := range items {
			text := truncateText(it.Text, inner-14)
			ts := it.Time.Format("15:04:05")
			if i == m.historyPanel.idx {
				lines = append(lines, aStyle.Render("▸ ")+vStyle.Render(text)+dStyle.Render("  "+ts))
			} else {
				lines = append(lines, "  "+vStyle.Render(text)+dStyle.Render("  "+ts))
			}
		}
	}
	lines = append(lines, "")
	lines = append(lines, " "+dStyle.Render("j/k 选择  回车复制到剪贴板  esc 退出"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(width - 2).
		Render(strings.Join(lines, "\n"))

	pad := 0
	if m.w > 0 {
		pad = (m.w - lipgloss.Width(box)) / 2
		if pad < 0 {
			pad = 0
		}
	}
	bl := strings.Split(box, "\n")
	for i := range bl {
		bl[i] = strings.Repeat(" ", pad) + bl[i]
	}
	return strings.Join(bl, "\n")
}

// truncateText 截断文本到指定显示宽度（含中文宽字符）。
func truncateText(s string, maxW int) string {
	w := 0
	for i, r := range s {
		cw := 1
		if r > 0x7F {
			cw = 2
		}
		if w+cw > maxW {
			return s[:i] + "…"
		}
		w += cw
	}
	return s
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// renderPicker 渲染渠道选择浮层（水平居中，subtitle 固定宽度自动换行）。
func (m *Model) renderPicker() string {
	p := m.picker
	if p == nil {
		return ""
	}
	width := 64
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
	}
	inner := width - 4
	if inner < 30 {
		inner = 30
	}
	subStyle := dStyle.Width(inner - 2)

	var lines []string
	lines = append(lines, " "+lStyle.Render(p.title))
	for i, it := range p.items {
		if i == p.idx {
			lines = append(lines, aStyle.Render("▸ "+it.name))
		} else {
			lines = append(lines, "  "+vStyle.Render(it.name))
		}
		// subtitle 跟随光标行显示，自动换行
		lines = append(lines, subStyle.Render("    "+it.subtitle))
	}
	lines = append(lines, "")
	lines = append(lines, " "+dStyle.Render("j/k 选择  回车确认  esc 关闭"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(width - 2).
		Render(strings.Join(lines, "\n"))

	// 水平居中
	pad := 0
	if m.w > 0 {
		pad = (m.w - lipgloss.Width(box)) / 2
		if pad < 0 {
			pad = 0
		}
	}
	bl := strings.Split(box, "\n")
	for i := range bl {
		bl[i] = strings.Repeat(" ", pad) + bl[i]
	}
	return strings.Join(bl, "\n")
}
func (m *Model) save() {
	next := *m.cfg
	vc := &next.Voice
	for _, f := range m.fields {
		switch f.key {
		case "enabled":
			vc.Enabled = f.boolVal
		case "push_to_talk":
			vc.PushToTalk = f.input.Value()
		case "mode":
			vc.Mode = f.selectValue()
		case "engine":
			vc.Engine = f.selectValue()
		case "language":
			vc.Language = f.input.Value()
		case "auto_submit":
			vc.AutoSubmit = f.boolVal
		case "stop_delay_ms":
			fmt.Sscanf(f.input.Value(), "%d", &vc.StopDelayMs)
		}
	}
	combo, err := config.ParseHotkey(vc.PushToTalk)
	if err != nil {
		m.logf("❌ 热键格式错误: %s", err)
		m.restorePushToTalkField()
		return
	}
	if combo.Key.IsTextKey() {
		m.logf("❌ 热键不支持普通字符键: %s", combo)
		m.logf("   请使用 Alt+Super、F9、Alt+F8、Ctrl+Alt+Tab 等全局快捷键")
		m.restorePushToTalkField()
		return
	}
	m.cfg = &next
	if err := config.Save(m.cfg); err != nil {
		m.logf("保存失败: %s", err)
	} else {
		m.logf("✅ 配置已保存到 %s", config.FindConfig())
	}
	m.logf("  push_to_talk=%s", vc.PushToTalk)
	if m.OnSave != nil {
		if err := m.OnSave(m.cfg); err != nil {
			m.logf("❌ 热键注册失败: %s", err)
		}
	}
}

func (m *Model) restorePushToTalkField() {
	value := m.cfg.Voice.PushToTalk
	if !validVoiceHotkeyString(value) {
		value = config.Default().Voice.PushToTalk
	}
	for i := range m.fields {
		if m.fields[i].key == "push_to_talk" {
			m.fields[i].input.SetValue(value)
			break
		}
	}
	m.logf("↩️  热键已恢复为 %s", value)
}

func validVoiceHotkeyString(value string) bool {
	combo, err := config.ParseHotkey(value)
	return err == nil && !combo.Key.IsTextKey()
}

func splitList(s string) []string {
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
func (m *Model) View() string {
	if !m.ready {
		return "loading..."
	}
	var b strings.Builder
	b.WriteString(tStyle.Render("🎙️ 🗣️ Just Talk Plus") + "\n")
	b.WriteString(vStyle.Render("减少用键盘的次数，改用口喷吧。") + "\n")
	b.WriteString(m.renderVoiceStats() + "\n\n")
	b.WriteString(lStyle.Render("── 配置 (e 编辑, s 保存, h 帮助, j/k 导航) ──") + "\n")
	for i, f := range m.fields {
		marker := "  "
		if i == m.cursor {
			if m.editing {
				marker = aStyle.Render("▶ ")
			} else {
				marker = aStyle.Render("▸ ")
			}
		}
		line := marker + lStyle.Render(f.label+": ") + m.renderField(i, f)
		if m.helpVisible && f.help != "" {
			line += " " + dStyle.Render("("+f.help+")")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + lStyle.Render("── 录音状态 ──") + "\n")
	b.WriteString("  " + m.renderVoiceStatus() + "\n")
	if m.showLogs {
		b.WriteString("\n" + lStyle.Render("── 日志 (l 展开) ──") + "\n")
		maxLogs := 5
		if m.logExpanded {
			maxLogs = 100
		}
		// TUI internal logs
		s := 0
		if len(m.logs) > maxLogs {
			s = len(m.logs) - maxLogs
		}
		for _, l := range m.logs[s:] {
			b.WriteString("  " + dStyle.Render(l) + "\n")
		}
		// Voice plugin logs
		sv := 0
		if len(voice.TUILogBuf) > maxLogs {
			sv = len(voice.TUILogBuf) - maxLogs
		}
		for _, l := range voice.TUILogBuf[sv:] {
			b.WriteString("  " + dStyle.Render(l) + "\n")
		}
	}
	b.WriteString(hStyle.Render("  j/k 导航 | e 编辑 | h 帮助 | esc 退出编辑 | s 保存 | q 退出"))
	if m.picker != nil {
		b.WriteString("\n" + m.renderPicker())
	} else if m.configOverlay != nil {
		b.WriteString("\n" + m.renderConfigOverlay())
	} else if m.micPanel != nil {
		b.WriteString("\n" + m.renderMicPanel())
	} else if m.historyPanel != nil {
		b.WriteString("\n" + m.renderHistoryPanel())
	}
	return b.String()
}

func (m *Model) renderVoiceStats() string {
	stats := voice.TUIStats()
	cpm := 0.0
	if stats.AudioDuration > 0 {
		cpm = float64(stats.Chars) / stats.AudioDuration.Minutes()
	}
	lastCPM := 0.0
	if stats.LastAudioDuration > 0 {
		lastCPM = float64(stats.LastTextChars) / stats.LastAudioDuration.Minutes()
	}
	last := dStyle.Render("最近速度 ") + wStyle.Render("0") + dStyle.Render(" 字/分钟")
	if stats.LastAudioDuration > 0 {
		last = dStyle.Render("最近速度 ") + wStyle.Render(fmt.Sprintf("%.0f", lastCPM)) + dStyle.Render(" 字/分钟")
	}
	return strings.Join([]string{
		dStyle.Render("历史 ") + aStyle.Render(fmt.Sprintf("%d", stats.Sessions)) + dStyle.Render(" 次"),
		dStyle.Render("总字数 ") + aStyle.Render(fmt.Sprintf("%d", stats.Chars)),
		dStyle.Render("平均速度 ") + wStyle.Render(fmt.Sprintf("%.0f", cpm)) + dStyle.Render(" 字/分钟"),
		last,
	}, "  |  ")
}

func (m *Model) renderVoiceStatus() string {
	status := voice.TUIStatus()
	label := "待机"
	style := dStyle
	switch status.State {
	case "connecting":
		label, style = "连接中", wStyle
	case "recording":
		label, style = "录音中", aStyle
	case "stopping_delayed":
		label, style = "延迟停止", wStyle
	case "stopping":
		label, style = "停止中", wStyle
	case "error":
		label, style = "错误", eStyle
	}
	parts := []string{style.Render(label)}
	if status.Detail != "" {
		parts = append(parts, vStyle.Render(status.Detail))
	}
	if !status.StopAt.IsZero() {
		remaining := time.Until(status.StopAt)
		if remaining < 0 {
			remaining = 0
		}
		parts = append(parts, fmt.Sprintf("剩余 %dms", remaining.Milliseconds()))
	}
	if status.State == "error" && !status.ErrorUntil.IsZero() {
		remaining := time.Until(status.ErrorUntil)
		if remaining < 0 {
			remaining = 0
		}
		parts = append(parts, fmt.Sprintf("保留 %ds", int(remaining.Seconds())))
	}
	if status.PendingFinishes > 0 {
		parts = append(parts, fmt.Sprintf("后台收尾 %d", status.PendingFinishes))
	}
	if status.SessionID > 0 {
		parts = append(parts, dStyle.Render(fmt.Sprintf("#%d", status.SessionID)))
	}
	if !m.debug {
		return strings.Join(parts, "  ")
	}
	if !status.UpdatedAt.IsZero() {
		age := time.Since(status.UpdatedAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, dStyle.Render(fmt.Sprintf("更新 %dms 前", age.Milliseconds())))
	}
	if !status.LastHotkeyAt.IsZero() {
		age := time.Since(status.LastHotkeyAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, dStyle.Render(fmt.Sprintf("热键 %s %dms 前", status.LastHotkeyType, age.Milliseconds())))
	}
	if !status.LastHandledAt.IsZero() {
		age := time.Since(status.LastHandledAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, dStyle.Render(fmt.Sprintf("处理 %s %dms 前", status.LastHandledType, age.Milliseconds())))
	}
	if status.QueuedHotkeys > 0 || status.HandledHotkeys > 0 {
		parts = append(parts, dStyle.Render(fmt.Sprintf("入队/处理 %d/%d q=%d", status.QueuedHotkeys, status.HandledHotkeys, status.EventQueueLen)))
	}
	return strings.Join(parts, "  ")
}

func (m *Model) renderField(i int, f field) string {
	editing := m.editing && m.cursor == i
	switch f.fType {
	case fString:
		v := f.input.Value()
		if (f.key == "ec.api_key" || f.key == "ec.access_key") && !editing && len(v) > 6 {
			v = v[:6] + "***"
		}
		if editing {
			return f.input.View()
		}
		return vStyle.Render(v)
	case fToggle:
		if f.boolVal {
			return aStyle.Render("● 开") + "  " + dStyle.Render("(空格)")
		}
		return dStyle.Render("○ 关  (空格)")
	case fSelect:
		v := f.opts[f.optIdx]
		if editing {
			return aStyle.Render("[" + v + " ▲▼]")
		}
		return vStyle.Render(v)
	case fAction:
		return aStyle.Render("▶ ") + vStyle.Render("进入")
	}
	return ""
}

func (m *Model) logf(format string, args ...interface{}) {
	if !m.showLogs {
		return
	}
	m.logs = append(m.logs, fmt.Sprintf(format, args...))
}

func SetProviderInfo(info hotkey.ProviderInfo) tea.Cmd {
	return func() tea.Msg { return backendMsg(info) }
}
func fetchDevices() tea.Cmd {
	return func() tea.Msg {
		devices, err := voice.ListDevices()
		if err != nil {
			return devMsg{Error: err}
		}
		return devMsg{Devices: devices}
	}
}

type refreshMsg struct{}

func tickRefresh() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return refreshMsg{} })
}

// Handle refreshMsg in Update
