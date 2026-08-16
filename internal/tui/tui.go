// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file was modified from the original Just Talk by whoamihappyhacking
// (GPL-3.0). See NOTICE for maintenance history.

// Package tui 提供 Just Talk Plus 的 Bubble Tea 配置界面。
//
// 布局为 tab 分组（通用/引擎/麦克风/输出/历史），每个 tab 内是单行字段表单
// （label: value），垂直高度只取决于单组字段数。渠道选择、引擎配置、相关链接、
// 麦克风调试为模态弹层，屏幕居中覆盖主内容（保留标题与 tab 行可见）。
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
	hStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
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

// ---- 弹层状态 ----

// pickerItem 渠道选择弹层中的一条候选，subtitle 说明该渠道特点。
type pickerItem struct {
	name     string
	subtitle string
}

// docLink 引擎文档/相关链接：相关链接弹层内浏览，回车用系统浏览器打开。
// 与渠道选择弹层分开维护，避免长 URL 挤占候选排版。
type docLink struct {
	name string
	url  string
}

// engineLinks 各渠道的可选链接（与 README / doctor 文案保持一致）。
var engineLinks = map[string][]docLink{
	"doubao-stream": {
		{name: "豆包官方文档", url: "https://docs.volcengine.com/docs/6561/1395846?lang=zh"},
		{name: "豆包新版控制台", url: "https://console-embedded.volcengine.com/speech/new/setting/activate"},
	},
	"dashscope": {
		{name: "千问实时语音识别文档", url: "https://platform.qianwenai.com/docs/developer-guides/speech/asr-realtime"},
		// 模型库属于千问平台（qwen3-asr-flash-realtime 等模型），归 dashscope 名下，不标 OpenAI
		{name: "千问实时 ASR 模型库", url: "https://www.qianwenai.com/models?search=asr"},
	},
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
		subtitle: "火山引擎流式 ASR，边录边出字，支持热词；选定后可在引擎配置中选择模型版本（1.0 / 2.0）",
	},
	{
		name:     "auralwise",
		subtitle: "异步任务转写，整段识别",
	},
	{
		name:     "openai",
		subtitle: "兼容 Whisper 接口，可指向本地 / 第三方端点",
	},
	{
		name:     "dashscope",
		subtitle: "千问 Fun-ASR 实时流式，边录边出字；8k 模型自动降采样",
	},
}

// configOverlayState 引擎配置二级弹层；nil 表示未打开。
type configOverlayState struct {
	title   string
	fields  []field
	cursor  int
	editing bool
}

// linkPickerState 「相关链接」弹层；nil 表示未打开。
type linkPickerState struct {
	title string
	items []docLink
	idx   int
}

// micPanelState 麦克风调试面板；nil 表示未打开。
type micPanelState struct{}

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
	cursor   int
	editing  bool

	picker        *pickerState
	configOverlay *configOverlayState
	linkPicker    *linkPickerState
	micPanel      *micPanelState
}

func New(cfg *config.Config) *Model {
	return &Model{
		cfg:      cfg,
		tabField: buildTabFields(cfg),
		logs:     make([]string, 0, 100),
		showLogs: true,
		tab:      tabGeneral,
		cursor:   0,
	}
}

// ---- 字段构建（按 tab 分组）----

