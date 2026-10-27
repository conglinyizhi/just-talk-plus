// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file was modified from the original Just Talk by whoamihappyhacking
// (GPL-3.0). See NOTICE for maintenance history.

// Package tui 提供 Just Talk Plus 的 Bubble Tea 配置界面。
//
// 布局为 tab 分组（通用/引擎/输入/输出/历史），每个 tab 内是单行字段表单
// （label: value），垂直高度只取决于单组字段数。渠道选择、引擎配置、相关链接、
// 麦克风调试为模态弹层，屏幕居中覆盖主内容（保留标题与 tab 行可见）。
package tui

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
	"github.com/conglinyizhi/just-talk-plus/internal/diag"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

var (
	tStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	configTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")) // 黄
	lStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	vStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	aStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	wStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	eStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	dStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	hStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// 麦克风调试电平条：条体按位置分绿/黄/红三区，填充为当前电平高亮。
	barFill = [3]lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),  // 绿
		lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true), // 黄
		lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),  // 红
	}
	barEmpty = [3]lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Faint(true), // 暗绿
		lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Faint(true), // 暗黄
		lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Faint(true), // 暗红
	}
	barHold = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
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
	// actionLabel fAction 的操作按钮文字：默认「进入」（打开弹层/面板）；
	// 纯动作（开始采集、生成报告）应设为对应动词，避免与弹层行为混淆。
	actionLabel string
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
	tabOverlay
)

