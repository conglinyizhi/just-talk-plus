// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file was modified from the original Just Talk by whoamihappyhacking
// (GPL-3.0). See NOTICE for maintenance history.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
)

type Config struct {
	Voice       VoiceConfig       `toml:"voice"`
	Debug       DebugConfig       `toml:"debug"`
	Diagnostics DiagnosticsConfig `toml:"diagnostics"`
	Overlay     OverlayConfig     `toml:"overlay"`
	Tray        TrayConfig        `toml:"tray"`
}

type DebugConfig struct {
	Enabled bool     `toml:"enabled"`
	Hotkeys []string `toml:"hotkeys"`
}

// DiagnosticsConfig controls bounded in-memory diagnostic collection.
// StartupRuns is 0 when disabled, -1 when enabled for every start, and a
// positive number of future starts to collect before automatically disabling.
type DiagnosticsConfig struct {
	StartupRuns int  `toml:"startup_runs"`
	Active      bool `toml:"-"`
}

const DiagnosticsAlways = -1

// ConsumeStartup reports whether diagnostics should be collected for this
// process and decrements a finite future-start allowance.
func (d *DiagnosticsConfig) ConsumeStartup() bool {
	switch {
	case d.StartupRuns == DiagnosticsAlways:
		return true
	case d.StartupRuns > 0:
		d.StartupRuns--
		return true
	default:
		return false
	}
}

type OverlayConfig struct {
	Enabled     bool    `toml:"enabled"`
	Position    string  `toml:"position"`
	IdleVisible bool    `toml:"idle_visible"`
	Scale       float64 `toml:"scale"`
}

// TrayConfig controls the system tray/menu bar control surface.
// When enabled (and running without the terminal UI), just-talk-plus shows
// a tray icon so the daemon can be controlled without occupying a console
// window: switch ASR engine, pick the microphone device, toggle push-to-talk,
// open logs/diagnostics, and quit.
//
// It is only used in background (--gui) mode; the full terminal UI
// (just-talk-plus --tui) keeps its own in-window controls.
type TrayConfig struct {
	Enabled bool `toml:"enabled"`

	// CycleEngineHotkey is an optional global hotkey that cycles the ASR
	// engine (doubao-stream -> auralwise -> ... -> back to the first). Empty
	// disables it. Only takes effect in background (--gui) mode; it must
	// not collide with the recording hotkey.
	CycleEngineHotkey string `toml:"cycle_engine_hotkey"`

	// TogglePushToTalkHotkey is an optional global hotkey that switches the
	// recording behaviour between push-to-talk (hold) and toggle. Empty
	// disables it.
	TogglePushToTalkHotkey string `toml:"toggle_push_to_talk_hotkey"`
}

