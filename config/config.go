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
	"github.com/c/just-talk-go/hotkey"
)

type Config struct {
	Voice   VoiceConfig   `toml:"voice"`
	Debug   DebugConfig   `toml:"debug"`
	Overlay OverlayConfig `toml:"overlay"`
}

type DebugConfig struct {
	Enabled bool     `toml:"enabled"`
	Hotkeys []string `toml:"hotkeys"`
}

type OverlayConfig struct {
	Enabled     bool    `toml:"enabled"`
	Position    string  `toml:"position"`
	IdleVisible bool    `toml:"idle_visible"`
	Scale       float64 `toml:"scale"`
}

type VoiceConfig struct {
	Enabled     bool   `toml:"enabled"`
	Mode        string `toml:"mode"`
	PushToTalk  string `toml:"push_to_talk"`
	Device      string `toml:"device"`
	Gain        int    `toml:"gain"`
	AutoGain    bool   `toml:"auto_gain"` // 自适应增益：爆音自动降低、安静恢复
	StopDelayMs int    `toml:"stop_delay_ms"`
	Language    string `toml:"language"`
	AutoSubmit  bool   `toml:"auto_submit"`
	// 句尾标点省略（输出前统一处理）：主开关省略句号「。」。
	OmitTrailingPunct bool `toml:"omit_trailing_punct"`
	// 副设置（依赖主开关）：切断句尾所有标点（含英文符号）。
	OmitTrailingAll bool `toml:"omit_trailing_all"`
	// 副设置（依赖主开关）：额外省略英文半角 . ! ?。
	OmitTrailingASCII bool `toml:"omit_trailing_ascii"`
	// RemoveCJKSpace 删除中文与中文之间的空格（如"你好 世界"→"你好世界"）。
	// 中文与英文/数字之间的空格保留（排版惯例）。
	RemoveCJKSpace bool `toml:"remove_cjk_space"`
	// Engine 语音引擎类型：doubao-stream（默认）| auralwise | openai | dashscope | null。
	Engine string `toml:"engine"`
	// EngineConfigs 按引擎类型分组的私有配置（api_key、base_url、model 等）。
	// 每个引擎一套，互不覆盖，可同时保存多个渠道的凭据。
	EngineConfigs map[string]map[string]string `toml:"engine_configs"`
	// EngineConfig 旧版平铺引擎配置（未按引擎分组）；仅作为读取兜底与迁移来源。
	EngineConfig map[string]string `toml:"engine_config"`
	// 以下为 doubao 旧版平铺字段，向后兼容；新配置建议写入 engine_config。
	AppKey     string   `toml:"app_key"`
	AccessKey  string   `toml:"access_key"`
	ResourceID string   `toml:"resource_id"`
	Hotwords   []string `toml:"hotwords"`
}

// EngineConfigFor 返回指定引擎的私有配置：优先 engine_configs[engine]，
// 旧版平铺 engine_config 作为回退（迁移来源）。nil 表示该引擎未配置。
func (vc VoiceConfig) EngineConfigFor(engine string) map[string]string {
	if per, ok := vc.EngineConfigs[engine]; ok {
		return per
	}
	if vc.EngineConfig != nil && len(vc.EngineConfig) > 0 {
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
			Language: "zh-CN", AutoSubmit: true, AutoGain: true,
			Engine: "doubao-stream",
		},
		Overlay: OverlayConfig{
			Enabled: true, Position: "bottom-center", IdleVisible: false, Scale: 1.0,
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
		candidates = append(candidates, filepath.Join(xdg, "just-talk", "config.toml"))
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
		if path == "" {
			return fmt.Errorf("cannot determine user config directory")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
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
			return filepath.Join(dir, "just-talk", "config.toml")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "just-talk", "config.toml")
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
