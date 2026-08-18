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

	"github.com/conglinyizhi/just-talk-plus/config"
)

func TestAutoGainLimiterDropsGain(t *testing.T) {
	a := NewAutoGain(true, 20)
	if !a.Process(0.05, 0.01, 0.97, true, time.Now()) {
		t.Fatal("limited speech should reduce gain")
	}
	// 限幅块按 targetPeak/outputPeak 回撤，但单块降幅不超过 maxReleaseFactor
	// (0.75)：20*0.75=15，而非旧版的一刀切 0.65（13）。
	if got := a.Gain(); got != 15 {
		t.Fatalf("gain=%d, want 15", got)
	}
}

func TestAutoGainRaisesWeakSpeechGradually(t *testing.T) {
	a := NewAutoGain(true, 7)
	now := time.Now()
	for i := 0; i < 4; i++ {
		a.Process(0.01, 0.002, 0.07, false, now.Add(time.Duration(i)*200*time.Millisecond))
	}
	if got := a.Gain(); got < 10 || got > 15 {
		t.Fatalf("gain=%d, want a conservative rise between 10 and 15", got)
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
	// 两个不同幅度样本：0.5 (16384) 与 0.25 (8192)。配置增益 2 会让 0.5 越过
	// safePeakCap，整块应被预缩放为 safePeakCap/rawPeak ≈ 1.6，而非逐样本软限幅。
	if _, err := writeEnd.Write([]byte{0x00, 0x40, 0x00, 0x20}); err != nil {
		t.Fatal(err)
	}
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 4)
	n, rawPeak, rawRMS, gainedPeak, gainedRMS, limited, err := recorder.readWithLevels(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("bytes read=%d, want 4", n)
	}
	if math.Abs(rawPeak-0.5) > 0.01 || math.Abs(rawRMS-0.395) > 0.01 {
		t.Fatalf("raw levels peak=%v rms=%v, want about 0.5 / 0.395", rawPeak, rawRMS)
	}
	// 安全 gain(1.6) 低于配置 gain(2)，必须报告 limited 供 AGC 下一块回撤。
	if !limited {
		t.Fatal("loud block must report limited so AGC backs off")
	}
	if gainedPeak < 0.75 || gainedPeak > 0.85 {
		t.Fatalf("gained peak=%v, want near safePeakCap %v", gainedPeak, safePeakCap)
	}
	// 预缩放后 RMS：sqrt((0.8^2+0.4^2)/2) ≈ 0.6325。
	if gainedRMS < 0.62 || gainedRMS > 0.65 {
		t.Fatalf("gained rms=%v, want about 0.6325", gainedRMS)
	}
	// 两个样本应以相同比例缩放（允许量化误差）：0.8 与 0.4。
	a0 := float64(int16(buf[0])|int16(buf[1])<<8) / 32767.0
	a1 := float64(int16(buf[2])|int16(buf[3])<<8) / 32767.0
	if math.Abs(a0-0.80) > 0.02 || math.Abs(a1-0.40) > 0.02 {
		t.Fatalf("gained samples=%v %v, want ~0.80 and ~0.40", a0, a1)
	}
	ratio0, ratio1 := a0/0.5, a1/0.25
	if math.Abs(ratio0-ratio1) > 0.02 {
		t.Fatalf("scaling ratios %v and %v differ: samples must scale uniformly", ratio0, ratio1)
	}
	if got := buf; got[0] == 0xFF && got[1] == 0x7F {
		t.Fatalf("gained PCM=%#v, should not hard-clip to full scale", got)
	}
}

