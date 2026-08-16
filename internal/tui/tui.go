// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file was modified from the original Just Talk by whoamihappyhacking
// (GPL-3.0). See NOTICE for maintenance history.

// Package tui 提供 Just Talk Plus 的 Bubble Tea 配置界面。
//
// 主绘制基于 charmbracelet 组件库：
//   - list      设置项列表（title/description 两行布局，自带滚动与光标）
//   - textinput 字符串/数字字段编辑
//   - help      底部按键帮助（由按键绑定自动生成）
//   - lipgloss  tab 分组栏与整体样式
//
// 布局为 tab 分组（通用/引擎/麦克风/输出/历史），垂直高度只取决于
// 单组字段数，不再随设置项总数线性增长。渠道选择、渠道配置、麦克风调试
// 为模态弹层，保留独立交互。
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/hotkey"
	"github.com/c/just-talk-go/internal/asrcfg"
	"github.com/c/just-talk-go/plugins/voice"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
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
	fAction // 二级入口：回车打开弹层/面板
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
	indent  int // 层级缩进（副设置项依赖主设置项时用）
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

// ---- tab 分组 ----

type tabID int

const (
	tabGeneral tabID = iota
	tabEngine
	tabMic
	tabOutput
	tabHistory
)

var tabNames = []string{"通用", "引擎", "麦克风", "输出", "历史"}

// ---- 弹层状态（保留现有模态交互）----

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
		subtitle: "火山引擎流式 ASR，边录边出字，支持热词；选定后可在渠道配置中选择模型版本（1.0 / 2.0）",
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

// configOverlayState 渠道配置二级弹层；nil 表示未打开。
type configOverlayState struct {
	title   string
	fields  []field
	cursor  int
	editing bool
}

// micPanelState 麦克风调试面板；nil 表示未打开。
type micPanelState struct{}

// ---- 按键绑定（供 help 组件展示）----

type keyMap struct {
	Up, Down, Enter, Left, Right, Save, Logs, Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Left, k.Right, k.Save, k.Logs, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

var keys = keyMap{
	Up:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "上移")),
	Down:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "下移")),
	Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "编辑/进入")),
	Left:  key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "上一个分组")),
	Right: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "下一个分组")),
	Save:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "保存")),
	Logs:  key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "展开日志")),
	Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "退出")),
}

// ---- list 条目适配 ----

// fieldItem 设置项的 list.Item：title=设置名，description=当前值/提示。
type fieldItem struct {
	title string
	desc  string
}

func (i fieldItem) Title() string       { return i.title }
func (i fieldItem) Description() string { return i.desc }
func (i fieldItem) FilterValue() string { return i.title }

// historyItem 识别历史的 list.Item：title=文本，description=时间与操作提示。
type historyItem struct {
	title string
	desc  string
	idx   int
}

func (i historyItem) Title() string       { return i.title }
func (i historyItem) Description() string { return i.desc }
func (i historyItem) FilterValue() string { return i.title }

// ---- 主 Model ----

type Model struct {
	w, h        int
	ready       bool
	info        hotkey.ProviderInfo
	logs        []string
	devices     []string
	cfg         *config.Config
	debug       bool
	showLogs    bool
	logExpanded bool
	OnSave      func(*config.Config) error

	tab      tabID
	tabField map[tabID][]field
	list     list.Model
	help     help.Model

	editing      bool
	editOriginal string // fString 编辑前的值（esc 恢复）
	input        textinput.Model

	picker        *pickerState
	configOverlay *configOverlayState
	micPanel      *micPanelState
}

func New(cfg *config.Config) *Model {
	li := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	li.SetShowTitle(false)
	li.SetShowStatusBar(false)
	li.SetShowHelp(false)
	li.SetFilteringEnabled(false)
	li.SetShowPagination(false)

	ti := textinput.New()
	ti.Placeholder = "输入新值…"
	ti.CharLimit = 128
	ti.Cursor.Blink = false

	m := &Model{
		cfg:      cfg,
		tabField: buildTabFields(cfg),
		list:     li,
		help:     help.New(),
		input:    ti,
		logs:     make([]string, 0, 100),
		showLogs: true,
		tab:      tabGeneral,
	}
	return m
}

// ---- 字段构建（按 tab 分组）----

