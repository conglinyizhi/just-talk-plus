// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"io"
	"log/slog"
	"testing"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
)

// fakeEnv 最小 PluginEnv 实现，用于插件级测试。
type fakeEnv struct {
	cfg *config.Config
}

func (e *fakeEnv) RegisterHotkey(hotkey.Combo, func(hotkey.Event)) error { return nil }
func (e *fakeEnv) RegisterHotkeyWithOptions(hotkey.Combo, hotkey.RegisterOptions, func(hotkey.Event)) error {
	return nil
}
func (e *fakeEnv) UnregisterHotkey(hotkey.Combo) error { return nil }
func (e *fakeEnv) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (e *fakeEnv) Config() *config.Config { return e.cfg }
func (e *fakeEnv) Engine() *engine.Engine { return nil }

func TestConfigReloadSync(t *testing.T) {
	p := NewVoicePlugin()
	start := config.Default()
	if err := p.Init(&fakeEnv{cfg: start}); err != nil {
		t.Fatal(err)
	}
	if p.cfg.Load().Voice.OmitTrailingPunct {
		t.Fatal("default omit_trailing_punct should be false")
	}
	// TUI 保存 → OnConfigReload → p.cfg 应同步为新配置（本进程内立即生效）
	updated := config.Default()
	updated.Voice.OmitTrailingPunct = true
	updated.Voice.OmitTrailingAll = true
	if err := p.OnConfigReload(updated); err != nil {
		t.Fatal(err)
	}
	vc := p.cfg.Load().Voice
	if !vc.OmitTrailingPunct || !vc.OmitTrailingAll {
		t.Fatalf("reload did not sync p.cfg: %+v", vc)
	}
	// 输出层应应用新配置
	if got := cleanTrailingPunct("你好。", vc); got != "你好" {
		t.Fatalf("cleanTrailingPunct after reload = %q, want 你好", got)
	}
}

func TestTUILogSnapshotPreservesAppendOrder(t *testing.T) {
	tuilogMu.Lock()
	originalLog := TUILog
	originalBuffer := TUILogBuf
	TUILog = nil
	TUILogBuf = nil
	tuilogMu.Unlock()
	t.Cleanup(func() {
		tuilogMu.Lock()
		TUILog = originalLog
		TUILogBuf = originalBuffer
		tuilogMu.Unlock()
	})

	AppendTUILog("first")
	AppendTUILog("second")
	AppendTUILog("third")
	logs := TUILogSnapshot()
	if len(logs) != 3 || logs[0] != "first" || logs[2] != "third" {
		t.Fatalf("logs = %v", logs)
	}

	logs[0] = "changed"
	if got := TUILogSnapshot()[0]; got != "first" {
		t.Fatalf("snapshot mutated shared buffer: %q", got)
	}
}
