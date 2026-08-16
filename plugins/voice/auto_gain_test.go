// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"testing"
	"time"
)

func TestAutoGainClipReduces(t *testing.T) {
	a := NewAutoGain(true, 10)
	now := time.Now()
	// 削波：第一块只计数
	a.Process(0.99, 0.5, now)
	if a.Gain() != 10 {
		t.Fatalf("gain=%d, want 10", a.Gain())
	}
	// 连续第二块 → 降 2
	a.Process(0.99, 0.5, now.Add(time.Millisecond))
	if a.Gain() != 8 {
		t.Fatalf("gain=%d, want 8", a.Gain())
	}
	// 持续削波继续降
	a.Process(1.0, 0.6, now.Add(2*time.Millisecond))
	a.Process(1.0, 0.6, now.Add(3*time.Millisecond))
	if a.Gain() != 6 {
		t.Fatalf("gain=%d, want 6", a.Gain())
	}
	// 削波降到下限为止
	for i := 0; i < 20; i++ {
		a.Process(1.0, 0.6, now.Add(time.Duration(i+4)*time.Millisecond))
		a.Process(1.0, 0.6, now.Add(time.Duration(i+5)*time.Millisecond))
	}
	if a.Gain() < MinGain {
		t.Fatalf("gain=%d below min", a.Gain())
	}
}

func TestAutoGainQuietRecovers(t *testing.T) {
	a := NewAutoGain(true, 5)
	now := time.Now()
	changed := false
	// 低语音持续 1.5s（lowDelay）后缓慢升回；4s 内升 2 次
	for i := 0; i < 20; i++ {
		if a.Process(0.1, 0.02, now.Add(time.Duration(i)*200*time.Millisecond)) {
			changed = true
		}
	}
	if !changed || a.Gain() != 7 {
		t.Fatalf("gain=%d changed=%v, want 7", a.Gain(), changed)
	}
}

func TestAutoGainSilenceDoesNotRecover(t *testing.T) {
	a := NewAutoGain(true, 5)
	now := time.Now()
	// 静音（rms < lowMin）不得升增益
	for i := 0; i < 20; i++ {
		a.Process(0.0, 0.001, now.Add(time.Duration(i)*200*time.Millisecond))
	}
	if a.Gain() != 5 {
		t.Fatalf("gain=%d, want 5 (silence must not raise)", a.Gain())
	}
}

func TestAutoGainDisabled(t *testing.T) {
	a := NewAutoGain(false, 5)
	a.Process(0.99, 0.5, time.Now())
	a.Process(0.99, 0.5, time.Now())
	if a.Gain() != 5 {
		t.Fatalf("disabled still changed gain: %d", a.Gain())
	}
}

func TestAutoGainManualBaseline(t *testing.T) {
	a := NewAutoGain(true, 5)
	a.SetGain(20)
	if a.Gain() != 20 {
		t.Fatalf("gain=%d, want 20", a.Gain())
	}
	a.SetGain(0)
	if a.Gain() != MinGain {
		t.Fatalf("gain=%d, want min", a.Gain())
	}
}

func TestAnalyzePCM(t *testing.T) {
	// 静音
	peak, rms := analyzePCM([]byte{0, 0, 0, 0})
	if peak != 0 || rms != 0 {
		t.Fatalf("silence: peak=%v rms=%v", peak, rms)
	}
	// 满幅（32767）
	peak, rms = analyzePCM([]byte{0xFF, 0x7F, 0xFF, 0x7F})
	if peak < 0.99 || rms < 0.99 {
		t.Fatalf("full: peak=%v rms=%v", peak, rms)
	}
	// 半幅（16384）
	peak, _ = analyzePCM([]byte{0x00, 0x40, 0x00, 0x40})
	if peak < 0.49 || peak > 0.51 {
		t.Fatalf("half: peak=%v", peak)
	}
	// 负数（-16384）
	peak, _ = analyzePCM([]byte{0x00, 0xC0, 0x00, 0xC0})
	if peak < 0.49 || peak > 0.51 {
		t.Fatalf("negative half: peak=%v", peak)
	}
}

func TestGetMicGain(t *testing.T) {
	// 重置全局状态，模拟初始增益
	micTest.mu.Lock()
	micTest.active = false
	micTest.cfg = nil
	micTest.autoGain = NewAutoGain(false, 7)
	micTest.mu.Unlock()
	if got := GetMicGain(); got != 7 {
		t.Fatalf("gain=%d, want 7", got)
	}
	// SetMicGain 在面板关闭时不生效
	SetMicGain(20)
	if got := GetMicGain(); got != 7 {
		t.Fatalf("inactive SetMicGain changed gain: %d", got)
	}
	// 面板打开后生效
	StartMicTest()
	SetMicGain(20)
	if got := GetMicGain(); got != 20 {
		t.Fatalf("gain=%d, want 20", got)
	}
	StopMicTest()
	// 退出面板后增益保留（供入口显示）
	if got := GetMicGain(); got != 20 {
		t.Fatalf("gain=%d, want 20 after stop", got)
	}
}
