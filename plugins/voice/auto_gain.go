// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
)

// AutoGain keeps speech in a useful range before it is sent to ASR. It gates
// adjustment on the unamplified input, so silence does not continually raise
// gain, while the output peak determines how quickly the gain is corrected.
type AutoGain struct {
	enabled bool
	gain    atomic.Int32

	targetPeak       float64
	minPeak          float64
	maxPeak          float64
	clipLimit        float64
	voicePeakMin     float64
	voiceRMSMin      float64
	maxRiseFactor    float64
	maxReleaseFactor float64
	minGain          int32
	maxGain          int32

	mu sync.Mutex
}

// AutoGainParams 是自适应增益的可调参数。除 TargetPeak/MaxPeak/MaxRise/MaxGain
// 可由 config.toml 覆盖外，其余参数由 DefaultAutoGainParams 提供保守默认值。
type AutoGainParams struct {
	TargetPeak       float64 // 目标输出峰值：弱信号被抬升到的大致电平
	MinPeak          float64 // 输出峰值低于此值才允许升高增益
	MaxPeak          float64 // 输出峰值超过此值开始回撤增益
	ClipLimit        float64 // 输出峰值硬上限，触达视为过载
	VoicePeakMin     float64 // 输入峰值门槛：低于视为非语音，不调整
	VoiceRMSMin      float64 // 输入 RMS 门槛：低于视为非语音，不调整
	MaxRiseFactor    float64 // 每块最大升高倍数（attack 上限，避免增益猛拉）
	MaxReleaseFactor float64 // 每块最大回撤比例（release 下限，避免骤降振荡）
	MinGain          int32   // 增益下限
	MaxGain          int32   // 增益上限：直接限制 AGC 能把增益拉到的最大值
}

// DefaultAutoGainParams 返回保守参数。相对早期版本（target 0.60 / 升 1.7 倍 /
// 上限 100 倍）收紧了目标电平、升高速度与增益上限，并给回撤加上限，
// 目标是压低过爆风险：宁可语音略小，也不能削波失真。
func DefaultAutoGainParams() AutoGainParams {
	return AutoGainParams{
		TargetPeak:       0.42,
		MinPeak:          0.24,
		MaxPeak:          0.65,
		ClipLimit:        0.96,
		VoicePeakMin:     0.008,
		VoiceRMSMin:      0.0015,
		MaxRiseFactor:    1.15,
		MaxReleaseFactor: 0.75,
		MinGain:          int32(MinGain),
		MaxGain:          25,
	}
}

// autoGainParamsFromVoice 把 config.toml 的可选字段叠加到保守默认值上。
// 未配置的字段保持默认；显式配置的字段生效（数值越界时钳制）。
func autoGainParamsFromVoice(vc config.VoiceConfig) AutoGainParams {
	p := DefaultAutoGainParams()
	if vc.AutoGainTargetPeak != nil {
		p.TargetPeak = clampPeak(*vc.AutoGainTargetPeak)
	}
	if vc.AutoGainMaxPeak != nil {
		p.MaxPeak = clampPeak(*vc.AutoGainMaxPeak)
	}
	if vc.AutoGainMaxRise != nil {
		p.MaxRiseFactor = clampRatio(*vc.AutoGainMaxRise)
	}
	if vc.AutoGainMaxGain != nil {
		p.MaxGain = int32(clampGain(*vc.AutoGainMaxGain))
	}
	return p
}

func clampPeak(v float64) float64 {
	if v < 0.05 {
		return 0.05
	}
	if v > 0.95 {
		return 0.95
	}
	return v
}

func clampRatio(v float64) float64 {
	if v < 1.01 {
		return 1.01
	}
	if v > 3.0 {
		return 3.0
	}
	return v
}

// NewAutoGain constructs an AGC controller with the conservative default
// parameters. The target leaves headroom for louder syllables while lifting
// consistently weak microphones slowly.
func NewAutoGain(enabled bool, initial int) *AutoGain {
	return NewAutoGainWithParams(enabled, initial, DefaultAutoGainParams())
}

// NewAutoGainWithParams constructs an AGC controller from explicit parameters.
func NewAutoGainWithParams(enabled bool, initial int, p AutoGainParams) *AutoGain {
	a := &AutoGain{
		enabled:          enabled,
		targetPeak:       p.TargetPeak,
		minPeak:          p.MinPeak,
		maxPeak:          p.MaxPeak,
		clipLimit:        p.ClipLimit,
		voicePeakMin:     p.VoicePeakMin,
		voiceRMSMin:      p.VoiceRMSMin,
		maxRiseFactor:    p.MaxRiseFactor,
		maxReleaseFactor: p.MaxReleaseFactor,
		minGain:          p.MinGain,
		maxGain:          p.MaxGain,
	}
	a.gain.Store(int32(clampGain(initial)))
	return a
}

func (a *AutoGain) Enabled() bool      { return a.enabled }
func (a *AutoGain) SetEnabled(on bool) { a.enabled = on }

// Gain returns the current multiplier.
func (a *AutoGain) Gain() int { return int(a.gain.Load()) }

// SetGain changes the manual baseline. Further automatic adjustment starts
// from this value.
func (a *AutoGain) SetGain(g int) { a.gain.Store(int32(clampGain(g))) }

// Process observes one PCM block. rawPeak/rawRMS are measured before gain,
// outputPeak after gain, and limited reports that the soft limiter or the
// per-block pre-scaling engaged. The time parameter is retained for callers
// that sample at explicit times.
func (a *AutoGain) Process(rawPeak, rawRMS, outputPeak float64, limited bool, _ time.Time) bool {
	if !a.enabled {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// Do not chase the noise floor. A weak microphone still clears both limits
	// during speech (for example peak 0.01 / RMS 0.002), while quiet room tone
	// normally does not.
	if rawPeak < a.voicePeakMin || rawRMS < a.voiceRMSMin {
		return false
	}

	current := a.gain.Load()
	next := current
	switch {
	case limited || outputPeak >= a.clipLimit:
		// Limiting/pre-scaling already protected this block from clipping, so we
		// can afford a bounded release instead of a one-shot drop: step toward
		// targetPeak-based gain but never release more than maxReleaseFactor per
		// block, avoiding the saw-tooth oscillation of a hard 0.65 cut.
		ratio := a.targetPeak / math.Max(outputPeak, 0.01)
		if ratio < a.maxReleaseFactor {
			ratio = a.maxReleaseFactor
		}
		next = int32(math.Floor(float64(current) * ratio))
	case outputPeak > a.maxPeak:
		ratio := a.targetPeak / outputPeak
		if ratio < a.maxReleaseFactor {
			ratio = a.maxReleaseFactor
		}
		next = int32(math.Floor(float64(current) * ratio))
	case outputPeak < a.minPeak:
		denominator := math.Max(outputPeak, 0.01)
		ratio := a.targetPeak / denominator
		if ratio > a.maxRiseFactor {
			ratio = a.maxRiseFactor
		}
		next = int32(math.Ceil(float64(current) * ratio))
	}

	if next < a.minGain {
		next = a.minGain
	}
	if next > a.maxGain {
		next = a.maxGain
	}
	if next == current {
		return false
	}
	a.gain.Store(next)
	return true
}