// buildTabFields 按 tab 分组构建全部设置字段。
func buildTabFields(cfg *config.Config) map[tabID][]field {
	vc := cfg.Voice
	ti := func(v string) textinput.Model { t := textinput.New(); t.SetValue(v); t.Cursor.Blink = false; return t }

	general := []field{
		{label: "语音输入", key: "enabled", help: "关闭后不注册热键", fType: fToggle, boolVal: vc.Enabled},
		{label: "热键", key: "push_to_talk", help: "例: Alt+Super / F9；不支持字母数字等普通字符键", fType: fString, input: ti(vc.PushToTalk)},
		{label: "模式", key: "mode", help: "toggle 切换 / hold 按住", fType: fSelect, opts: []string{"toggle", "hold"}, optIdx: idxOf([]string{"toggle", "hold"}, vc.Mode)},
		{label: "语言", key: "language", help: "BCP-47，如 zh-CN；空 = 自动", fType: fString, input: ti(vc.Language)},
		{label: "自动上屏", key: "auto_submit", help: "识别后自动粘贴", fType: fToggle, boolVal: vc.AutoSubmit},
		{label: "停止延迟(ms)", key: "stop_delay_ms", help: "松手后补录毫秒", fType: fString, input: ti(fmt.Sprintf("%d", vc.StopDelayMs))},
	}

	engineTab := []field{
		{label: "引擎", key: "engine", help: "回车选择渠道；doubao-stream / dashscope 流式，auralwise / openai 批处理", fType: fAction},
		{label: "引擎配置", key: "engine_config", help: "回车进入 [引擎名] 引擎配置（凭据、模型等，各引擎独立保存）", fType: fAction},
		{label: "相关链接", key: "doc_links", help: "回车查看全部引擎的文档与链接，回车浏览器打开", fType: fAction},
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

	return map[tabID][]field{
		tabGeneral: general,
		tabEngine:  engineTab,
		tabMic:     mic,
		tabOutput:  output,
	}
}

// channelConfigFields 构建「引擎配置」弹层里的字段：凭据 + 该引擎的全部专属设置。
// 各引擎配置独立保存于 engine_configs[engine]，互不覆盖。
func channelConfigFields(vc config.VoiceConfig, ti func(string) textinput.Model) []field {
	engine := asrcfg.EngineType(vc)
	ec := vc.EngineConfigFor(engine)
	if ec == nil {
		ec = map[string]string{}
	}
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

	var fields []field
	switch engine {
	case "auralwise":
		fields = []field{
			{label: "API Key", key: "ec.api_key", help: "必填（asr_…）", fType: fString, input: ti(ec["api_key"])},
			{label: "Base URL", key: "ec.base_url", help: "默认 https://api.auralwise.cn/v1", fType: fString, input: ti(ec["base_url"])},
			{label: "Optimize", key: "ec.optimize", help: "auto | true | false", fType: fSelect, opts: []string{"auto", "true", "false"}, optIdx: idxOf([]string{"auto", "true", "false"}, def(ec["optimize"], "auto"))},
		}
	case "openai":
		fields = []field{
			{label: "API Key", key: "ec.api_key", help: "必填（sk-…）", fType: fString, input: ti(ec["api_key"])},
			{label: "Base URL", key: "ec.base_url", help: "默认 https://api.openai.com/v1", fType: fString, input: ti(ec["base_url"])},
			{label: "模型", key: "ec.model", help: "默认 whisper-1", fType: fString, input: ti(ec["model"])},
		}
	case "dashscope":
		rateOpts := []string{"16000", "8000"}
		rateDefault := "16000"
		if strings.Contains(def(ec["model"], "fun-asr-realtime"), "8k") {
			rateDefault = "8000"
		}
		fields = []field{
			{label: "API Key", key: "ec.api_key", help: "必填（sk-…，DASHSCOPE_API_KEY）", fType: fString, input: ti(ec["api_key"])},
			{label: "模型", key: "ec.model", help: "默认 fun-asr-realtime；8k 模型自动降采样", fType: fString, input: ti(ec["model"])},
			{label: "采样率", key: "ec.sample_rate", help: "8k 模型须为 8000", fType: fSelect, opts: rateOpts, optIdx: idxOf(rateOpts, def(ec["sample_rate"], rateDefault))},
			{label: "Base URL", key: "ec.base_url", help: "默认 wss://dashscope.aliyuncs.com/api-ws/v1/inference/", fType: fString, input: ti(ec["base_url"])},
			{label: "语言", key: "ec.language", help: "BCP-47，如 zh-CN；空 = 自动", fType: fString, input: ti(ec["language"])},
		}
	default: // doubao-stream
		mvOpts := []string{"1.0 (bigasr)", "2.0 (seedasr)"}
		mvVals := []string{"1.0", "2.0"}
		smOpts := []string{"async", "nostream"}
		mvDefault := "1.0"
		if ec["api_key"] != "" {
			mvDefault = "2.0"
		}
		fields = []field{
			{label: "模型版本", key: "ec.model_version", help: "1.0=bigasr 旧资源 / 2.0=seedasr 新资源", fType: fSelect, opts: mvOpts, optVals: mvVals, optIdx: idxOf(mvVals, def(ec["model_version"], mvDefault))},
			{label: "流式模式", key: "ec.stream_mode", help: "默认 async", fType: fSelect, opts: smOpts, optIdx: idxOf(smOpts, def(ec["stream_mode"], "async"))},
			{label: "API Key(新版)", key: "ec.api_key", help: "新版控制台 X-Api-Key；与下方旧版凭据二选一", fType: fString, input: ti(ec["api_key"])},
			{label: "App ID(旧版)", key: "ec.app_id", help: "旧版控制台 App ID（无 api_key 时使用）", fType: fString, input: ti(get("app_id"))},
			{label: "Access Key(旧版)", key: "ec.access_key", help: "旧版 Access Token", fType: fString, input: ti(get("access_key"))},
			{label: "Base Host", key: "ec.base_host", help: "默认 openspeech.bytedance.com", fType: fString, input: ti(ec["base_host"])},
			{label: "热词", key: "hotwords", help: "逗号分隔术语", fType: fString, input: ti(strings.Join(vc.Hotwords, ", "))},
		}
	}
	return fields
}

func idxOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return 0
}