// buildTabFields 按 tab 分组构建全部设置字段。
func buildTabFields(cfg *config.Config) map[tabID][]field {
	vc := cfg.Voice
	ti := func(v string) textinput.Model { t := textinput.New(); t.SetValue(v); t.Cursor.Blink = false; return t }
	engines := asrcfg.Types()

	general := []field{
		{label: "语音输入", key: "enabled", help: "关闭后不注册热键", fType: fToggle, boolVal: vc.Enabled},
		{label: "热键", key: "push_to_talk", help: "例: Alt+Super / F9；不支持字母数字等普通字符键", fType: fString, input: ti(vc.PushToTalk)},
		{label: "模式", key: "mode", help: "toggle 切换 / hold 按住", fType: fSelect, opts: []string{"toggle", "hold"}, optIdx: idxOf([]string{"toggle", "hold"}, vc.Mode)},
		{label: "语言", key: "language", help: "BCP-47，如 zh-CN；空 = 自动", fType: fString, input: ti(vc.Language)},
		{label: "自动上屏", key: "auto_submit", help: "识别后自动粘贴", fType: fToggle, boolVal: vc.AutoSubmit},
		{label: "停止延迟(ms)", key: "stop_delay_ms", help: "松手后补录毫秒", fType: fString, input: ti(fmt.Sprintf("%d", vc.StopDelayMs))},
	}

	engineTab := []field{
		{label: "引擎", key: "engine", help: "回车选择渠道；doubao-stream 流式 / auralwise、openai 批处理", fType: fAction},
		{label: "渠道配置", key: "channel_config", help: "回车进入 [引擎名] 渠道配置（凭据、模型版本等）", fType: fAction},
	}

	mic := []field{
		{label: "麦克风调试", key: "mic_test", help: "回车进入；同一录音热键触发测试，实时电平与增益调整", fType: fAction},
		{label: "增益", key: "gain", help: "1-100，细粒度；调试面板内可实时调整", fType: fString, input: ti(fmt.Sprintf("%d", vc.Gain))},
		{label: "自适应增益", key: "auto_gain", help: "爆音自动降低，安静后缓慢恢复", fType: fToggle, boolVal: vc.AutoGain},
	}

	output := []field{
		{label: "句尾标点省略", key: "omit_trailing_punct", help: "上屏/复制前省略句尾句号「。」", fType: fToggle, boolVal: vc.OmitTrailingPunct},
		{label: "切断所有符号", key: "omit_trailing_all", help: "句尾所有标点一并省略（含英文）", fType: fToggle, boolVal: vc.OmitTrailingAll, indent: 1},
		{label: "英文符号 .!?", key: "omit_trailing_ascii", help: "额外省略英文半角 . ! ?", fType: fToggle, boolVal: vc.OmitTrailingASCII, indent: 1},
	}

	_ = engines
	return map[tabID][]field{
		tabGeneral: general,
		tabEngine:  engineTab,
		tabMic:     mic,
		tabOutput:  output,
	}
}

// channelConfigFields 构建「渠道配置」弹层里的字段：凭据 + 该引擎的全部专属设置。
func channelConfigFields(vc config.VoiceConfig, ti func(string) textinput.Model) []field {
	ec := vc.EngineConfig
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

func idxOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return 0
}

// ---- list 数据刷新 ----

// currentFields 返回当前 tab 的设置字段（历史 tab 返回 nil）。
func (m *Model) currentFields() []field {
	if m.tab == tabHistory {
		return nil
	}
	return m.tabField[m.tab]
}

// selectedField 返回 list 当前选中项对应的字段。
func (m *Model) selectedField() *field {
	fs := m.currentFields()
	i := m.list.Index()
	if i < 0 || i >= len(fs) {
		return nil
	}
	return &fs[i]
}

// refreshItems 按当前 tab 重建 list 条目（设置值变化后调用）。
func (m *Model) refreshItems() {
	if m.tab == tabHistory {
		m.refreshHistoryItems()
		return
	}
	fs := m.currentFields()
	items := make([]list.Item, len(fs))
	for i := range fs {
		f := &fs[i]
		items[i] = fieldItem{
			title: strings.Repeat("  ", f.indent) + f.label,
			desc:  m.fieldValue(f),
		}
	}
	m.list.SetItems(items)
}

func (m *Model) refreshHistoryItems() {
	hist := voice.History()
	items := make([]list.Item, len(hist))
	for i, e := range hist {
		items[i] = historyItem{
			title: truncateText(e.Text, 60),
			desc:  "   " + e.Time.Format("15:04:05") + "  ·  回车复制到剪贴板",
			idx:   i,
		}
	}
	m.list.SetItems(items)
}

