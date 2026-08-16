// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"sync"
	"sync/atomic"
	"time"
)

// AutoGain 自适应麦克风增益（AGC）：
//   - 削波（peak 接近满幅）或持续高电平 → 自动降低增益
//   - 长时间低语音 → 缓慢升回增益
//   - 细粒度步进（整数增益），可手动设置基准后继续自动调节
//
// 分析基于增益应用后的实际电平（recorder.Read 的输出），削波检测天然准确。
type AutoGain struct {
	enabled   bool
	gain      atomic.Int32
	target    float64 // 目标峰值，0..1
	clipLimit float64 // 削波阈值，0..1
	lowMin    float64 // 低语音 RMS 下界（低于此视为静音，不升增益）
	lowMax    float64 // 低语音 RMS 上界
	lowDelay  time.Duration
	stepDown  int32 // 削波降档步进
	minGain   int32
	maxGain   int32

	mu        sync.Mutex
	clipCount int
	hotCount  int
	lowSince  time.Time
	lowActive bool
}

// NewAutoGain 构造自适应增益；enabled 为开关，initial 为初始增益。
func NewAutoGain(enabled bool, initial int) *AutoGain {
	a := &AutoGain{
		enabled:   enabled,
		target:    0.70,
		clipLimit: 0.98,
		lowMin:    0.005,
		lowMax:    0.05,
		lowDelay:  1500 * time.Millisecond,
		stepDown:  2,
		minGain:   int32(MinGain),
		maxGain:   int32(MaxGain),
	}
	a.gain.Store(int32(clampGain(initial)))
	return a
}

func (a *AutoGain) Enabled() bool      { return a.enabled }
func (a *AutoGain) SetEnabled(on bool) { a.enabled = on }

// Gain 返回当前增益（并发安全）。
func (a *AutoGain) Gain() int { return int(a.gain.Load()) }

// SetGain 手动设定增益基准（如 TUI 微调后），自动调节从该值继续。
func (a *AutoGain) SetGain(g int) { a.gain.Store(int32(clampGain(g))) }

// Process 分析一块电平（peak/rms，0..1），按需调整增益；返回是否调整。
// now 用于低语音恢复的计时（跨块累计）。
func (a *AutoGain) Process(peak, rms float64, now time.Time) bool {
	if !a.enabled {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	g := a.gain.Load()
	changed := false
	switch {
	case peak >= a.clipLimit:
		// 削波：连续两块立即降档（不跨过下限）
		a.clipCount++
		a.hotCount = 0
		a.lowActive = false
		if a.clipCount >= 2 && g > a.minGain {
			g -= a.stepDown
			if g < a.minGain {
				g = a.minGain
			}
			a.clipCount = 0
			changed = true
		}
	case peak >= a.target:
		// 高电平未削波：持续约 1.5s 后小步降
		a.clipCount = 0
		a.hotCount++
		a.lowActive = false
		if a.hotCount >= 15 && g > a.minGain {
			g--
			a.hotCount = 0
			changed = true
		}
	case rms >= a.lowMin && rms < a.lowMax:
		// 低语音：持续 lowDelay 后缓慢升回
		a.clipCount = 0
		a.hotCount = 0
		if !a.lowActive {
			a.lowActive = true
			a.lowSince = now
		} else if now.Sub(a.lowSince) >= a.lowDelay && g < a.maxGain {
			g++
			a.lowSince = now
			changed = true
		}
	default:
		// 静音或正常电平：复位计数
		a.clipCount = 0
		a.hotCount = 0
		a.lowActive = false
	}
	if changed {
		a.gain.Store(g)
	}
	return changed
}
