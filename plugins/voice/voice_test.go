// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

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

func TestSubscribeStatusReceivesCoalescedChanges(t *testing.T) {
	tuiStatusMu.Lock()
	originalStatus := tuiStatus
	tuiStatus = TUIVoiceStatus{State: "idle", UpdatedAt: time.Now()}
	tuiStatusMu.Unlock()
	t.Cleanup(func() {
		tuiStatusMu.Lock()
		tuiStatus = originalStatus
		tuiStatusMu.Unlock()
	})

	changes, unsubscribe := SubscribeStatus()
	defer unsubscribe()
	setTUIStatus(func(s *TUIVoiceStatus) { s.State = "recording" })
	setTUIStatus(func(s *TUIVoiceStatus) { s.State = "stopping" })

	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("status change was not delivered")
	}
	select {
	case <-changes:
		t.Fatal("status changes should be coalesced while unread")
	default:
	}

	unsubscribe()
	setTUIStatus(func(s *TUIVoiceStatus) { s.State = "idle" })
	select {
	case _, ok := <-changes:
		if ok {
			t.Fatal("unsubscribed channel should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("unsubscribed channel was not closed")
	}
}

func TestDispatchTextOutputRecordsHistoryBeforeAsyncOutput(t *testing.T) {
	useTestHistoryStore(t)
	p := NewVoicePlugin()
	p.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Default()
	cfg.Voice.AutoSubmit = false
	cfg.Voice.WriteClipboard = false
	p.cfg.Store(cfg)

	p.dispatchTextOutput("先记历史", 1, "trace-1", "")
	items := History()
	if len(items) != 1 || items[0].Text != "先记历史" {
		t.Fatalf("history immediately after dispatch = %+v, want one entry", items)
	}
}

func useTestStatsStore(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stats.json")
	originalPathFunc := statsPathFunc
	statsPathFunc = func() string { return path }
	tuiStatsMu.Lock()
	tuiStats = TUIVoiceStats{}
	tuiStatsMu.Unlock()
	t.Cleanup(func() {
		tuiStatsMu.Lock()
		tuiStats = TUIVoiceStats{}
		tuiStatsMu.Unlock()
		statsPathFunc = originalPathFunc
	})
}

func TestSubscribeStatsReceivesCoalescedChanges(t *testing.T) {
	useTestStatsStore(t)
	tuiStatsMu.Lock()
	original := tuiStats
	tuiStats = TUIVoiceStats{}
	tuiStatsMu.Unlock()
	t.Cleanup(func() {
		tuiStatsMu.Lock()
		tuiStats = original
		tuiStatsMu.Unlock()
	})

	changes, unsubscribe := SubscribeStats()
	defer unsubscribe()
	recordTUIStats("统计测试", time.Minute)
	recordTUIStats("第二条", time.Minute)
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("stats change was not delivered")
	}
	select {
	case <-changes:
		t.Fatal("stats changes should be coalesced while unread")
	default:
	}
	stats := TUIStats()
	if stats.Sessions != 2 || stats.Chars != uint64(len([]rune("统计测试第二条"))) {
		t.Fatalf("stats = %+v", stats)
	}
}

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

// TestHotkeyCooldown 覆盖录音会话结束后的热键冷却：
// 冷却期内 inCooldownLocked 为真、超时恢复、0ms 关闭、restart 豁免。
func TestHotkeyCooldown(t *testing.T) {
	t.Run("cooldown active then expires", func(t *testing.T) {
		p := &VoicePlugin{}
		p.armCooldownLocked(100)
		if !p.inCooldownLocked() {
			t.Fatal("should be in cooldown right after arm")
		}
		// 手动把冷却起点拨到过去，模拟超时
		p.cooldownUntil = time.Now().Add(-time.Millisecond)
		if p.inCooldownLocked() {
			t.Fatal("cooldown should have expired")
		}
	})
	t.Run("zero ms disables cooldown", func(t *testing.T) {
		p := &VoicePlugin{}
		p.armCooldownLocked(0)
		if p.inCooldownLocked() {
			t.Fatal("cooldown should be disabled when ms=0")
		}
	})
	t.Run("restart clears cooldown", func(t *testing.T) {
		p := &VoicePlugin{}
		p.armCooldownLocked(100)
		if !p.inCooldownLocked() {
			t.Fatal("cooldown should be armed")
		}
		// restart 语义：清掉冷却，立即继续
		p.cooldownUntil = time.Time{}
		if p.inCooldownLocked() {
			t.Fatal("cleared cooldown should not be active")
		}
	})
}

// TestFormatLimitDuration 上限文案：整数秒不带小数点，非整数保留一位。
func TestFormatLimitDuration(t *testing.T) {
	cases := []struct {
		ms   int
		want string
	}{
		{120000, "120 秒"},
		{12500, "12.5 秒"},
		{1000, "1 秒"},
		{1500, "1.5 秒"},
	}
	for _, tc := range cases {
		if got := formatLimitDuration(tc.ms); got != tc.want {
			t.Fatalf("formatLimitDuration(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

// TestStopForAudioLimitGuards 未录音或会话已换时不产生副作用：
// 音频上限触发可能晚于用户手动停止，此时不能再动状态。
func TestStopForAudioLimitGuards(t *testing.T) {
	t.Run("未录音时不动作", func(t *testing.T) {
		p := &VoicePlugin{}
		p.stopForAudioLimit(1, 120000)
		if p.audioLimitHit {
			t.Fatal("未录音时不应记录上限命中")
		}
	})
	t.Run("会话不匹配时不动作", func(t *testing.T) {
		p := &VoicePlugin{recording: true, sessionID: 7}
		p.stopForAudioLimit(1, 120000)
		if p.audioLimitHit {
			t.Fatal("旧会话的上限触发不应影响当前会话")
		}
	})
	t.Run("已在停止中时不动作", func(t *testing.T) {
		p := &VoicePlugin{recording: true, stopping: true, sessionID: 1}
		p.stopForAudioLimit(1, 120000)
		if p.audioLimitHit {
			t.Fatal("停止过程中不应重复触发")
		}
	})
}
