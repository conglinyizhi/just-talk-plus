// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestAutoGainLimiterDropsGain(t *testing.T) {
	a := NewAutoGain(true, 20)
	if !a.Process(0.05, 0.01, 0.97, true, time.Now()) {
		t.Fatal("limited speech should reduce gain")
	}
	if got := a.Gain(); got != 13 {
		t.Fatalf("gain=%d, want 13", got)
	}
}

func TestAutoGainRaisesWeakSpeechQuickly(t *testing.T) {
	a := NewAutoGain(true, 7)
	now := time.Now()
	for i := 0; i < 4; i++ {
		a.Process(0.01, 0.002, 0.07, false, now.Add(time.Duration(i)*200*time.Millisecond))
	}
	if got := a.Gain(); got < 30 {
		t.Fatalf("gain=%d, want weak speech to reach useful range quickly", got)
	}
}

func TestAutoGainSilenceDoesNotRise(t *testing.T) {
	a := NewAutoGain(true, 7)
	for i := 0; i < 20; i++ {
		a.Process(0.001, 0.0001, 0.007, false, time.Now())
	}
	if got := a.Gain(); got != 7 {
		t.Fatalf("gain=%d, want 7 (silence must not raise)", got)
	}
}

func TestAutoGainDisabled(t *testing.T) {
	a := NewAutoGain(false, 5)
	a.Process(0.01, 0.002, 0.05, false, time.Now())
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

func TestReadWithLevels(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readEnd.Close()

	recorder := NewRecorder(nil, 2)
	recorder.stdout = readEnd
	if _, err := writeEnd.Write([]byte{0x00, 0x40}); err != nil {
		t.Fatal(err)
	}
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2)
	n, rawPeak, rawRMS, gainedPeak, gainedRMS, limited, err := recorder.readWithLevels(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("bytes read=%d, want 2", n)
	}
	if math.Abs(rawPeak-0.5) > 0.01 || math.Abs(rawRMS-0.5) > 0.01 {
		t.Fatalf("raw levels peak=%v rms=%v, want about 0.5", rawPeak, rawRMS)
	}
	if !limited {
		t.Fatal("gain should engage soft limiter")
	}
	if gainedPeak < 0.95 || gainedPeak >= 0.99 || gainedRMS < 0.95 || gainedRMS >= 0.99 {
		t.Fatalf("gained levels peak=%v rms=%v, want soft-limited range", gainedPeak, gainedRMS)
	}
	if got := buf; got[0] == 0xFF && got[1] == 0x7F {
		t.Fatalf("gained PCM=%#v, should not hard-clip to full scale", got)
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