// fieldValue 渲染字段当前值（list 的 description 行）。
func (m *Model) fieldValue(f *field) string {
	switch f.fType {
	case fString:
		v := f.input.Value()
		if v == "" {
			return dStyle.Render("（空）")
		}
		if f.key == "ec.api_key" || f.key == "ec.access_key" {
			if len(v) > 6 {
				v = v[:6] + "***"
			}
		}
		return v
	case fToggle:
		if f.boolVal {
			return aStyle.Render("● 开")
		}
		return dStyle.Render("○ 关")
	case fSelect:
		return vStyle.Render(f.selectValue())
	case fAction:
		switch f.key {
		case "engine":
			return vStyle.Render(m.cfg.Voice.Engine) + dStyle.Render("（回车选择）")
		case "mic_test":
			return dStyle.Render("回车进入（当前增益 " + fmt.Sprintf("%d", voice.GetMicGain()) + "）")
		default:
			return dStyle.Render("回车进入")
		}
	}
	return ""
}

// ---- 生命周期 ----

func (m *Model) SetDebug(debug bool) { m.debug = debug }

func (m *Model) Init() tea.Cmd { return tea.Batch(fetchDevices(), tea.EnterAltScreen, tickRefresh()) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h, m.ready = msg.Width, msg.Height, true
		m.list.SetWidth(msg.Width - 4)
		m.list.SetHeight(msg.Height - 11)
		m.help.Width = msg.Width - 4
		m.refreshItems()
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case backendMsg:
		m.info = hotkey.ProviderInfo(msg)
	case refreshMsg:
		// 历史 tab 与状态区需要持续刷新
		if m.tab == tabHistory && len(m.list.Items()) != len(voice.History()) {
			m.refreshHistoryItems()
		}
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

	// 弹层/面板优先
	if m.picker != nil {
		return m.handlePickerKey(k)
	}
	if m.configOverlay != nil {
		return m.handleConfigOverlayKey(msg)
	}
	if m.micPanel != nil {
		return m.handleMicPanelKey(k)
	}

	// 编辑态
	if m.editing {
		f := m.selectedField()
		if f == nil {
			m.editing = false
			return nil
		}
		switch {
		case k == "esc":
			if f.fType == fString {
				f.input.SetValue(m.editOriginal)
			}
			m.editing = false
			m.input.Blur()
			m.refreshItems()
			return nil
		case k == "enter":
			if f.fType == fString {
				f.input.SetValue(m.input.Value())
			}
			m.editing = false
			m.input.Blur()
			m.refreshItems()
			return nil
		}
		switch f.fType {
		case fString:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return cmd
		case fSelect:
			switch k {
			case "j", "down", "k", "up", "left", "right":
				f.optIdx = (f.optIdx + 1) % len(f.opts)
			}
			m.refreshItems()
			return nil
		}
		return nil
	}

	// 历史 tab：回车复制
	if m.tab == tabHistory {
		switch {
		case key.Matches(msg, keys.Enter):
			items := voice.History()
			i := m.list.Index()
			if i >= 0 && i < len(items) {
				if err := voice.CopyHistoryEntry(i); err != nil {
					m.logf("❌ 复制失败: %s", err)
				} else {
					m.logf("📋 已复制: %s", truncateText(items[i].Text, 60))
				}
			}
			return nil
		case key.Matches(msg, keys.Left):
			m.switchTab((tabHistory + tabID(len(tabNames)) - 1) % tabID(len(tabNames)))
			return nil
		case key.Matches(msg, keys.Right):
			m.switchTab((tabHistory + 1) % tabID(len(tabNames)))
			return nil
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return cmd
	}

	// 导航态
	switch {
	case key.Matches(msg, keys.Quit):
		return tea.Quit
	case key.Matches(msg, keys.Save):
		m.save()
		return nil
	case key.Matches(msg, keys.Left):
		m.switchTab((m.tab + tabID(len(tabNames)) - 1) % tabID(len(tabNames)))
		return nil
	case key.Matches(msg, keys.Right):
		m.switchTab((m.tab + 1) % tabID(len(tabNames)))
		return nil
	case key.Matches(msg, keys.Logs):
		m.logExpanded = !m.logExpanded
		return nil
	case len(k) == 1 && k[0] >= '1' && k[0] <= '5':
		m.switchTab(tabID(k[0] - '1'))
		return nil
	case key.Matches(msg, keys.Enter):
		f := m.selectedField()
		if f == nil {
			return nil
		}
		switch f.fType {
		case fAction:
			m.triggerAction(f.key)
		case fToggle:
			f.boolVal = !f.boolVal
			m.refreshItems()
		case fString:
			m.editing = true
			m.editOriginal = f.input.Value()
			m.input.SetValue(f.input.Value())
			m.input.Focus()
		case fSelect:
			m.editing = true
		}
		return nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return cmd
}

func (m *Model) switchTab(t tabID) {
	if t == m.tab {
		return
	}
	m.tab = t
	m.editing = false
	m.input.Blur()
	m.refreshItems()
}

// triggerAction 处理 fAction 入口：打开对应弹层/面板。
func (m *Model) triggerAction(key string) {
	switch key {
	case "engine":
		m.openEnginePicker()
	case "channel_config":
		m.openChannelConfig()
	case "mic_test":
		voice.StartMicTest()
		m.micPanel = &micPanelState{}
	}
}

// currentEngine 返回当前选中的引擎。
func (m *Model) currentEngine() string {
	if m.cfg != nil && m.cfg.Voice.Engine != "" {
		return m.cfg.Voice.Engine
	}
	return asrcfg.DefaultEngine
}

// ---- 保存 ----

// applyFieldsTo 把所有 tab 的设置字段写回目标配置（纯内存，供 save 与测试使用）。
func (m *Model) applyFieldsTo(next *config.Config) {
	vc := &next.Voice
	for _, fs := range m.tabField {
		for _, f := range fs {
			switch f.key {
			case "enabled":
				vc.Enabled = f.boolVal
			case "push_to_talk":
				vc.PushToTalk = f.input.Value()
			case "mode":
				vc.Mode = f.selectValue()
			case "language":
				vc.Language = f.input.Value()
			case "auto_submit":
				vc.AutoSubmit = f.boolVal
			case "omit_trailing_punct":
				vc.OmitTrailingPunct = f.boolVal
			case "omit_trailing_all":
				vc.OmitTrailingAll = f.boolVal
			case "omit_trailing_ascii":
				vc.OmitTrailingASCII = f.boolVal
			case "stop_delay_ms":
				fmt.Sscanf(f.input.Value(), "%d", &vc.StopDelayMs)
			case "gain":
				fmt.Sscanf(f.input.Value(), "%d", &vc.Gain)
				if vc.Gain < 1 {
					vc.Gain = 1
				}
			case "auto_gain":
				vc.AutoGain = f.boolVal
			}
		}
	}
}

func (m *Model) save() {
	next := *m.cfg
	m.applyFieldsTo(&next)
	vc := &next.Voice
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
	for _, fs := range m.tabField {
		for i := range fs {
			if fs[i].key == "push_to_talk" {
				fs[i].input.SetValue(value)
				break
			}
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

// ---- 渲染 ----

func (m *Model) View() string {
	if !m.ready {
		return "loading..."
	}
	m.refreshItems()
	base := m.renderBase()
	if m.picker == nil && m.configOverlay == nil && m.micPanel == nil {
		return base
	}
	var overlay string
	switch {
	case m.picker != nil:
		overlay = m.renderPicker()
	case m.configOverlay != nil:
		overlay = m.renderConfigOverlay()
	case m.micPanel != nil:
		overlay = m.renderMicPanel()
	}
	return overlayOn(base, overlay, m.h)
}

// renderBase 渲染主界面（不含模态弹层）。
func (m *Model) renderBase() string {
	var b strings.Builder
	b.WriteString(tStyle.Render("🎙️ 🗣️ Just Talk Plus") + "\n")
	b.WriteString(m.tabBar() + "\n")
	b.WriteString(lStyle.Render("── 状态 ──") + "\n")
	b.WriteString("  " + m.renderVoiceStatus() + "\n")
	b.WriteString("  " + m.renderVoiceStats() + "\n\n")

	if m.editing {
		if f := m.selectedField(); f != nil {
			b.WriteString(lStyle.Render(f.label+": ") + "\n")
			b.WriteString("  " + m.input.View() + "\n")
			b.WriteString(dStyle.Render("  enter 确认 · esc 取消") + "\n")
		}
	} else if m.tab == tabHistory && len(voice.History()) == 0 {
		// 历史为空：多行中文提示（触发条件 + 内存特性）
		b.WriteString(m.renderHistoryEmpty())
	} else {
		b.WriteString(m.list.View() + "\n")
	}

	if m.showLogs {
		maxLogs := 3
		if m.logExpanded {
			maxLogs = 100
		}
		s := 0
		if len(m.logs) > maxLogs {
			s = len(m.logs) - maxLogs
		}
		if s < len(m.logs) || len(voice.TUILogBuf) > 0 {
			b.WriteString(lStyle.Render("── 日志 (L 展开) ──") + "\n")
			for _, l := range m.logs[s:] {
				b.WriteString("  " + dStyle.Render(l) + "\n")
			}
			sv := 0
			if len(voice.TUILogBuf) > maxLogs {
				sv = len(voice.TUILogBuf) - maxLogs
			}
			for _, l := range voice.TUILogBuf[sv:] {
				b.WriteString("  " + dStyle.Render(l) + "\n")
			}
		}
	}

	b.WriteString(m.help.View(keys) + "\n")
	return b.String()
}

// renderHistoryEmpty 历史为空时的提示：说明何时会出现内容，以及仅存内存不持久化。
func (m *Model) renderHistoryEmpty() string {
	msg := strings.Join([]string{
		"  暂无识别结果",
		"",
		"  完成一次语音识别（上屏或复制到剪贴板）后，",
		"  结果会显示在这里，最多保留最近 10 条。",
		"",
		"  历史仅保存在内存中：",
		"  退出 Just Talk Plus 后会清空，不会保留到下次启动。",
	}, "\n")
	return dStyle.Render(msg) + "\n"
}

// overlayOn 把模态浮层垂直居中覆盖在 base 之上：用浮层行替换主内容对应行，
// 主内容整体保持原位（不因弹层出现而被挤高）。
func overlayOn(base, overlay string, screenH int) string {
	baseLines := strings.Split(base, "\n")
	ovLines := strings.Split(overlay, "\n")
	if screenH > 0 {
		for len(baseLines) < screenH {
			baseLines = append(baseLines, "")
		}
	}
	start := 0
	if screenH > len(ovLines) {
		start = (screenH - len(ovLines)) / 2
	}
	for i, line := range ovLines {
		idx := start + i
		if idx < 0 || idx >= len(baseLines) {
			break
		}
		baseLines[idx] = line
	}
	return strings.Join(baseLines, "\n")
}

// tabBar 渲染 tab 分组栏（bubbles 无 tab 组件，手写；当前 tab 高亮且保留数字键提示）。
func (m *Model) tabBar() string {
	base := lipgloss.NewStyle().Padding(0, 1)
	active := base.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	inactive := base.Foreground(lipgloss.Color("8"))
	var b strings.Builder
	b.WriteString("  ")
	for i, t := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, t)
		if tabID(i) == m.tab {
			b.WriteString(active.Render(label))
		} else {
			b.WriteString(inactive.Render(label))
		}
		b.WriteString(" ")
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

func (m *Model) logf(format string, args ...interface{}) {
	if !m.showLogs {
		return
	}
	m.logs = append(m.logs, fmt.Sprintf(format, args...))
}

// ---- 渠道选择弹层 ----

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

func (m *Model) selectEngine(name string) {
	m.picker = nil
	m.cfg.Voice.Engine = name
	m.refreshItems()
	m.logf("引擎已切换为 %s", name)
}

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
		lines = append(lines, subStyle.Render("    "+it.subtitle))
	}
	lines = append(lines, "")
	lines = append(lines, " "+dStyle.Render("j/k 选择  回车确认  esc 关闭"))

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

// ---- 渠道配置二级弹层 ----

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

func (m *Model) handleConfigOverlayKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	ov := m.configOverlay
	if ov == nil {
		return nil
	}
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
			case "j", "down", "k", "up", "left", "right":
				f.optIdx = (f.optIdx + 1) % len(f.opts)
			}
		case fString:
			var cmd tea.Cmd
			f.input, cmd = f.input.Update(msg)
			return cmd
		}
		return nil
	}
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

func (m *Model) saveChannelConfig() {
	m.applyConfigOverlay()
	if err := config.Save(m.cfg); err != nil {
		m.logf("❌ 渠道配置保存失败: %s", err)
	} else {
		m.logf("✅ 渠道配置已保存")
	}
}

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

func (m *Model) renderConfigOverlay() string {
	ov := m.configOverlay
	if ov == nil {
		return ""
	}
	width := 72
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
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