type VoiceConfig struct {
	Enabled    bool   `toml:"enabled"`
	Mode       string `toml:"mode"`
	PushToTalk string `toml:"push_to_talk"`
	Device     string `toml:"device"`
	// AndroidMicPort 内嵌 AndroidMic TCP 服务端监听端口（device 为
	// "androidmic" 时作为录音输入源；默认与 AndroidMic 桌面端一致 54345）。
	AndroidMicPort int `toml:"androidmic_port"`
	// AndroidMicAutoStart 启动时自动启用内嵌服务端（默认关闭：新用户
	// 不认识该功能时，避免默默占用端口）。
	AndroidMicAutoStart bool `toml:"androidmic_auto_start"`
	Gain                int  `toml:"gain"`
	AutoGain            bool `toml:"auto_gain"` // 自适应增益：爆音自动降低、安静恢复
	// 以下自适应增益参数均为可选（nil = 使用内置保守默认值），写入 config.toml 后重启生效：
	//   auto_gain_target_peak 目标输出峰值（默认 0.42）
	//   auto_gain_max_peak    输出峰值超过此值开始回撤增益（默认 0.65）
	//   auto_gain_max_rise    每块（约 200ms）最大升高倍数（默认 1.15）
	//   auto_gain_max_gain    增益上限（倍，默认 25；上限 100）
	AutoGainTargetPeak *float64 `toml:"auto_gain_target_peak"`
	AutoGainMaxPeak    *float64 `toml:"auto_gain_max_peak"`
	AutoGainMaxRise    *float64 `toml:"auto_gain_max_rise"`
	AutoGainMaxGain    *int     `toml:"auto_gain_max_gain"`
	NoiseThreshold     float64  `toml:"noise_threshold"`  // 底噪阈值（RMS）：用于开头开始录音与结束停止判定
	NoiseDeadband      float64  `toml:"noise_deadband"`   // 停止判定容差：高于底噪阈值多少才算人声（默认 0.1）
	NoiseFloorAuto     bool     `toml:"noise_floor_auto"` // 自动底噪：停止判定阈值随环境底噪自动漂移
	SaveRecordings     bool     `toml:"save_recordings"`  // 保存最近历史对应的运行期录音 WAV
	StopDelayMs        int      `toml:"stop_delay_ms"`
	StopAfterSilence   bool     `toml:"stop_after_silence"` // 停止请求后检测有效人声并重置静音倒计时
	// HotkeyCooldownMs 一次录音会话结束后的热键冷却（毫秒）：冷却期内再按
	// 录音热键静默忽略，防止结束瞬间误触导致秒开秒关。默认 100ms。
	HotkeyCooldownMs int    `toml:"hotkey_cooldown_ms"`
	Language         string `toml:"language"`
	// 句尾标点省略（输出前统一处理）：主开关省略句号「。」。
	OmitTrailingPunct bool `toml:"omit_trailing_punct"`
	// 副设置（依赖主开关）：切断句尾所有标点（含英文符号）。
	OmitTrailingAll bool `toml:"omit_trailing_all"`
	// 副设置（依赖主开关）：额外省略英文半角 . ! ?。
	OmitTrailingASCII bool `toml:"omit_trailing_ascii"`
	// RemoveCJKSpace 删除中文与中文之间的空格（如"你好 世界"→"你好世界"）。
	// 中文与英文/数字之间的空格保留（排版惯例）。
	RemoveCJKSpace bool `toml:"remove_cjk_space"`
	// AutoSpace 按中文排版规范自动处理空格：
	//   - 中文与英文之间补空格（如"在LeanCloud上"→"在 LeanCloud 上"）
	//   - 中文与数字之间补空格（如"花了5000元"→"花了 5000 元"）
	//   - 全角标点与两侧字符之间不留空格（如"iPhone ，好开心"→"iPhone，好开心"）
	AutoSpace bool `toml:"auto_space"`
	// 上屏与复制行为（平级独立开关，识别完成后按组合执行）：
	AutoSubmit bool `toml:"auto_submit"`
	// WriteClipboard 复制到剪贴板：识别结果写入剪贴板（独立行为，可与上屏同开）。
	WriteClipboard bool `toml:"write_clipboard"`
	// RestoreClipboard 上屏后还原剪贴板：上屏成功后还原原内容，尽量不污染剪贴板历史。
	RestoreClipboard bool `toml:"restore_clipboard"`
	// NoClipboardOnFail 上屏失败不写入：上屏失败时也还原原内容，识别文本不留在剪贴板。
	NoClipboardOnFail bool `toml:"no_clipboard_on_fail"`
	// Engine 语音引擎类型：doubao-stream（默认）| auralwise | openai | dashscope | null。
	Engine string `toml:"engine"`
	// EngineConfigs 按引擎类型分组的私有配置（api_key、base_url、model 等）。
	// 每个引擎一套，互不覆盖，可同时保存多个渠道的凭据。
	EngineConfigs map[string]map[string]string `toml:"engine_configs"`
	// EngineConfig 旧版平铺引擎配置（未按引擎分组）；只作为 doubao-stream
	// 的读取兜底与迁移来源，其它引擎不读它（见 legacyFlatEngine）。
	EngineConfig map[string]string `toml:"engine_config"`
	// 以下为 doubao 旧版平铺字段，向后兼容；新配置建议写入 engine_config。
	AppKey     string   `toml:"app_key"`
	AccessKey  string   `toml:"access_key"`
	ResourceID string   `toml:"resource_id"`
	Hotwords   []string `toml:"hotwords"`
}

// legacyFlatEngine 是旧版平铺 engine_config 唯一归属的引擎。
//
// [voice.engine_config] 以及顶层的 app_key / access_key / resource_id 都是
// 单引擎时代（豆包）的产物：2026-08-16 的 e7f8c27 才引入按引擎分组
// [voice.engine_configs.<engine>]。所以那份平铺配置属于 doubao-stream，
// 只有它能把它当回退来源。
const legacyFlatEngine = "doubao-stream"

