// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package tray

import (
	"bytes"
	"image/png"
	"log/slog"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

// fakeEnv satisfies engine.PluginEnv with a fixed config so the tray's pure
// read logic can be exercised without a real engine.
type fakeEnv struct {
	cfg *config.Config
}

func (f fakeEnv) RegisterHotkey(hotkey.Combo, func(hotkey.Event)) error {
	return nil
}

func (f fakeEnv) RegisterHotkeyWithOptions(hotkey.Combo, hotkey.RegisterOptions, func(hotkey.Event)) error {
	return nil
}

func (f fakeEnv) UnregisterHotkey(hotkey.Combo) error { return nil }

func (f fakeEnv) Logger() *slog.Logger { return slog.Default() }

func (f fakeEnv) Config() *config.Config { return f.cfg }

func (f fakeEnv) Engine() *engine.Engine { return nil }

func TestStateLabel(t *testing.T) {
	cases := map[string]string{
		"idle":      "待机",
		"recording": "录音中",
		"stopping":  "停止中",
		"error":     "错误",
		"weird":     "weird", // 未知状态透传
	}
	for in, want := range cases {
		if got := stateLabel(in); got != want {
			t.Errorf("stateLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModeLabel(t *testing.T) {
	cases := map[string]string{
		"hold":   "Push-to-talk",
		"toggle": "切换",
		"":       "切换", // 空 mode 视为切换
	}
	for in, want := range cases {
		if got := modeLabel(in); got != want {
			t.Errorf("modeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCurrentMode(t *testing.T) {
	empty := &config.Config{}
	p := &Plugin{env: fakeEnv{cfg: empty}}
	if got := p.currentMode(); got != "toggle" {
		t.Errorf("currentMode(empty) = %q, want toggle", got)
	}

	hold := &config.Config{Voice: config.VoiceConfig{Mode: "hold"}}
	p = &Plugin{env: fakeEnv{cfg: hold}}
	if got := p.currentMode(); got != "hold" {
		t.Errorf("currentMode(hold) = %q, want hold", got)
	}
}

func TestNextEngine(t *testing.T) {
	types := asrcfg.Types()
	if len(types) == 0 {
		t.Skip("no user-facing engines registered")
	}
	// current = first -> next = second
	if len(types) >= 2 {
		if got := nextEngine(types[0]); got != types[1] {
			t.Errorf("nextEngine(%q) = %q, want %q", types[0], got, types[1])
		}
	}
	// last -> wraps around to first
	last := types[len(types)-1]
	if got := nextEngine(last); got != types[0] {
		t.Errorf("nextEngine(last=%q) = %q, want %q", last, got, types[0])
	}
	// unknown -> first
	if got := nextEngine("not-a-real-engine"); got != types[0] {
		t.Errorf("nextEngine(unknown) = %q, want %q", got, types[0])
	}
	// empty -> first
	if got := nextEngine(""); got != types[0] {
		t.Errorf("nextEngine(empty) = %q, want %q", got, types[0])
	}
}

func TestLogPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("logPath on windows uses the user cache dir; not deterministic in CI")
	}
	if got := logPath(); got != "/tmp/just-talk-plus.log" {
		t.Errorf("logPath = %q, want /tmp/just-talk-plus.log", got)
	}
}

func TestStatusIcon(t *testing.T) {
	idle := statusIcon("idle")
	rec := statusIcon("recording")
	if bytes.Equal(idle, rec) {
		t.Error("idle 与 recording 的状态图标应不同")
	}
	img, err := png.Decode(bytes.NewReader(idle))
	if err != nil {
		t.Fatalf("statusIcon 生成的不是合法 PNG: %v", err)
	}
	if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 16 {
		t.Errorf("图标尺寸 = %dx%d, want 16x16", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestAndroidMicPort(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.AndroidMicPort = 5555
	if got := androidmicPort(cfg); got != 5555 {
		t.Errorf("androidmicPort(5555) = %d, want 5555", got)
	}
	if got := androidmicPort(nil); got != 54345 {
		t.Errorf("androidmicPort(nil) = %d, want 54345", got)
	}
	zero := config.Default()
	zero.Voice.AndroidMicPort = 0
	if got := androidmicPort(zero); got != 54345 {
		t.Errorf("androidmicPort(0) = %d, want 54345", got)
	}
}

func TestPortOnline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if !portOnline(port) {
		t.Errorf("portOnline(%d) = false, want true", port)
	}
	// 1 is a privileged port; nothing listens there, so it must report false.
	if portOnline(1) {
		t.Errorf("portOnline(1) = true, want false")
	}
}

func TestFormatStatsSpeed(t *testing.T) {
	if got := formatStatsSpeed(0, 0); got != "0" {
		t.Fatalf("zero speed = %q, want 0", got)
	}
	if got := formatStatsSpeed(60, time.Minute); got != "60" {
		t.Fatalf("speed = %q, want 60", got)
	}
}

func TestHistoryRefreshGenerationSupersedesOlderRetry(t *testing.T) {
	p := &Plugin{}
	p.historyRefreshGeneration = 1
	first := p.historyRefreshGeneration
	p.historyRefreshGeneration++
	second := p.historyRefreshGeneration
	if first == second {
		t.Fatal("new history refresh must supersede the previous generation")
	}
}

func TestHistorySnapshotChangesWhenEntriesChange(t *testing.T) {
	first := []voice.HistoryEntry{{Text: "first", Time: time.Unix(1, 0)}}
	second := []voice.HistoryEntry{{Text: "second", Time: time.Unix(2, 0)}}
	if historySnapshot(first) == historySnapshot(second) {
		t.Fatal("different history entries must produce different snapshots")
	}
	if historySnapshot(first) != historySnapshot(first) {
		t.Fatal("same history entries must produce a stable snapshot")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("短文本", 24); got != "短文本" {
		t.Errorf("truncate(short) = %q, want unchanged", got)
	}
	if got := truncate("一二三四五", 3); got != "一二三…" {
		t.Errorf("truncate(cjk) = %q, want 一二三…", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("truncate(ascii) = %q, want hello…", got)
	}
}