// TestReadWithLevelsSilenceNoBoost 覆盖 rawPeak=0 的安全逻辑：不除零、不放大、
// 不报 limited。
func TestReadWithLevelsSilenceNoBoost(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readEnd.Close()

	recorder := NewRecorder(nil, 2)
	recorder.stdout = readEnd
	if _, err := writeEnd.Write([]byte{0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 4)
	n, rawPeak, rawRMS, gainedPeak, gainedRMS, limited, err := recorder.readWithLevels(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("bytes read=%d, want 4", n)
	}
	if rawPeak != 0 || rawRMS != 0 || gainedPeak != 0 || gainedRMS != 0 {
		t.Fatalf("silence levels raw=(%v,%v) gained=(%v,%v), want all zero", rawPeak, rawRMS, gainedPeak, gainedRMS)
	}
	if limited {
		t.Fatal("silence must not report limited")
	}
}

func TestEffectiveGainForPeak(t *testing.T) {
	// rawPeak 0.5、配置 gain 2：安全 gain = 0.8/0.5 = 1.6，低于配置 gain → limited。
	scale, limited := effectiveGainForPeak(0.5, 2)
	if math.Abs(scale-1.6) > 1e-9 || !limited {
		t.Fatalf("raw 0.5/gain 2: scale=%v limited=%v, want 1.6 true", scale, limited)
	}
	// 安静块 rawPeak 0.1、gain 2：2*0.1=0.2 < 0.8，原样通过。
	scale, limited = effectiveGainForPeak(0.1, 2)
	if scale != 2 || limited {
		t.Fatalf("raw 0.1/gain 2: scale=%v limited=%v, want 2 false", scale, limited)
	}
	// rawPeak=0：不除零，返回配置增益且不 limited。
	scale, limited = effectiveGainForPeak(0, 2)
	if scale != 2 || limited {
		t.Fatalf("raw 0/gain 2: scale=%v limited=%v, want 2 false", scale, limited)
	}
	// rawPeak<0 同样兜底。
	scale, limited = effectiveGainForPeak(-0.1, 3)
	if scale != 3 || limited {
		t.Fatalf("raw -0.1/gain 3: scale=%v limited=%v, want 3 false", scale, limited)
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

// TestAutoGainMaxGainCap 验证 AGC 的增益上限：即使输入持续偏弱，增益也不会
// 越过 maxGain（默认 25），从源头阻止"不经意把增益拉得很大"。
func TestAutoGainMaxGainCap(t *testing.T) {
	a := NewAutoGain(true, 20)
	now := time.Now()
	for i := 0; i < 30; i++ {
		a.Process(0.01, 0.002, 0.05, false, now.Add(time.Duration(i)*200*time.Millisecond))
	}
	if got := a.Gain(); got != 25 {
		t.Fatalf("gain=%d, want capped at 25", got)
	}
}

// TestAutoGainParamsFromVoice 验证 config.toml 可选字段对默认参数的覆盖与钳制。
func TestAutoGainParamsFromVoice(t *testing.T) {
	// 未配置 → 保守默认值。
	p := autoGainParamsFromVoice(config.VoiceConfig{})
	if p.TargetPeak != 0.42 || p.MaxPeak != 0.65 || p.MaxRiseFactor != 1.15 || p.MaxGain != 25 {
		t.Fatalf("defaults mismatch: %+v", p)
	}
	// 显式配置 → 覆盖默认值。
	tp, mp, mr, mg := 0.5, 0.7, 1.3, 40
	p = autoGainParamsFromVoice(config.VoiceConfig{
		AutoGainTargetPeak: &tp, AutoGainMaxPeak: &mp, AutoGainMaxRise: &mr, AutoGainMaxGain: &mg,
	})
	if p.TargetPeak != 0.5 || p.MaxPeak != 0.7 || p.MaxRiseFactor != 1.3 || p.MaxGain != 40 {
		t.Fatalf("override mismatch: %+v", p)
	}
	// 越界钳制：maxGain 超过 100 钳回 100，targetPeak 低于 0.05 钳到 0.05。
	tp2, mg2 := 0.01, 500
	p = autoGainParamsFromVoice(config.VoiceConfig{AutoGainTargetPeak: &tp2, AutoGainMaxGain: &mg2})
	if p.TargetPeak != 0.05 || p.MaxGain != 100 {
		t.Fatalf("clamp mismatch: %+v", p)
	}
}
