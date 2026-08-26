// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"testing"

	"github.com/conglinyizhi/just-talk-plus/config"
)

func TestNormalizeStopDelayMs(t *testing.T) {
	for _, test := range []struct {
		name string
		in   int
		want int
	}{
		{name: "missing", in: 0, want: defaultStopDelayMs},
		{name: "negative", in: -1, want: defaultStopDelayMs},
		{name: "configured", in: 6000, want: 6000},
		{name: "bounded", in: maxStopDelayMs + 1, want: maxStopDelayMs},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeStopDelayMs(test.in); got != test.want {
				t.Fatalf("normalizeStopDelayMs(%d) = %d, want %d", test.in, got, test.want)
			}
		})
	}
}

func TestStopAfterSilenceExtendsOnVoice(t *testing.T) {
	p := NewVoicePlugin()
	if err := p.Init(&fakeEnv{cfg: config.Default()}); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.recording = true
	p.stopping = true
	p.stopAfterSilence = true
	p.stopDelayMs = 6000
	p.noiseThreshold = 0.015
	p.armStopTimerLocked()
	firstDeadline := p.stopAt
	p.mu.Unlock()
	// 超过「阈值 + 判定容差」（0.015+0.1=0.115）才算人声并重置倒计时
	p.noteStopActivity(0.2, 0.1)
	p.mu.Lock()
	stopAt := p.stopAt
	stopTimer := p.stopTimer
	p.mu.Unlock()
	if !stopAt.After(firstDeadline) {
		t.Fatalf("voice activity should extend stop deadline: %s <= %s", stopAt, firstDeadline)
	}
	if stopTimer != nil {
		stopTimer.Stop()
	}
}

// TestVoiceActivityLocked 覆盖停止判定的容差（死区）与自动底噪跟踪。
func TestVoiceActivityLocked(t *testing.T) {
	t.Run("static deadband", func(t *testing.T) {
		p := &VoicePlugin{noiseThreshold: 0.015, noiseDeadband: 0.1}
		// 阈值附近的环境噪声（0.05 < 0.015+0.1）不算人声
		if p.voiceActivityLocked(0.05, 0.05) {
			t.Fatal("near-threshold noise should not count as voice")
		}
		// 明显高于阈值才算
		if !p.voiceActivityLocked(0.2, 0.1) {
			t.Fatal("loud block should count as voice")
		}
		// 峰值判定同样带容差：t*3 = 0.345
		if p.voiceActivityLocked(0.02, 0.2) {
			t.Fatal("peak below t*3 should not count")
		}
		if !p.voiceActivityLocked(0.02, 0.4) {
			t.Fatal("peak above t*3 should count")
		}
	})
	t.Run("no deadband keeps original", func(t *testing.T) {
		p := &VoicePlugin{noiseThreshold: 0.015}
		if !p.voiceActivityLocked(0.02, 0.02) {
			t.Fatal("original threshold should still trigger")
		}
	})
	t.Run("auto floor tracks noise", func(t *testing.T) {
		p := &VoicePlugin{noiseThreshold: 0.015, noiseDeadband: 0.1, noiseFloorAuto: true, noiseFloor: 0.015}
		// 底噪块（0.05 < 0.115）不算人声，且缓慢抬升底噪基准
		if p.voiceActivityLocked(0.05, 0.05) {
			t.Fatal("quiet block should be noise")
		}
		if p.noiseFloor <= 0.015 {
			t.Fatalf("noise floor should rise toward block, got %v", p.noiseFloor)
		}
		floor := p.noiseFloor
		// 人声块（0.3）触发判定，且不更新底噪基准
		if !p.voiceActivityLocked(0.3, 0.1) {
			t.Fatal("voice block should trigger")
		}
		if p.noiseFloor != floor {
			t.Fatalf("voice block must not update noise floor: %v -> %v", floor, p.noiseFloor)
		}
	})
	t.Run("threshold clamped at 0.2", func(t *testing.T) {
		p := &VoicePlugin{noiseThreshold: 0.2, noiseDeadband: 0.1}
		// t = 0.3 -> clamp 0.2；0.25 >= 0.2 算人声
		if !p.voiceActivityLocked(0.25, 0.1) {
			t.Fatal("0.25 should be voice after clamp")
		}
	})
}

func TestNormalizeNoiseDeadband(t *testing.T) {
	for _, test := range []struct {
		name string
		in   float64
		want float64
	}{
		{name: "zero", in: 0, want: 0},
		{name: "negative", in: -0.5, want: 0},
		{name: "default", in: 0.1, want: 0.1},
		{name: "bounded", in: 0.5, want: 0.2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeNoiseDeadband(test.in); got != test.want {
				t.Fatalf("normalizeNoiseDeadband(%v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}
