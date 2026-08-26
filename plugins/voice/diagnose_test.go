// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

func TestReadWAVRoundTrip(t *testing.T) {
	pcm := make([]byte, 0, 32000)
	for i := 0; i < 16000; i++ {
		s := int16(math.Sin(float64(i)/20) * 0.5 * 32767)
		pcm = append(pcm, byte(s), byte(s>>8))
	}
	wav := speech.EncodeWAVWithSampleRate(pcm, 16000)
	path := filepath.Join(t.TempDir(), "r.wav")
	if err := os.WriteFile(path, wav, 0600); err != nil {
		t.Fatal(err)
	}
	got, rate, err := readWAV(path)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 {
		t.Fatalf("sample rate = %d, want 16000", rate)
	}
	if !bytes.Equal(got, pcm) {
		t.Fatalf("pcm mismatch: got %d bytes, want %d", len(got), len(pcm))
	}
}

func TestReadWAVRejectsNonWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.wav")
	if err := os.WriteFile(path, []byte("not a wav file at all"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readWAV(path); err == nil {
		t.Fatal("expected error for non-WAV")
	}
}

// buildTestPCM 构造 16kHz 16bit mono PCM：按 200ms 块（6400 字节）拼接。
// speech 块用固定幅度 0.5（RMS=0.5, peak=0.5），silence 块全零。
func buildTestPCM(blocks ...int) []byte {
	const chunkBytes = 16000 * 2 * 200 / 1000
	var pcm []byte
	for _, n := range blocks {
		blk := make([]byte, n*chunkBytes)
		pcm = append(pcm, blk...)
	}
	return pcm
}

// TestAnalyzeSpeechFeatures 覆盖：底噪、说话电平、中间停顿统计（开头/结尾静音不计）。
func TestAnalyzeSpeechFeatures(t *testing.T) {
	pcm := buildTestPCM(3, 5, 2, 5, 2, 5, 1) // 静3 话5 停2 话5 停2 话5 静1
	// 填入说话块（固定幅度 0.5）
	const chunkBytes = 16000 * 2 * 200 / 1000
	chunkIdx := 0
	for i := 0; i < len(pcm); i += chunkBytes {
		// 块序：0-2 静，3-7 话，8-9 静，10-14 话，15-16 静，17-21 话，22 静
		speech := (chunkIdx >= 3 && chunkIdx <= 7) || (chunkIdx >= 10 && chunkIdx <= 14) || (chunkIdx >= 17 && chunkIdx <= 21)
		if speech {
			for j := i; j < i+chunkBytes; j += 2 {
				pcm[j] = 0x00
				pcm[j+1] = 0x40 // 0.5 * 32767 ≈ 16384
			}
		}
		chunkIdx++
	}

	f := analyzeSpeechFeatures(pcm, 16000)
	if f.durationMs != 4600 {
		t.Fatalf("duration = %dms, want 4600", f.durationMs)
	}
	if math.Abs(f.noiseFloorRMS) > 0.0001 {
		t.Fatalf("noise floor = %v, want ~0", f.noiseFloorRMS)
	}
	if math.Abs(f.speechRMS-0.5) > 0.01 {
		t.Fatalf("speech RMS = %v, want ~0.5", f.speechRMS)
	}
	if len(f.gapsMs) != 2 {
		t.Fatalf("gaps = %v, want 2 pauses (400ms each)", f.gapsMs)
	}
	for _, g := range f.gapsMs {
		if g != 400 {
			t.Fatalf("gap = %dms, want 400ms", g)
		}
	}
	if f.gapMedianMs != 400 || f.gapP90Ms != 400 {
		t.Fatalf("gap median/P90 = %d/%d, want 400/400", f.gapMedianMs, f.gapP90Ms)
	}
	if f.chunkCount != 23 {
		t.Fatalf("chunk count = %d, want 23", f.chunkCount)
	}
	if math.Abs(f.speechRatio-15.0/23) > 0.01 {
		t.Fatalf("speech ratio = %v, want %v", f.speechRatio, 15.0/23)
	}
}

// TestBuildDiagnosticReport 断言报告包含特征、配置与 prompt 关键段。
func TestBuildDiagnosticReport(t *testing.T) {
	vc := config.Default().Voice
	f := speechFeatures{
		durationMs:    5000,
		noiseFloorRMS: 0.012,
		speechRMS:     0.2,
		gapsMs:        []int{400, 600, 1000},
		gapMedianMs:   600,
		gapP90Ms:      1000,
		speechPeak:    0.5,
		speechRatio:   0.6,
		chunkCount:    25,
	}
	report := buildDiagnosticReport("/tmp/x.wav", f, vc)
	for _, want := range []string{
		"# Just Talk Plus 参数诊断报告",
		"底噪 RMS",
		"停顿数量",
		"## 当前配置",
		"stop_delay_ms",
		"noise_deadband",
		"## 请大模型分析",
		"只输出 JSON",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q", want)
		}
	}
	if !strings.Contains(report, "600ms") {
		t.Fatal("report should contain gap median 600ms")
	}
}