// currentFields 返回当前 tab 的设置字段（历史 tab 返回 nil）。
func (m *Model) currentFields() []field {
	if m.tab == tabHistory {
		return nil
	}
	return m.tabField[m.tab]
}

// currentEngine 返回当前选中的引擎。
func (m *Model) currentEngine() string {
	if m.cfg != nil && m.cfg.Voice.Engine != "" {
		return m.cfg.Voice.Engine
	}
	return asrcfg.DefaultEngine
}

// ---- 生命周期 ----

func (m *Model) SetDebug(debug bool) { m.debug = debug }

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
	case linkOpenFailedMsg:
		m.logf("❌ 打开链接失败: %s", msg.err)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// 弹层/面板优先
	if m.linkPicker != nil {
		return m.handleLinkPickerKey(k)
	}
	if m.picker != nil {
		return m.handlePickerKey(k)
	}
	if m.configOverlay != nil {
		return m.handleConfigOverlayKey(msg)
	}
	if m.micPanel != nil {
		return m.handleMicPanelKey(k)
	}

	// 历史 tab：列表选择与复制
	if m.tab == tabHistory {
		hist := voice.History()
		switch k {
		case "j", "down":
			if m.cursor < len(hist)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(hist) {
				if err := voice.CopyHistoryEntry(m.cursor); err != nil {
					m.logf("❌ 复制失败: %s", err)
				} else {
					m.logf("📋 已复制: %s", truncateText(hist[m.cursor].Text, 60))
				}
			}
		case "h", "left":
			m.switchTab((tabHistory + tabID(len(tabNames)) - 1) % tabID(len(tabNames)))
		case "l", "right":
			m.switchTab((tabHistory + 1) % tabID(len(tabNames)))
		case "q", "ctrl+c":
			return tea.Quit
		case "s":
			m.save()
		case "L":
			m.logExpanded = !m.logExpanded
		}
		return nil
	}

	// 设置 tab：编辑态
	fs := m.currentFields()
	if m.editing && m.cursor >= 0 && m.cursor < len(fs) {
		f := &fs[m.cursor]
		switch k {
		case "esc":
			m.editing = false
			f.input.Blur()
			return nil
		case "enter":
			m.editing = false
			f.input.Blur()
			return nil
		}
		switch f.fType {
		case fSelect:
			switch k {
			case "j", "down", "k", "up", "left", "right":
				f.optIdx = (f.optIdx + 1) % len(f.opts)
			}
		case fToggle:
			switch k {
			case " ":
				f.boolVal = !f.boolVal
			case "j", "down", "k", "up":
				m.editing = false
				m.cursor++
				if m.cursor >= len(fs) {
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

	// 导航态
	switch {
	case k == "q" || k == "ctrl+c":
		return tea.Quit
	case k == "s":
		m.save()
		return nil
	case k == "e" || k == "i" || k == "enter":
		if len(fs) == 0 {
			return nil
		}
		f := &fs[m.cursor]
		switch f.fType {
		case fAction:
			m.triggerAction(f.key)
		case fToggle:
			f.boolVal = !f.boolVal
		default: // fString / fSelect
			m.editing = true
			if f.fType == fString {
				f.input.Focus()
			}
		}
	case k == "j" || k == "down":
		if m.cursor < len(fs)-1 {
			m.cursor++
		}
	case k == "k" || k == "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case k == "h" || k == "left":
		m.switchTab((m.tab + tabID(len(tabNames)) - 1) % tabID(len(tabNames)))
	case k == "l" || k == "right":
		m.switchTab((m.tab + 1) % tabID(len(tabNames)))
	case k == "L":
		m.logExpanded = !m.logExpanded
	case len(k) == 1 && k[0] >= '1' && k[0] <= '5':
		m.switchTab(tabID(k[0] - '1'))
	}
	return nil
}

func (m *Model) switchTab(t tabID) {
	if t == m.tab {
		return
	}
	m.tab = t
	m.editing = false
	m.cursor = 0
	if fs := m.currentFields(); len(fs) > 0 {
		// 保持光标在合法范围
	}
}

// triggerAction 处理 fAction 入口：打开对应弹层/面板。
func (m *Model) triggerAction(key string) {
	switch key {
	case "engine":
		m.openEnginePicker()
	case "engine_config":
		m.openChannelConfig()
	case "mic_test":
		voice.StartMicTest()
		m.micPanel = &micPanelState{}
	case "doc_links":
		m.openLinkPicker()
	}
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
	base := m.renderBase()
	if m.picker == nil && m.configOverlay == nil && m.linkPicker == nil && m.micPanel == nil {
		// 无弹层：键位帮助行固定在屏幕最底部
		return pinBottom(base, m.helpLine(), m.h)
	}
	var overlay string
	switch {
	case m.picker != nil:
		overlay = m.renderPicker()
	case m.linkPicker != nil:
		overlay = m.renderLinkPicker()
	case m.configOverlay != nil:
		overlay = m.renderConfigOverlay()
	case m.micPanel != nil:
		overlay = m.renderMicPanel()
	}
	// 有弹层：弹层为模态，覆盖主内容（含帮助行），自含按键提示
	return overlayOn(base, overlay, m.h)
}

// helpLine 底部键位帮助行（固定显示在屏幕最后一行）。
// 导航遵循 vim 规范：h 左 / j 下 / k 上 / l 右；方向键等效。
func (m *Model) helpLine() string {
	return hStyle.Render("  j/k 或 ↑/↓ 选择 · h/l 或 ←/→ 分组 · 1-5 分组 · e 编辑 · s 保存 · L 日志 · q 退出")
}

// renderBase 渲染主界面（不含模态弹层与底部帮助行）。
func (m *Model) renderBase() string {
	var b strings.Builder
	b.WriteString(tStyle.Render("🎙️ 🗣️ Just Talk Plus") + "\n")
	b.WriteString(m.tabBar() + "\n")
	b.WriteString(lStyle.Render("── 状态 ──") + "\n")
	b.WriteString("  " + m.renderVoiceStatus() + "\n")
	b.WriteString("  " + m.renderVoiceStats() + "\n\n")

	if m.tab == tabHistory {
		b.WriteString(m.renderHistory())
	} else {
		b.WriteString(m.renderFields())
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
	return b.String()
}

// pinBottom 把 footer 固定在屏幕最后一行：主内容补空行到 screenH-1 行，
// 最后一行写 footer。内容超出时优先保留 footer（超出的内容被终端截断）。
func pinBottom(base, footer string, screenH int) string {
	base = strings.TrimSuffix(base, "\n")
	lines := strings.Split(base, "\n")
	if screenH > 0 {
		contentMax := screenH - 1
		if len(lines) > contentMax {
			lines = lines[:contentMax]
		}
		for len(lines) < contentMax {
			lines = append(lines, "")
		}
		lines = append(lines, footer)
	}
	return strings.Join(lines, "\n")
}

// renderFields 渲染当前 tab 的设置字段（单行 label: value）。
func (m *Model) renderFields() string {
	fs := m.currentFields()
	var b strings.Builder
	for i, f := range fs {
		marker := "  "
		if i == m.cursor {
			if m.editing {
				marker = aStyle.Render("▶ ")
			} else {
				marker = aStyle.Render("▸ ")
			}
		}
		line := marker + strings.Repeat("  ", f.indent) + lStyle.Render(f.label+": ") + m.renderField(i, f)
		b.WriteString(line + "\n")
	}
	return b.String()
}

// renderHistory 渲染历史 tab：空提示或可复制的条目列表。
func (m *Model) renderHistory() string {
	hist := voice.History()
	if len(hist) == 0 {
		return m.renderHistoryEmpty()
	}
	if m.cursor >= len(hist) {
		m.cursor = len(hist) - 1
	}
	var b strings.Builder
	for i, e := range hist {
		marker := "  "
		if i == m.cursor {
			marker = aStyle.Render("▸ ")
		}
		text := truncateText(e.Text, 60)
		ts := e.Time.Format("15:04:05")
		b.WriteString(marker + vStyle.Render(text) + "  " + dStyle.Render(ts) + "\n")
	}
	b.WriteString(dStyle.Render("  j/k 或 ↑/↓ 选择 · enter 复制到剪贴板") + "\n")
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

// renderField 渲染单个字段的值（单行样式）。
func (m *Model) renderField(i int, f field) string {
	editing := m.editing && m.cursor == i
	switch f.fType {
	case fString:
		v := f.input.Value()
		if v == "" {
			return dStyle.Render("（空）")
		}
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
		extra := ""
		if f.key == "mic_test" {
			extra = "  " + dStyle.Render("增益:") + " " + wStyle.Render(fmt.Sprintf("%d", voice.GetMicGain()))
		} else if f.key == "engine" {
			extra = "  " + vStyle.Render(m.currentEngine())
		}
		return aStyle.Render("▶ ") + vStyle.Render("进入") + extra
	}
	return ""
}

// tabBar 渲染 tab 分组栏（当前 tab 高亮且保留数字键提示）。
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

// overlayOn 把模态浮层垂直居中覆盖在 base 之上：用浮层行替换主内容对应行，
// 主内容整体保持原位（不因弹层出现而被挤高）。
// 弹层高度受限（最多 screenH-3 行），起始行保底在标题/tab 之后，避免盖掉界面头部。
func overlayOn(base, overlay string, screenH int) string {
	base = strings.TrimSuffix(base, "\n")
	baseLines := strings.Split(base, "\n")
	ovLines := strings.Split(overlay, "\n")
	if screenH > 0 {
		for len(baseLines) < screenH {
			baseLines = append(baseLines, "")
		}
		maxOv := screenH - 3
		if maxOv > 0 && len(ovLines) > maxOv {
			ovLines = ovLines[:maxOv]
		}
	}
	start := 0
	if screenH > len(ovLines) {
		start = (screenH - len(ovLines)) / 2
	}
	if start < 2 {
		start = 2 // 保底：标题行 + tab 行可见
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
	m.logf("引擎已切换为 %s", name)
}

func (m *Model) renderPicker() string {
	p := m.picker
	if p == nil {
		return ""
	}
	width := 80
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
	lines = append(lines, " "+dStyle.Render("j/k 或 ↑/↓ 选择  回车确认  esc 关闭"))

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

// ---- 相关链接子页面 ----

// allDocLinks 汇总全部引擎的链接，供「相关链接」弹层展示（不依赖当前所选引擎）。
func allDocLinks() []docLink {
	var links []docLink
	for _, name := range asrcfg.Types() {
		for _, l := range engineLinks[name] {
			links = append(links, docLink{name: fmt.Sprintf("[%s] %s", name, l.name), url: l.url})
		}
	}
	return links
}

// openLinkPicker 打开「相关链接」弹层：展示全部引擎的文档链接，回车浏览器打开。
func (m *Model) openLinkPicker() {
	links := allDocLinks()
	if len(links) == 0 {
		return
	}
	m.linkPicker = &linkPickerState{
		title: "相关链接",
		items: links,
		idx:   0,
	}
}

func (m *Model) handleLinkPickerKey(k string) tea.Cmd {
	p := m.linkPicker
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
		item := p.items[p.idx]
		m.logf("🔗 正在打开: %s", item.name)
		return openLinkCmd(item.url)
	case "esc", "q", "ctrl+c":
		m.linkPicker = nil
	}
	return nil
}

// openLinkCmd 用系统默认浏览器打开链接（异步，不阻塞 TUI）。
func openLinkCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if err := openURL(url); err != nil {
			return linkOpenFailedMsg{url: url, err: err}
		}
		return nil
	}
}

// linkOpenFailedMsg 打开链接失败的消息。
type linkOpenFailedMsg struct {
	url string
	err error
}

func (m *Model) renderLinkPicker() string {
	p := m.linkPicker
	if p == nil {
		return ""
	}
	width := 72
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
		lines = append(lines, subStyle.Render("    "+it.url))
	}
	lines = append(lines, "")
	lines = append(lines, " "+dStyle.Render("j/k 或 ↑/↓ 选择  回车浏览器打开  esc 返回"))

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

// ---- 引擎配置二级弹层 ----

func (m *Model) openChannelConfig() {
	engine := m.currentEngine()
	vc := m.cfg.Voice
	vc.Engine = engine
	ti := func(v string) textinput.Model { t := textinput.New(); t.SetValue(v); t.Cursor.Blink = false; return t }
	m.configOverlay = &configOverlayState{
		title:  fmt.Sprintf("[%s] 引擎配置", engine),
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
			case "j", "down", "k", "up", "h", "l", "left", "right":
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
		if ov.fields[ov.cursor].fType == fAction {
			m.triggerAction(ov.fields[ov.cursor].key)
			return nil
		}
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
		m.logf("❌ 引擎配置保存失败: %s", err)
	} else {
		m.logf("✅ 引擎配置已保存")
	}
}

func (m *Model) applyConfigOverlay() {
	ov := m.configOverlay
	if ov == nil {
		return
	}
	vc := &m.cfg.Voice
	engine := asrcfg.EngineType(*vc)
	ec := vc.EngineConfigs[engine]
	if ec == nil {
		ec = map[string]string{}
		if vc.EngineConfigs == nil {
			vc.EngineConfigs = map[string]map[string]string{}
		}
		vc.EngineConfigs[engine] = ec
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
				delete(ec, name)
			} else {
				ec[name] = val
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
	lines = append(lines, " "+dStyle.Render("j/k 或 ↑/↓ 导航  e 编辑  s 保存  esc 取消"))

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
	case fAction:
		return aStyle.Render("▶ 回车进入")
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