var tabNames = []string{"通用", "引擎", "输入", "输出", "历史", "显示"}

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
	"mimo-asr": {
		{name: "MiMo 语音识别文档", url: "https://mimo.mi.com/docs/zh-CN/api/audio/Speech-Recognition"},
		{name: "MiMo 按量付费价格", url: "https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go"},
	},
	"siliconflow": {
		{name: "硅基流动语音转写文档", url: "https://docs.siliconflow.cn/docs/api/audio-transcriptions-post"},
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
	{
		name:     "mimo-asr",
		subtitle: "小米 MiMo 语音识别，整段识别；价格见官网",
	},
	{
		name:     "siliconflow",
		subtitle: "硅基流动语音转写，整段识别；模型可在引擎配置中自行更换",
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
// devIdx 当前设备在 m.devices 中的下标；-1 = 自动（系统默认）。
// androidSlot 当前是否为 Android 设备（内嵌 AndroidMic 服务端）档。
type micPanelState struct {
	devIdx      int
	androidSlot bool
}

type historyAction string

const (
	historyCopy   historyAction = "copy"
	historyRetry  historyAction = "retry"
	historyPlay   historyAction = "play"
	historyReveal historyAction = "reveal"
)

type historyMenuState struct {
	entry      voice.HistoryEntry
	entryIndex int // 该条目在 History() 中的下标（0 = 最新），用于菜单内显示序号
	idx        int
	actions    []historyAction
}

// ---- 主 Model ----

type Model struct {
	w, h                int
	ready               bool
	info                hotkey.ProviderInfo
	logs                []string
	devices             []string
	cfg                 *config.Config
	debug               bool
	showLogs            bool
	logExpanded         bool
	diagnostics         *diag.MemoryBuffer
	OnSave              func(*config.Config) error
	OnDiagnosticsChange func(enabled bool)

	tab      tabID
	tabField map[tabID][]field
	cursor   int
	editing  bool

	picker                  *pickerState
	configOverlay           *configOverlayState
	linkPicker              *linkPickerState
	micPanel                *micPanelState
	historyMenu             *historyMenuState
	androidPanel            bool // Android 虚拟麦克风服务端管理面板
	noiseCalibrationVersion uint64
	configOnly              bool // 配置模式：不录音/不开服务，只编辑配置
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

	diagnosticOptions := []string{"关闭", "本次运行", "后续 3 次启动", "持续开启"}
	diagnosticOptionIndex := 0
	switch {
	case cfg.Diagnostics.StartupRuns == config.DiagnosticsAlways:
		diagnosticOptionIndex = 3
	case cfg.Diagnostics.StartupRuns > 0:
		diagnosticOptionIndex = 2
	case cfg.Diagnostics.Active:
		diagnosticOptionIndex = 1
	}

	diagnosticLabel := "[高级] 诊断收集"
	if cfg.Diagnostics.Active {
		diagnosticLabel += "（当前运行中）"
	}

	general := []field{
		{label: "语音输入", key: "enabled", help: "关闭后不注册热键", fType: fToggle, boolVal: vc.Enabled},
		{label: "热键", key: "push_to_talk", help: "例: Alt+Super / F9；不支持字母数字等普通字符键", fType: fString, input: ti(vc.PushToTalk)},
		{label: "模式", key: "mode", help: "toggle 切换 / hold 按住", fType: fSelect, opts: []string{"toggle", "hold"}, optIdx: idxOf([]string{"toggle", "hold"}, vc.Mode)},
		{label: "语言", key: "language", help: "BCP-47，如 zh-CN；空 = 自动", fType: fString, input: ti(vc.Language)},
		{label: "停止延迟(ms)", key: "stop_delay_ms", help: "松手后补录毫秒", fType: fString, input: ti(fmt.Sprintf("%d", vc.StopDelayMs))},
		{label: "热键冷却(ms)", key: "hotkey_cooldown_ms", help: "一次录音结束后，冷却期内再按热键静默忽略，防误触秒开秒关；0 = 关闭", fType: fString, input: ti(fmt.Sprintf("%d", vc.HotkeyCooldownMs))},
		{label: "有声后停止", key: "stop_after_silence", help: "停止请求后，检测到有效人声会重置倒计时；静音满「停止延迟」才截断录音", fType: fToggle, boolVal: vc.StopAfterSilence},
		{label: diagnosticLabel, key: "diagnostics_startup_runs", help: "仅用于排查异常：在本机保存最近 512 KiB 日志，按 K 导出。默认关闭；选择次数后会在对应启动完成后自动关闭", fType: fSelect, opts: diagnosticOptions, optIdx: diagnosticOptionIndex},
		{label: "[高级] 调试插件", key: "debug_enabled", help: "调试用：注册调试热键并打印按键事件；日常无需开启", fType: fToggle, boolVal: cfg.Debug.Enabled},
		{label: "[高级] 调试热键", key: "debug_hotkeys", help: "逗号分隔多个热键（如 Alt+F8, F10）；按下时在终端打印热键事件", fType: fString, input: ti(strings.Join(cfg.Debug.Hotkeys, ", "))},
	}

	engineTab := []field{
		{label: "引擎", key: "engine", help: "回车选择渠道", fType: fAction},
		{label: "引擎配置", key: "engine_config", help: "回车进入当前引擎的配置；各引擎独立保存，互不冲突", fType: fAction},
		{label: "相关链接", key: "doc_links", help: "回车查看全部引擎的文档与链接，回车浏览器打开", fType: fAction},
	}

	mic := []field{
		{label: "Android 虚拟麦克风", key: "androidmic_panel", help: "内嵌 TCP 服务端：接收 Android 设备音频作为录音输入；可启动/停止/查看状态与连接", fType: fAction},
		{label: "启动时自动启用", key: "androidmic_auto_start", help: "开启后应用启动即自动监听服务端，手机随时可连；默认关闭，避免不知情时占用端口", fType: fToggle, boolVal: vc.AndroidMicAutoStart, indent: 1},
		{label: "麦克风调试", key: "mic_test", help: "回车进入；同一录音热键触发测试，实时电平与增益调整，可切换设备（含 Android 设备）", fType: fAction},
		{label: "自适应增益", key: "auto_gain", help: "爆音自动降低，安静后缓慢恢复", fType: fToggle, boolVal: vc.AutoGain},
		{label: "目标峰值", key: "auto_gain_target_peak", help: "AGC 输出目标峰值 0..1；留空 = 内置默认 0.42（宁小勿爆）；调高更响、调低更保守", fType: fString, input: ti(agcFloat(vc.AutoGainTargetPeak)), indent: 1},
		{label: "峰值上限", key: "auto_gain_max_peak", help: "输出峰值超过此值开始回撤增益；留空 = 默认 0.65", fType: fString, input: ti(agcFloat(vc.AutoGainMaxPeak)), indent: 1},
		{label: "每块最大升高", key: "auto_gain_max_rise", help: "每块（约 200ms）增益最大升高倍数；留空 = 默认 1.15", fType: fString, input: ti(agcFloat(vc.AutoGainMaxRise)), indent: 1},
		{label: "增益上限", key: "auto_gain_max_gain", help: "AGC 最大放大倍数；留空 = 默认 25（上限 100）", fType: fString, input: ti(agcInt(vc.AutoGainMaxGain)), indent: 1},
		{label: "噪声阈值", key: "noise_threshold", help: "底噪阈值（RMS）：开头超过它开始录音，结束持续低于它满停止延迟即停止", fType: fString, input: ti(fmt.Sprintf("%.4f", vc.NoiseThreshold))},
		{label: "自动底噪", key: "noise_floor_auto", help: "开启后停止判定的阈值随环境底噪自动漂移，无需手动校准", fType: fToggle, boolVal: vc.NoiseFloorAuto},
		{label: "判定容差", key: "noise_deadband", help: "停止判定时高于底噪阈值多少才算人声（默认 0.1）；调太大会把正常说话判成噪音，导致提前截断丢字", fType: fString, input: ti(fmt.Sprintf("%g", vc.NoiseDeadband))},
		{label: "保留录音文件", key: "save_recordings", help: "将成功识别时实际发往 ASR 的音频以 WAV 保留在系统运行时目录，最多随历史保留 50 条", fType: fToggle, boolVal: vc.SaveRecordings},
		{label: "检测当前底噪", key: "noise_calibrate", help: "采集约 3 秒环境底噪，自动计算并保存噪声阈值", fType: fAction, actionLabel: "开始"},
		{label: "生成诊断报告", key: "diagnose", help: "用最近一条有录音的历史生成参数诊断报告（含给大模型的分析 prompt）；只生成不改配置", fType: fAction, actionLabel: "生成"},
	}

	output := []field{
		{label: "句尾标点省略", key: "omit_trailing_punct", help: "上屏/复制前省略句尾句号「。」", fType: fToggle, boolVal: vc.OmitTrailingPunct},
		{label: "切断所有符号", key: "omit_trailing_all", help: "句尾所有标点一并省略（含英文）", fType: fToggle, boolVal: vc.OmitTrailingAll, indent: 1},
		{label: "英文符号 .!?", key: "omit_trailing_ascii", help: "额外省略英文半角 . ! ?", fType: fToggle, boolVal: vc.OmitTrailingASCII, indent: 1},
		{label: "删除中文间空格", key: "remove_cjk_space", help: "删中文↔中文间空格（如「你好 世界」→「你好世界」）；默认关；与「自动空格」互不影响", fType: fToggle, boolVal: vc.RemoveCJKSpace},
		{label: "中文排版自动空格", key: "auto_space", help: "中英/中数间补空格（如「K3刚发布」→「K3 刚发布」）", fType: fToggle, boolVal: vc.AutoSpace},
		{label: "自动上屏", key: "auto_submit", help: "识别后模拟粘贴到当前输入框", fType: fToggle, boolVal: vc.AutoSubmit},
		{label: "复制到剪贴板", key: "write_clipboard", help: "识别结果写入剪贴板（可与上屏同开）", fType: fToggle, boolVal: vc.WriteClipboard, indent: 1},
		{label: "上屏后还原剪贴板", key: "restore_clipboard", help: "上屏成功后还原原内容，尽量不污染剪贴板历史", fType: fToggle, boolVal: vc.RestoreClipboard, indent: 1},
		{label: "上屏失败不写入", key: "no_clipboard_on_fail", help: "上屏失败时也还原原内容，识别文本不留在剪贴板", fType: fToggle, boolVal: vc.NoClipboardOnFail, indent: 1},
	}

	overlay := []field{
		{label: "显示胶囊", key: "overlay_enabled", help: "录制状态悬浮胶囊（状态点 + 文字）", fType: fToggle, boolVal: cfg.Overlay.Enabled},
		{label: "位置", key: "overlay_position", help: "胶囊在屏幕上的停靠位置", fType: fSelect, opts: overlayPositions, optIdx: idxOf(overlayPositions, cfg.Overlay.Position)},
		{label: "空闲时可见", key: "overlay_idle_visible", help: "待机时也显示胶囊（更淡）", fType: fToggle, boolVal: cfg.Overlay.IdleVisible},
		{label: "缩放", key: "overlay_scale", help: "胶囊缩放比例，如 1.0 / 1.25 / 1.5", fType: fString, input: ti(fmt.Sprintf("%g", cfg.Overlay.Scale))},
	}

	return map[tabID][]field{
		tabGeneral: general,
		tabEngine:  engineTab,
		tabMic:     mic,
		tabOutput:  output,
		tabOverlay: overlay,
	}
}

// overlayPositions 胶囊支持的屏幕停靠位置（与各后端 anchor/margins 实现对应）。
var overlayPositions = []string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}

// agcFloat 把可选的 AGC 浮点参数转为显示文本；nil（走内置默认）显示为空。
func agcFloat(p *float64) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%g", *p)
}

