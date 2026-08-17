// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"testing"
	"time"
)

func TestPeakHoldRisesAndHolds(t *testing.T) {
	var p peakHoldTracker
	t0 := time.Now()
	if got := p.update(0.3, t0); got != 0.3 {
		t.Fatalf("hold=%v, want 0.3", got)
	}
	if got := p.update(0.6, t0.Add(10*time.Millisecond)); got != 0.6 {
		t.Fatalf("hold=%v, want 0.6", got)
	}
	// 更小声不会拉低保持峰
	if got := p.update(0.2, t0.Add(20*time.Millisecond)); got != 0.6 {
		t.Fatalf("hold dropped to %v, want 0.6", got)
	}
}

func TestPeakHoldFallsBackAfterSilence(t *testing.T) {
	var p peakHoldTracker
	t0 := time.Now()
	p.update(0.8, t0)
	// 静音超时后回落清零
	if got := p.update(0.01, t0.Add(peakHoldDuration+time.Millisecond)); got != 0 {
		t.Fatalf("hold=%v, want 0 after silence", got)
	}
}

func TestPeakHoldPersistsThroughBriefQuiet(t *testing.T) {
	var p peakHoldTracker
	t0 := time.Now()
	p.update(0.7, t0)
	// 短暂静音（未超时）不回落
	if got := p.update(0.01, t0.Add(500*time.Millisecond)); got != 0.7 {
		t.Fatalf("hold=%v, want 0.7 (brief quiet)", got)
	}
}

func TestPeakHoldIgnoresQuiet(t *testing.T) {
	var p peakHoldTracker
	if got := p.update(0.01, time.Now()); got != 0 {
		t.Fatalf("hold=%v, want 0 (quiet)", got)
	}
}