// EngineConfigFor 返回指定引擎的私有配置：优先 engine_configs[engine]，
// 只有 doubao-stream 缺失分组时才回退到旧版平铺 engine_config。
// nil 表示该引擎未配置。
//
// 平铺配置不跨引擎共享是刻意的（凭据隔离）：让 auralwise / openai /
// siliconflow 之类的引擎读豆包的平铺配置，等于把一家的 key 发到另一家的
// 端点——请求必然 401，还平白泄漏了凭据；TUI 的引擎配置弹层也会被预填成
// 豆包凭据，一保存就把豆包 key 写进别的引擎分组。
func (vc VoiceConfig) EngineConfigFor(engine string) map[string]string {
	if per, ok := vc.EngineConfigs[engine]; ok {
		return per
	}
	if engine == legacyFlatEngine && len(vc.EngineConfig) > 0 {
		return vc.EngineConfig
	}
	return nil
}

// SetEngineConfig 写入指定引擎的私有配置；返回是否发生了变更。
// 写入后旧版平铺 engine_config 不再更新（迁移到分组存储）。
func (vc *VoiceConfig) SetEngineConfig(engine string, cfg map[string]string) bool {
	if vc.EngineConfigs == nil {
		vc.EngineConfigs = map[string]map[string]string{}
	}
	old := vc.EngineConfigs[engine]
	vc.EngineConfigs[engine] = cfg
	return !equalStringMap(old, cfg)
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func Default() *Config {
	return &Config{
		Voice: VoiceConfig{
			Enabled: true, Mode: "toggle", PushToTalk: "Alt+Super",
			Language: "zh-CN", AutoSubmit: true, AutoGain: true, AutoSpace: true,
			WriteClipboard: true, RestoreClipboard: true, NoClipboardOnFail: true,
			Engine: "doubao-stream", StopDelayMs: 800, NoiseThreshold: 0.015, NoiseDeadband: 0.1,
			AndroidMicPort: 54345, HotkeyCooldownMs: 100, // 热键冷却默认 100ms；AndroidMicAutoStart 默认关闭
		},
		Overlay: OverlayConfig{
			Enabled: true, Position: "bottom-center", IdleVisible: false, Scale: 1.0,
		},
		Tray: TrayConfig{
			Enabled: true,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = FindConfig()
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func FindConfig() string {
	candidates := []string{"./config.toml"}
	if path := DefaultPath(); path != "" {
		candidates = append(candidates, path)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "just-talk-plus", "config.toml"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func Save(cfg *Config) error {
	path := FindConfig()
	if path == "" {
		path = DefaultPath()
	}
	return SaveTo(path, cfg)
}

// SaveTo writes cfg to an explicit TOML path. It is used when a startup-only
// setting must be consumed from a config selected with --config.
func SaveTo(path string, cfg *Config) error {
	if path == "" {
		return fmt.Errorf("cannot determine user config directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// DefaultPath returns the platform-standard per-user configuration path.
func DefaultPath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
			return filepath.Join(dir, "just-talk-plus", "config.toml")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "just-talk-plus", "config.toml")
	}
	return ""
}

// ---- Hotkey parser ----

var modifierNames = map[string]hotkey.Modifier{
	"ctrl": hotkey.ModCtrl, "alt": hotkey.ModAlt, "shift": hotkey.ModShift,
	"control": hotkey.ModCtrl, "option": hotkey.ModAlt, "super": hotkey.ModSuper,
	"cmd": hotkey.ModSuper, "command": hotkey.ModSuper, "win": hotkey.ModSuper,
}

var keyNameToCode = buildKeyNameMap()

func buildKeyNameMap() map[string]hotkey.KeyCode {
	m := make(map[string]hotkey.KeyCode)
	for i := hotkey.KeyA; i <= hotkey.KeyZ; i++ {
		m[strings.ToLower(i.String())] = i
		m[i.String()] = i
	}
	for i := hotkey.Key0; i <= hotkey.Key9; i++ {
		m[i.String()] = i
	}
	for i := hotkey.KeyF1; i <= hotkey.KeyF24; i++ {
		m[strings.ToLower(i.String())] = i
		m[i.String()] = i
	}
	m["ctrl"] = hotkey.KeyCtrl
	m["control"] = hotkey.KeyCtrl
	m["alt"] = hotkey.KeyAlt
	m["option"] = hotkey.KeyAlt
	m["shift"] = hotkey.KeyShift
	m["super"] = hotkey.KeySuper
	m["cmd"] = hotkey.KeySuper
	m["command"] = hotkey.KeySuper
	m["win"] = hotkey.KeySuper
	for _, k := range []hotkey.KeyCode{
		hotkey.KeySpace, hotkey.KeyTab, hotkey.KeyEnter, hotkey.KeyEscape,
		hotkey.KeyBackspace, hotkey.KeyCapsLock,
		hotkey.KeyArrowUp, hotkey.KeyArrowDown, hotkey.KeyArrowLeft, hotkey.KeyArrowRight,
		hotkey.KeyHome, hotkey.KeyEnd, hotkey.KeyPageUp, hotkey.KeyPageDown,
		hotkey.KeyInsert, hotkey.KeyDelete,
		hotkey.KeyNum0, hotkey.KeyNum1, hotkey.KeyNum2, hotkey.KeyNum3, hotkey.KeyNum4,
		hotkey.KeyNum5, hotkey.KeyNum6, hotkey.KeyNum7, hotkey.KeyNum8, hotkey.KeyNum9,
		hotkey.KeyBacktick, hotkey.KeyMinus, hotkey.KeyEqual,
		hotkey.KeyLeftBracket, hotkey.KeyRightBracket, hotkey.KeyBackslash,
		hotkey.KeySemicolon, hotkey.KeyQuote,
		hotkey.KeyComma, hotkey.KeyPeriod, hotkey.KeySlash,
	} {
		m[strings.ToLower(k.String())] = k
		m[k.String()] = k
	}
	m["space"] = hotkey.KeySpace
	m["enter"] = hotkey.KeyEnter
	m["return"] = hotkey.KeyEnter
	m["esc"] = hotkey.KeyEscape
	m["escape"] = hotkey.KeyEscape
	m["backspace"] = hotkey.KeyBackspace
	m["tab"] = hotkey.KeyTab
	m["up"] = hotkey.KeyArrowUp
	m["down"] = hotkey.KeyArrowDown
	m["left"] = hotkey.KeyArrowLeft
	m["right"] = hotkey.KeyArrowRight
	m["home"] = hotkey.KeyHome
	m["end"] = hotkey.KeyEnd
	m["pageup"] = hotkey.KeyPageUp
	m["pagedown"] = hotkey.KeyPageDown
	m["insert"] = hotkey.KeyInsert
	m["delete"] = hotkey.KeyDelete
	m["capslock"] = hotkey.KeyCapsLock
	m["`"] = hotkey.KeyBacktick
	m["-"] = hotkey.KeyMinus
	m["="] = hotkey.KeyEqual
	m["["] = hotkey.KeyLeftBracket
	m["]"] = hotkey.KeyRightBracket
	m["\\"] = hotkey.KeyBackslash
	m[";"] = hotkey.KeySemicolon
	m["'"] = hotkey.KeyQuote
	m[","] = hotkey.KeyComma
	m["."] = hotkey.KeyPeriod
	m["/"] = hotkey.KeySlash
	return m
}

func ParseHotkey(s string) (hotkey.Combo, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return hotkey.Combo{}, fmt.Errorf("empty hotkey string")
	}
	parts := strings.Split(s, "+")
	var mods hotkey.Modifier
	var key hotkey.KeyCode
	for _, part := range parts {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		if mod, ok := modifierNames[lower]; ok {
			mods |= mod
			continue
		}
		if k, ok := keyNameToCode[lower]; ok {
			if key != hotkey.KeyNone {
				return hotkey.Combo{}, fmt.Errorf("multiple keys in %q", s)
			}
			if k.IsModifier() && key == hotkey.KeyNone {
				key = k
			} else if !k.IsModifier() {
				key = k
			}
			continue
		}
		return hotkey.Combo{}, fmt.Errorf("unknown key %q in %q", part, s)
	}
	if key == hotkey.KeyNone && mods != hotkey.ModNone {
		return hotkey.Combo{Mods: mods, Key: hotkey.KeyNone}, nil
	}
	if key != hotkey.KeyNone && mods == hotkey.ModNone && !key.IsModifier() {
		return hotkey.Combo{Mods: hotkey.ModNone, Key: key}, nil
	}
	if key.IsModifier() && mods == hotkey.ModNone {
		return hotkey.Combo{Mods: hotkey.KeyCodeToModifier(key), Key: hotkey.KeyNone}, nil
	}
	if key != hotkey.KeyNone && mods != hotkey.ModNone {
		return hotkey.Combo{Mods: mods, Key: key}, nil
	}
	return hotkey.Combo{}, fmt.Errorf("cannot parse hotkey %q", s)
}

func ParseHotkeys(strings []string) ([]hotkey.Combo, error) {
	var combos []hotkey.Combo
	for _, s := range strings {
		c, err := ParseHotkey(s)
		if err != nil {
			return nil, err
		}
		combos = append(combos, c)
	}
	return combos, nil
}
