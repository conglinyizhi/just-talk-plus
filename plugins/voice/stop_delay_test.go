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
	p.noteStopActivity(0.02, 0.02)
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