// agcInt 把可选的 AGC 整数参数转为显示文本；nil（走内置默认）显示为空。
func agcInt(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
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
	bval := func(name, defv string) bool {
		v := strings.TrimSpace(ec[name])
		if v == "" {
			v = defv
		}
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
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
	case "mimo-asr":
		fields = []field{
			{label: "API Key", key: "ec.api_key", help: "必填（小米 MiMo API Key）", fType: fString, input: ti(ec["api_key"])},
			{label: "Base URL", key: "ec.base_url", help: "默认 https://api.xiaomimimo.com/v1", fType: fString, input: ti(ec["base_url"])},
			{label: "模型", key: "ec.model", help: "默认 mimo-v2.5-asr", fType: fString, input: ti(ec["model"])},
			{label: "语言", key: "ec.language", help: "auto / zh / en；空 = 自动", fType: fString, input: ti(ec["language"])},
		}
	case "siliconflow":
		fields = []field{
			{label: "API Key", key: "ec.api_key", help: "必填（账户 API Key）", fType: fString, input: ti(ec["api_key"])},
			{label: "Base URL", key: "ec.base_url", help: "默认 https://api.siliconflow.cn/v1", fType: fString, input: ti(ec["base_url"])},
			{label: "模型", key: "ec.model", help: "默认 FunAudioLLM/SenseVoiceSmall；也可填 TeleAI/TeleSpeechASR 等，以官网模型列表为准", fType: fString, input: ti(ec["model"])},
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
		smOpts := []string{"双向流式（低延迟，实时结果）", "单向流式（优先完整上下文）"}
		smVals := []string{"async", "nostream"}
		mvDefault := "1.0"
		if ec["api_key"] != "" {
			mvDefault = "2.0"
		}
		fields = []field{
			{label: "模型版本", key: "ec.model_version", help: "1.0=bigasr 旧资源 / 2.0=seedasr 新资源", fType: fSelect, opts: mvOpts, optVals: mvVals, optIdx: idxOf(mvVals, def(ec["model_version"], mvDefault))},
			{label: "识别传输模式", key: "ec.stream_mode", help: "双向：边录边出字、延迟低；单向：服务端在结束后结合完整音频给最终结果。二遍识别仅双向模式可用", fType: fSelect, opts: smOpts, optVals: smVals, optIdx: idxOf(smVals, def(ec["stream_mode"], "async"))},
			{label: "API Key(新版)", key: "ec.api_key", help: "新版控制台 X-Api-Key；与下方旧版凭据二选一", fType: fString, input: ti(ec["api_key"])},
			{label: "App ID(旧版)", key: "ec.app_id", help: "旧版控制台 App ID（无 api_key 时使用）", fType: fString, input: ti(get("app_id"))},
			{label: "Access Key(旧版)", key: "ec.access_key", help: "旧版 Access Token", fType: fString, input: ti(get("access_key"))},
			{label: "Base Host", key: "ec.base_host", help: "默认 openspeech.bytedance.com", fType: fString, input: ti(ec["base_host"])},
			{label: "热词", key: "hotwords", help: "逗号分隔术语", fType: fString, input: ti(strings.Join(vc.Hotwords, ", "))},
			{label: "中英文识别", key: "ec.enable_lid", help: "中英混说/方言识别；语言需为自动或 zh-CN", fType: fToggle, boolVal: bval("enable_lid", "false")},
			{label: "二遍识别", key: "ec.enable_nonstream", help: "分句二次识别，最终更准；仅流式模式=async 生效", fType: fToggle, boolVal: bval("enable_nonstream", "true")},
			{label: "数字规范化", key: "ec.enable_itn", help: "口语数字/金额转阿拉伯数字（一九七零年→1970年）", fType: fToggle, boolVal: bval("enable_itn", "true")},
			{label: "标点", key: "ec.enable_punc", help: "识别结果加逗号句号等标点", fType: fToggle, boolVal: bval("enable_punc", "true")},
			{label: "语义顺滑", key: "ec.enable_ddc", help: "删停顿词/语气词/重复词，文本更连贯", fType: fToggle, boolVal: bval("enable_ddc", "false")},
			{label: "热词表名", key: "ec.boosting_table_name", help: "控制台自学习平台创建的热词词表名", fType: fString, input: ti(ec["boosting_table_name"])},
			{label: "热词表ID", key: "ec.boosting_table_id", help: "热词词表ID；与表名不一致时以ID为准", fType: fString, input: ti(ec["boosting_table_id"])},
			{label: "替换词表名", key: "ec.correct_table_name", help: "控制台创建的替换词词表名", fType: fString, input: ti(ec["correct_table_name"])},
			{label: "替换词表ID", key: "ec.correct_table_id", help: "替换词词表ID", fType: fString, input: ti(ec["correct_table_id"])},
			{label: "正则替换表名", key: "ec.regex_correct_table_name", help: "控制台创建的正则替换词表名", fType: fString, input: ti(ec["regex_correct_table_name"])},
			{label: "正则替换表ID", key: "ec.regex_correct_table_id", help: "正则替换词表ID", fType: fString, input: ti(ec["regex_correct_table_id"])},
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

// SetConfigOnly marks the model as a config-only editor: it does not record,
// register hotkeys, or run services. Runtime panels show a note instead.
func (m *Model) SetConfigOnly(b bool) {
	m.configOnly = b
	if !b {
		return
	}
	// 配置模式下运行时开关/动作不生效——对相关字段的说明追加一句，并对
	// 运行时动作项加 🚫 前缀，明确提示不可用。
	runDisabled := map[string]bool{
		"androidmic_panel": true,
		"mic_test":         true,
		"noise_calibrate":  true,
		"diagnose":         true,
	}
	for tab, fields := range m.tabField {
		for i := range fields {
			f := &m.tabField[tab][i]
			if runDisabled[f.key] {
				f.label = "🚫 " + f.label
				f.help = strings.TrimRight(f.help, " ") + "；配置模式下始终不开启"
				continue
			}
			if f.key == "enabled" {
				f.help = strings.TrimRight(f.help, " ") + "；配置模式下始终不开启"
			}
		}
	}
}

func (m *Model) SetDiagnostics(logs *diag.MemoryBuffer) { m.diagnostics = logs }

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
		m.syncNoiseCalibration()
		return m, tickRefresh()
	case devMsg:
		if msg.Error != nil {
			m.logf("设备: %s", msg.Error)
		} else {
			m.devices = msg.Devices
		}
	case linkOpenFailedMsg:
		m.logf("❌ 打开链接失败: %s", msg.err)
	case historyRetryMsg:
		if msg.err != nil {
			m.logf("❌ 语音重试失败: %s；原历史记录未修改", msg.err)
		} else {
			m.logf("✅ 语音重试成功（引擎: %s）: %s", msg.result.Engine, truncateText(msg.result.Text, 100))
			m.logf("   已新增到历史；不会自动上屏或写入剪贴板，请从历史记录中复制")
		}
	case recordingActionMsg:
		if msg.err != nil {
			m.logf("❌ 录音操作失败: %s", msg.err)
		} else {
			m.logf("✅ %s", msg.detail)
		}
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
	if m.androidPanel {
		return m.handleAndroidPanelKey(k)
	}
	if m.historyMenu != nil {
		return m.handleHistoryMenuKey(k)
	}
	if k == "K" {
		m.openDiagnostics()
		return nil
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
		case "pgup", "pgdown":
			// 翻页：步长取当前可见窗口条目数，保证翻完光标仍在可视区。
			start, end := historyWindow(hist, m.cursor, m.historyRows())
			page := end - start
			if k == "pgup" {
				m.cursor -= page
				if m.cursor < 0 {
					m.cursor = 0
				}
			} else {
				m.cursor += page
				if m.cursor > len(hist)-1 {
					m.cursor = len(hist) - 1
				}
			}
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(hist) - 1
		case "enter":
			if m.cursor >= 0 && m.cursor < len(hist) {
				if hist[m.cursor].RecordingPath != "" {
					m.openHistoryMenu(m.cursor, hist[m.cursor])
				} else {
					m.copyHistoryText(hist[m.cursor].Text)
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
			case "j", "down", "k", "up", "h", "l", "left", "right":
				f.optIdx = (f.optIdx + 1) % len(f.opts)
			}
		case fToggle:
			// toggle 在导航态用回车直接切换（不进编辑态）；这里仅处理移动光标
			switch k {
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
	case len(k) == 1 && k[0] >= '1' && k[0] <= '6':
		m.switchTab(tabID(k[0] - '1'))
	}
	return nil
}

func (m *Model) openDiagnostics() {
	if m.diagnostics == nil || !m.diagnostics.Enabled() {
		m.logf("诊断收集未启用；在「[高级] 诊断收集」中选择“本次运行”，或以 --diagnostics 启动")
		return
	}
	path, err := diag.OpenInKate(m.diagnostics)
	if err != nil {
		m.logf("❌ 打开诊断日志失败: %s", err)
		return
	}
	m.logf("✅ 诊断日志已在 Kate 打开: %s", path)
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
	if m.configOnly {
		switch key {
		case "androidmic_panel", "mic_test", "noise_calibrate", "diagnose":
			m.logf("ⓘ 配置模式不提供该运行时功能（请运行 daemon 或使用托盘）：%s", key)
			return
		}
	}
	switch key {
	case "engine":
		m.openEnginePicker()
	case "engine_config":
		m.openChannelConfig()
	case "androidmic_panel":
		m.androidPanel = true
	case "mic_test":
		voice.StartMicTest()
		m.micPanel = &micPanelState{devIdx: m.deviceIndex(voice.MicTestDevice()), androidSlot: voice.MicTestDevice() == voice.AndroidMicDeviceName()}
	case "noise_calibrate":
		if voice.StartNoiseCalibration() {
			m.logf("🎤 正在检测底噪，请保持安静约 3 秒")
		} else {
			m.logf("⚠️ 底噪检测未启动：麦克风正在使用中")
		}
	case "diagnose":
		summary, path, err := voice.GenerateDiagnosticReport(m.cfg.Voice)
		if err != nil {
			m.logf("⚠️ 诊断报告生成失败: %v", err)
		} else {
			m.logf("%s", summary)
			m.logf("📄 完整报告: %s（复制「请大模型分析」段落发给大模型）", path)
			// 报告生成后直接在文件管理器中定位文件，方便复制/导出内容
			if err := revealRecording(path); err != nil {
				m.logf("⚠️ 打开报告位置失败: %v", err)
			} else {
				m.logf("📂 已在文件管理器中打开报告位置")
			}
		}
	case "doc_links":
		m.openLinkPicker()
	}
}

// syncNoiseCalibration 把异步底噪检测的结果写回当前表单，并通知运行中的语音插件。
func (m *Model) syncNoiseCalibration() {
	version, threshold := voice.NoiseCalibrationSnapshot()
	m.syncNoiseCalibrationSnapshot(version, threshold)
}

func (m *Model) syncNoiseCalibrationSnapshot(version uint64, threshold float64) {
	if version == 0 || version == m.noiseCalibrationVersion {
		return
	}
	m.noiseCalibrationVersion = version
	m.cfg.Voice.NoiseThreshold = threshold
	for i := range m.tabField[tabMic] {
		field := &m.tabField[tabMic][i]
		if field.key == "noise_threshold" {
			field.input.SetValue(fmt.Sprintf("%.4f", threshold))
			break
		}
	}
	if m.OnSave != nil {
		if err := m.OnSave(m.cfg); err != nil {
			m.logf("❌ 底噪阈值已保存，但运行时更新失败: %s", err)
			return
		}
	}
	m.logf("✅ 底噪阈值已同步为 %.4f", threshold)
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
			case "remove_cjk_space":
				vc.RemoveCJKSpace = f.boolVal
			case "auto_space":
				vc.AutoSpace = f.boolVal
			case "write_clipboard":
				vc.WriteClipboard = f.boolVal
			case "restore_clipboard":
				vc.RestoreClipboard = f.boolVal
			case "no_clipboard_on_fail":
				vc.NoClipboardOnFail = f.boolVal
			case "stop_delay_ms":
				fmt.Sscanf(f.input.Value(), "%d", &vc.StopDelayMs)
			case "hotkey_cooldown_ms":
				fmt.Sscanf(f.input.Value(), "%d", &vc.HotkeyCooldownMs)
			case "stop_after_silence":
				vc.StopAfterSilence = f.boolVal
			case "auto_gain":
				vc.AutoGain = f.boolVal
			case "noise_threshold":
				fmt.Sscanf(f.input.Value(), "%f", &vc.NoiseThreshold)
			case "noise_floor_auto":
				vc.NoiseFloorAuto = f.boolVal
			case "noise_deadband":
				fmt.Sscanf(f.input.Value(), "%f", &vc.NoiseDeadband)
			case "save_recordings":
				vc.SaveRecordings = f.boolVal
			case "androidmic_auto_start":
				vc.AndroidMicAutoStart = f.boolVal
			case "diagnostics_startup_runs":
				switch f.selectValue() {
				case "后续 3 次启动":
					next.Diagnostics.StartupRuns = 3
				case "持续开启":
					next.Diagnostics.StartupRuns = config.DiagnosticsAlways
				default:
					next.Diagnostics.StartupRuns = 0
				}
			case "auto_gain_target_peak":
				if v, ok := parseAGCFloat(f.input.Value()); ok {
					vc.AutoGainTargetPeak = v
				}
			case "auto_gain_max_peak":
				if v, ok := parseAGCFloat(f.input.Value()); ok {
					vc.AutoGainMaxPeak = v
				}
			case "auto_gain_max_rise":
				if v, ok := parseAGCFloat(f.input.Value()); ok {
					vc.AutoGainMaxRise = v
				}
			case "auto_gain_max_gain":
				if v, ok := parseAGCInt(f.input.Value()); ok {
					vc.AutoGainMaxGain = v
				}
			case "overlay_enabled":
				next.Overlay.Enabled = f.boolVal
			case "overlay_position":
				next.Overlay.Position = f.selectValue()
			case "overlay_idle_visible":
				next.Overlay.IdleVisible = f.boolVal
			case "overlay_scale":
				fmt.Sscanf(f.input.Value(), "%f", &next.Overlay.Scale)
			case "debug_enabled":
				next.Debug.Enabled = f.boolVal
			case "debug_hotkeys":
				next.Debug.Hotkeys = splitList(f.input.Value())
			}
		}
	}
}

// parseAGCFloat 解析 AGC 浮点参数：空字符串 = nil（走内置默认）；非法输入返回 ok=false（保持原值）。
func parseAGCFloat(s string) (*float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	fv, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, false
	}
	return &fv, true
}

// parseAGCInt 解析 AGC 整数参数：空字符串 = nil（走内置默认）；非法输入返回 ok=false（保持原值）。
func parseAGCInt(s string) (*int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	iv, err := strconv.Atoi(s)
	if err != nil {
		return nil, false
	}
	return &iv, true
}

func (m *Model) diagnosticsCurrentEnabled() bool {
	for _, fs := range m.tabField {
		for _, f := range fs {
			if f.key != "diagnostics_startup_runs" {
				continue
			}
			return f.selectValue() == "本次运行" || f.selectValue() == "持续开启"
		}
	}
	return false
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
		if m.OnDiagnosticsChange != nil {
			enabled := m.diagnosticsCurrentEnabled()
			m.cfg.Diagnostics.Active = enabled
			m.OnDiagnosticsChange(enabled)
		}
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
	if m.picker == nil && m.configOverlay == nil && m.linkPicker == nil && m.micPanel == nil && m.historyMenu == nil && !m.androidPanel {
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
	case m.androidPanel:
		overlay = m.renderAndroidPanel()
	case m.historyMenu != nil:
		overlay = m.renderHistoryMenu()
	}
	// 有弹层：弹层为模态，覆盖主内容（含帮助行），自含按键提示
	return overlayOn(base, overlay, m.h)
}

// helpLine 底部键位帮助行（固定显示在屏幕最后一行）。
// 导航遵循 vim 规范：h 左 / j 下 / k 上 / l 右；方向键等效。
func (m *Model) helpLine() string {
	return hStyle.Render("  j/k 或 ↑/↓ 选择 · h/l 或 ←/→ 分组 · 1-6 分组 · e 编辑 · s 保存 · L 日志 · K 导出诊断 · q 退出")
}

// renderBase 渲染主界面（不含模态弹层与底部帮助行）。
func (m *Model) renderBase() string {
	var b strings.Builder
	titleText := "🎙️ 🗣️ Just Talk Plus"
	titleStyle := tStyle
	if m.configOnly {
		titleText = "🎙️ 🗣️ Just Talk Plus - Config Mode (配置模式)"
		titleStyle = configTitleStyle
	}
	b.WriteString(titleStyle.Render(titleText) + "\n")
	b.WriteString(m.tabBar() + "\n")
	b.WriteString(lStyle.Render("── 状态 ──") + "\n")
	b.WriteString("  " + m.renderVoiceStatus() + "\n")
	b.WriteString("  " + m.renderVoiceStats() + "\n\n")

	if m.tab == tabHistory {
		b.WriteString(m.renderHistory(m.historyRows()))
	} else {
		b.WriteString(m.renderFields())
	}

	if m.showLogs {
		maxLogs := 3
		if m.logExpanded {
			maxLogs = 100
		}
		logs := voice.TUILogSnapshot()
		s := 0
		if len(logs) > maxLogs {
			s = len(logs) - maxLogs
		}
		b.WriteString(lStyle.Render("── 日志 (L 展开) ──") + "\n")
		// 折叠时固定 3 行内容；不足补空行，保证日志区恒占 4 行（标题 + 3 内容）。
		for i, l := range logs[s:] {
			b.WriteString("  " + dStyle.Render(m.formatLogLine(l)) + "\n")
			if i == 2 && !m.logExpanded {
				break
			}
		}
		if !m.logExpanded {
			for n := len(logs[s:]); n < 3; n++ {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// formatLogLine 将流式 ASR 更新保持为单行，并保留 partial 结果中最新的文字。
func (m *Model) formatLogLine(line string) string {
	line = strings.ReplaceAll(line, "\r", "")
	const prefix = "🎤 "
	if !strings.HasPrefix(line, prefix) {
		return line
	}
	maxWidth := m.w - 4
	if maxWidth <= 0 {
		maxWidth = 76
	}
	return prefix + truncateTextTail(strings.TrimPrefix(line, prefix), maxWidth-displayWidth(prefix))
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

// renderFields 渲染当前 tab 的设置字段（单行 label: value），
// 光标所在字段下方附一行灰色帮助说明（f.help）。
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
		if i == m.cursor && f.help != "" {
			b.WriteString("  " + strings.Repeat("  ", f.indent) + dStyle.Render("· "+f.help) + "\n")
		}
	}
	return b.String()
}

// historyHeaderRows 是历史 tab 在列表之上的固定开销行数
// （标题/tab 栏/状态区/分隔/提示行余量），滚动窗口据此计算可用高度。
const historyHeaderRows = 7

// historyRows 返回历史列表可用的渲染行数：整屏减去头部固定开销，
// 再减去日志区（若展开）占用的行数，保证滚动窗口不溢出屏幕。
func (m *Model) historyRows() int {
	rows := m.h - historyHeaderRows
	if rows < 3 {
		rows = 3
	}
	if m.showLogs {
		// 日志区固定占位：折叠时标题 1 行 + 内容 3 行，不足补空，保证布局稳定、
		// 不因日志条数变化挤压历史列表底部的“▼ 以下 N 条”提示。
		logRows := 4
		if m.logExpanded {
			logs := voice.TUILogSnapshot()
			logRows = len(logs) + 1
		}
		rows -= logRows
		if rows < 3 {
			rows = 3
		}
	}
	return rows
}

// historyWindow 返回以 cursor 为中心的滚动窗口 [start, end)（条目下标）。
// 窗口内渲染行数（每条 1 行，带 LogID 的条目 2 行）不超过 maxRows，
// 且光标所在条目始终可见——这是长历史列表正常浏览的前提。
func historyWindow(hist []voice.HistoryEntry, cursor, maxRows int) (start, end int) {
	total := len(hist)
	if total == 0 {
		return 0, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	if maxRows < 3 {
		maxRows = 3
	}
	rows := func(s, e int) int {
		n := 0
		for i := s; i < e; i++ {
			n++
			if hist[i].UpstreamLogID != "" {
				n++
			}
		}
		return n
	}
	start, end = cursor, cursor+1
	for {
		upOK := start > 0 && rows(start-1, end) <= maxRows
		downOK := end < total && rows(start, end+1) <= maxRows
		if !upOK && !downOK {
			break
		}
		// 先向上扩一格（基于旧窗口评估）；再基于更新后的窗口评估向下扩，
		// 避免 up/down 同时按旧窗口扩展导致一轮超出 maxRows。
		if upOK {
			start--
		}
		if end < total && rows(start, end+1) <= maxRows {
			end++
		}
	}
	return start, end
}

// renderHistory 渲染历史 tab：以光标为中心的滚动窗口，超出窗口的部分
// 用 ▲/▼ 指示器提示，保证光标条目始终可见且列表跟随光标滚动。
func (m *Model) renderHistory(rows int) string {
	if m.configOnly {
		return m.renderConfigOnlyNote("历史记录在配置模式下不提供，请运行 daemon 或使用托盘查看。")
	}
	hist := voice.History()
	if len(hist) == 0 {
		return m.renderHistoryEmpty()
	}
	if m.cursor >= len(hist) {
		m.cursor = len(hist) - 1
	}
	// 窗口条目行数预算：从总行数里预留 ▲/▼ 指示与底部按键提示（各 1 行），
	// 保证列表底部的“▼ 以下 N 条”始终可见，不被日志区或屏幕边界挤掉。
	windowRows := rows - 3
	if windowRows < 1 {
		windowRows = 1
	}
	start, end := historyWindow(hist, m.cursor, windowRows)
	var b strings.Builder
	if start > 0 {
		b.WriteString(dStyle.Render(fmt.Sprintf("  ▲ 以上 %d 条", start)) + "\n")
	}
	for i := start; i < end; i++ {
		e := hist[i]
		marker := "  "
		if i == m.cursor {
			marker = aStyle.Render("▸ ")
		}
		idx := dStyle.Render(fmt.Sprintf("#%-3d ", i+1))
		text := truncateText(e.Text, 48)
		ts := e.Time.Format("15:04:05")
		b.WriteString(marker + idx + vStyle.Render(text) + "  " + dStyle.Render(ts) + "\n")
		if e.UpstreamLogID != "" {
			b.WriteString("       " + dStyle.Render("豆包 LogID: "+e.UpstreamLogID) + "\n")
		}
	}
	if end < len(hist) {
		b.WriteString(dStyle.Render(fmt.Sprintf("  ▼ 以下 %d 条", len(hist)-end)) + "\n")
	}
	b.WriteString(dStyle.Render("  j/k 或 ↑/↓ 选择 · PgUp/PgDn 翻页 · Home/End 首尾 · enter 复制/录音操作") + "\n")
	return b.String()
}

func (m *Model) copyHistoryText(text string) {
	if err := voice.CopyHistoryText(text); err != nil {
		m.logf("❌ 复制失败: %s", err)
		return
	}
	m.logf("📋 已复制: %s", truncateText(text, 60))
}

func (m *Model) openHistoryMenu(i int, entry voice.HistoryEntry) {
	m.historyMenu = &historyMenuState{entry: entry, entryIndex: i, actions: []historyAction{historyCopy, historyRetry, historyPlay, historyReveal}}
}

func (m *Model) handleHistoryMenuKey(key string) tea.Cmd {
	menu := m.historyMenu
	if menu == nil {
		return nil
	}
	switch key {
	case "j", "down", "tab":
		menu.idx = (menu.idx + 1) % len(menu.actions)
	case "k", "up", "shift+tab":
		menu.idx = (menu.idx + len(menu.actions) - 1) % len(menu.actions)
	case "esc", "q", "ctrl+c":
		m.historyMenu = nil
	case "enter":
		action, entry := menu.actions[menu.idx], menu.entry
		m.historyMenu = nil
		switch action {
		case historyCopy:
			m.copyHistoryText(entry.Text)
		case historyRetry:
			m.logf("⏳ 正在读取录音并使用当前引擎识别…")
			return retryHistoryCmd(entry, *m.cfg)
		case historyPlay:
			return recordingActionCmd(entry.RecordingPath, false)
		case historyReveal:
			return recordingActionCmd(entry.RecordingPath, true)
		}
	}
	return nil
}

func (m *Model) renderHistoryMenu() string {
	menu := m.historyMenu
	labels := map[historyAction]string{historyCopy: "复制识别文本", historyRetry: "使用当前引擎重试", historyPlay: "播放录音（无 ffplay 时打开文件夹）", historyReveal: "在文件管理器中显示录音"}
	lines := []string{" " + lStyle.Render("历史记录操作")}
	// 序号 + 时间戳：让操作对象与列表中的条目可对照，长列表下不迷路。
	lines = append(lines, "  "+dStyle.Render(fmt.Sprintf("#%d  %s", menu.entryIndex+1, menu.entry.Time.Format("15:04:05"))))
	lines = append(lines, "  "+dStyle.Render(truncateText(menu.entry.Text, 48)))
	for index, action := range menu.actions {
		if index == menu.idx {
			lines = append(lines, aStyle.Render("▸ "+labels[action]))
		} else {
			lines = append(lines, "  "+vStyle.Render(labels[action]))
		}
	}
	lines = append(lines, "", " "+dStyle.Render("j/k 或 ↑/↓ 选择  回车确认  esc 返回"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Width(64).Render(strings.Join(lines, "\n"))
	return box
}

// renderHistoryEmpty 历史为空时的提示：说明运行期滚动保存方式。
func (m *Model) renderConfigOnlyNote(note string) string {
	return "\n\n  " + dStyle.Render(note) + "\n\n  配置模式仅用于编辑配置，运行时功能由 daemon / 托盘提供。\n"
}

func (m *Model) renderHistoryEmpty() string {
	msg := strings.Join([]string{
		"  暂无识别结果",
		"",
		"  完成一次语音识别（上屏或复制到剪贴板）后，",
		"  结果会显示在这里，最多保留最近 50 条。",
		"",
		"  历史保存在系统运行时目录：",
		"  程序重启后可恢复，系统重启后会自动清理。",
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
			return aStyle.Render("● 开")
		}
		return dStyle.Render("○ 关")
	case fSelect:
		v := f.opts[f.optIdx]
		if editing {
			return aStyle.Render("[" + v + " ▲▼]")
		}
		return vStyle.Render(v)
	case fAction:
		extra := ""
		if f.key == "engine" {
			extra = "  " + vStyle.Render(m.currentEngine())
		}
		label := f.actionLabel
		if label == "" {
			label = "进入"
		}
		return aStyle.Render("▶ ") + vStyle.Render(label) + extra
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
	if m.configOnly {
		return dStyle.Render("想录音或连手机：关掉本窗口，按平时方式打开软件")
	}
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
	if m.configOnly {
		return dStyle.Render("配置模式：只改设置，不监听按键、不连接手机麦克风")
	}
	status := voice.TUIStatus()
	label := "待机"
	style := dStyle
	// 底噪检测进行中：顶部状态优先显示检测进度，让用户在任意 tab 都能看到。
	calibrating := voice.MicTestLevel().NoiseCalibrating
	switch status.State {
	case "connecting":
		label, style = "连接中", wStyle
	case "recording":
		label, style = "录音中", aStyle
	case "waiting_asr":
		label, style = "等待 ASR", aStyle
	case "stopping_delayed":
		label, style = "延迟停止", wStyle
	case "stopping":
		label, style = "停止中", wStyle
	case "error":
		label, style = "错误", eStyle
	}
	if calibrating {
		label, style = "底噪检测中", wStyle
	}
	parts := []string{style.Render(label)}
	if status.Detail != "" && !calibrating {
		parts = append(parts, vStyle.Render(status.Detail))
	}
	if status.RecorderReady || status.ASRReady {
		recorderState, asrState := "off", "off"
		if status.RecorderReady {
			recorderState = "on"
		}
		if status.ASRReady {
			asrState = "on"
		}
		parts = append(parts, fmt.Sprintf("MIC %s  ASR %s", recorderState, asrState))
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
	message := fmt.Sprintf(format, args...)
	m.logs = append(m.logs, message)
	voice.AppendTUILog(message)
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

type recordingActionMsg struct {
	detail string
	err    error
}

type historyRetryMsg struct {
	result voice.RetryResult
	err    error
}

func retryHistoryCmd(entry voice.HistoryEntry, cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		result, err := voice.RetryHistory(ctx, entry, cfg.Voice)
		return historyRetryMsg{result: result, err: err}
	}
}
func recordingActionCmd(path string, reveal bool) tea.Cmd {
	return func() tea.Msg {
		if reveal {
			return recordingActionMsg{detail: "已在文件管理器中打开录音", err: revealRecording(path)}
		}
		openedFolder, err := playRecording(path)
		if openedFolder && err == nil {
			return recordingActionMsg{detail: "未找到 ffplay，已在文件管理器中打开录音"}
		}
		return recordingActionMsg{detail: "已开始播放录音", err: err}
	}
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
		case fToggle:
			switch k {
			case "j", "down", "k", "up", "h", "l", "left", "right":
				f.boolVal = !f.boolVal
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
		f := &ov.fields[ov.cursor]
		if f.fType == fAction {
			m.triggerAction(f.key)
			return nil
		}
		if f.fType == fToggle {
			f.boolVal = !f.boolVal
			return nil
		}
		ov.editing = true
		if f.fType == fString {
			f.input.Focus()
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
			var val string
			if f.fType == fToggle {
				if f.boolVal {
					val = "true"
				} else {
					val = "false"
				}
			} else {
				val = strings.TrimSpace(f.input.Value())
				if f.fType == fSelect {
					val = f.selectValue()
				}
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
		if i == ov.cursor && f.help != "" {
			lines = append(lines, "  "+dStyle.Render("· "+f.help))
		}
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
	case fToggle:
		if f.boolVal {
			return aStyle.Render("● 开")
		}
		return dStyle.Render("○ 关")
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
	case "]", "l", "j", "down":
		m.switchPanelDevice(+1)
	case "[", "h", "k", "up":
		m.switchPanelDevice(-1)
	case "esc", "q":
		voice.StopMicTest()
		m.micPanel = nil
	}
	return nil
}

// deviceIndex 返回设备在 m.devices 中的下标；空（自动）或未找到返回 -1。
func (m *Model) deviceIndex(dev string) int {
	for i, d := range m.devices {
		if d == dev {
			return i
		}
	}
	return -1
}

// switchPanelDevice 在调试面板内切换录音设备（dir=±1 循环；-1 档 = 自动）。
// 档位：0 = 自动，1..len(devices) = 系统设备，len(devices)+1 = Android 设备。
// 切换会停止当前测试录音并持久化到配置。
func (m *Model) switchPanelDevice(dir int) {
	if m.micPanel == nil {
		return
	}
	if len(m.devices) == 0 {
		m.logf("⚠️  未检测到可用录音设备")
		return
	}
	steps := len(m.devices) + 2 // 自动 + 各设备 + Android 设备
	slot := m.micPanel.devIdx + 1 + dir
	if m.micPanel.androidSlot {
		slot = len(m.devices) + 1 + dir
	}
	slot = ((slot % steps) + steps) % steps
	dev := ""
	if slot > 0 && slot <= len(m.devices) {
		dev = m.devices[slot-1]
	} else if slot > len(m.devices) {
		dev = voice.AndroidMicDeviceName()
	}
	voice.SetMicTestDevice(dev)
	m.micPanel.devIdx = slot - 1
	m.micPanel.androidSlot = dev == voice.AndroidMicDeviceName()
	m.logf("🎤 录音设备: %s", displayDeviceName(dev))
}

func (m *Model) renderMicPanel() string {
	if m.configOnly {
		return m.renderConfigOnlyNote("麦克风调试在配置模式下不提供，请运行 daemon 或使用托盘。")
	}
	lvl := voice.MicTestLevel()
	width := 84
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
	}
	// 最长的「增益后真实」行决定条宽，避免窄终端里数值越出面板边框。
	barW := width - lipgloss.Width("  增益后真实: [] 0.00") - 2
	if barW < 1 {
		barW = 1
	}
	peakBar := renderLevelBar(lvl.Peak, lvl.PeakHold, barW)
	rmsBar := renderLevelBar(lvl.RMS, lvl.RMSHold, barW)
	gainedRMSBar := renderLevelBar(lvl.GainedRMS, lvl.GainedRMSHold, barW)
	state := "待机（按录音热键开始测试）"
	if voice.MicTestTesting() {
		state = "测试中（按录音热键停止）"
	}
	ag := "○ 关"
	if lvl.AutoGain {
		ag = "● 开"
	}
	noiseState := fmt.Sprintf("%.4f", lvl.NoiseThreshold)
	if lvl.NoiseCalibrating {
		noiseState = "检测中..."
	}
	clipMark := ""
	if lvl.Clipping {
		clipMark = eStyle.Render(" ⚠ 已限幅")
	}
	gainLine := "  增益: " + wStyle.Render(fmt.Sprintf("%d", lvl.Gain)) + dStyle.Render("  ( +/- 调整，范围 1-100 )")

	lines := []string{
		" " + lStyle.Render("🎤 麦克风调试"),
		"  设备: " + vStyle.Render(displayDeviceName(voice.MicTestDevice())) + dStyle.Render("  ( [ ] 切换，自动 = 系统默认 )"),
		"  峰值: " + vStyle.Render("["+peakBar+"]") + fmt.Sprintf(" %.2f", lvl.Peak),
		"  真实: " + vStyle.Render("["+rmsBar+"]") + fmt.Sprintf(" %.2f", lvl.RMS),
		"  增益后真实: " + vStyle.Render("["+gainedRMSBar+"]") + fmt.Sprintf(" %.2f", lvl.GainedRMS) + clipMark,
		gainLine,
		"  自适应增益: " + aStyle.Render(ag) + dStyle.Render("  (空格切换)"),
		"  底噪阈值: " + wStyle.Render(noiseState) + dStyle.Render("  (设置页可调整/检测)"),
		"  状态: " + vStyle.Render(state),
		"",
		" " + dStyle.Render("[ ] 设备  |  +/- 增益  |  空格 自适应增益  |  录音热键 开始/停止  |  esc 退出"),
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

// clamp01 把 v 限制在 [0,1]。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// levelZone 返回电平条某位置所在分区：0 绿（<1/3）｜1 黄（1/3..2/3）｜2 红（>=2/3）。
func levelZone(frac float64) int {
	if frac < 1.0/3.0 {
		return 0
	}
	if frac < 2.0/3.0 {
		return 1
	}
	return 2
}

// renderLevelBar 渲染三色电平条：条体按位置分绿/黄/红三区（暗色底），
// 填充部分为当前电平（高亮、按分区着色）；hold>0 时在保持峰值位置绘制标记。
func renderLevelBar(level, hold float64, width int) string {
	if width <= 0 {
		return ""
	}
	level = clamp01(level)
	filled := int(level*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	holdIdx := -1
	if hold > 0 {
		holdIdx = int(hold*float64(width) + 0.5)
		if holdIdx >= width {
			holdIdx = width - 1
		}
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i == holdIdx {
			b.WriteString(barHold.Render("▏"))
			continue
		}
		z := levelZone(float64(i) / float64(width))
		if i < filled {
			b.WriteString(barFill[z].Render("█"))
		} else {
			b.WriteString(barEmpty[z].Render("░"))
		}
	}
	return b.String()
}

// truncateText 截断文本到指定显示宽度（含中文宽字符）。
func truncateText(s string, maxW int) string {
	w := 0
	for i, r := range s {
		cw := displayWidth(string(r))
		if w+cw > maxW {
			return s[:i] + "…"
		}
		w += cw
	}
	return s
}

// truncateTextTail 截断文本到指定显示宽度，保留末尾字符。
func truncateTextTail(s string, maxW int) string {
	if displayWidth(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return "…"
	}
	width := 1
	start := len(s)
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		charWidth := displayWidth(string(r))
		if width+charWidth > maxW {
			break
		}
		width += charWidth
		start = i - size
		i -= size
	}
	return "…" + s[start:]
}

func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if r > 0x7F {
			width += 2
		} else {
			width++
		}
	}
	return width
}

// displayDeviceName 设备显示名；空（自动）显示「自动」。
func displayDeviceName(dev string) string {
	if dev == "" {
		return "自动"
	}
	if dev == voice.AndroidMicDeviceName() {
		return "Android 设备 (AndroidMic)"
	}
	return dev
}

// ---- Android 虚拟麦克风服务端管理面板 ----

// handleAndroidPanelKey 管理面板按键：s 启动 / t 停止 / esc 退出。
func (m *Model) handleAndroidPanelKey(k string) tea.Cmd {
	switch k {
	case "s":
		port := m.cfg.Voice.AndroidMicPort
		if err := voice.AndroidMicStart(port); err != nil {
			m.logf("❌ AndroidMic 服务端启动失败: %v", err)
		} else {
			m.logf("✅ AndroidMic 服务端已启动，监听端口 %d", voice.AndroidMicStatus().Port)
		}
	case "t":
		if err := voice.AndroidMicStop(); err != nil {
			m.logf("❌ AndroidMic 服务端停止失败: %v", err)
		} else {
			m.logf("🛑 AndroidMic 服务端已停止")
		}
	case "esc", "q":
		m.androidPanel = false
	}
	return nil
}

// localIPv4Addrs 返回本机非回环的 IPv4 地址（局域网内手机可连接的地址）。
// 用于 Android 虚拟麦克风面板展示“手机连接”入口，避免用户手动查 IP。
func localIPv4Addrs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}

// renderAndroidPanel 渲染 AndroidMic 服务端管理面板（模态）。
func (m *Model) renderAndroidPanel() string {
	if m.configOnly {
		return m.renderConfigOnlyNote("Android 虚拟麦克风服务端在配置模式下不提供，请运行 daemon 或使用托盘。")
	}
	st := voice.AndroidMicStatus()
	width := 84
	if m.w > 0 && width > m.w-4 {
		width = m.w - 4
	}

	statusLine := "○ 未启动"
	connectLine := "服务端未启动"
	if st.Running {
		statusLine = fmt.Sprintf("● 监听中 (端口 %d)", st.Port)
		ips := localIPv4Addrs()
		if len(ips) > 0 {
			addrs := make([]string, len(ips))
			for i, ip := range ips {
				addrs[i] = fmt.Sprintf("%s:%d", ip, st.Port)
			}
			connectLine = strings.Join(addrs, "  ")
		} else {
			connectLine = fmt.Sprintf("0.0.0.0:%d", st.Port)
		}
	}
	clientLine := "无客户端"
	if len(st.Clients) == 1 {
		clientLine = "1 个客户端: " + st.Clients[0]
	} else if len(st.Clients) > 1 {
		clientLine = fmt.Sprintf("%d 个客户端: %s", len(st.Clients), strings.Join(st.Clients, ", "))
	}

	inputLine := "未指向本服务端"
	if voice.MicTestDevice() == voice.AndroidMicDeviceName() {
		inputLine = "已指向本服务端（麦克风调试面板可切换）"
	}

	lines := []string{
		" " + lStyle.Render("📱 Android 虚拟麦克风（内嵌 TCP 服务端）"),
		"  状态: " + vStyle.Render(statusLine),
		"  手机连接: " + vStyle.Render(connectLine),
		"  连接: " + vStyle.Render(clientLine),
		"  录音输入: " + vStyle.Render(inputLine),
		"",
		"  AndroidMic 协议服务端：手机端 AndroidMic 应用连接",
		"  「电脑IP:端口」后，即可把手机当 Just Talk Plus 的麦克风。",
		"  手机需与电脑在同一局域网。",
		"",
		" " + dStyle.Render("[s] 启动  |  [t] 停止  |  esc 退出"),
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
