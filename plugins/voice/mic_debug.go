// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
)

// MicLevel 麦克风实时电平快照（供 TUI 调试面板显示）。
type MicLevel struct {
	Peak             float64 // 原始输入峰值 0..1
	RMS              float64 // 原始输入 RMS 0..1
	GainedRMS        float64 // 应用增益后的 RMS 0..1
	Gain             int     // 当前增益
	Clipping         bool    // 应用增益后是否触发软限幅
	AutoGain         bool    // 自适应增益开关
	PeakHold         float64 // 原始输入峰值保持（0 = 无标记）
	RMSHold          float64 // 原始输入 RMS 保持（0 = 无标记）
	GainedRMSHold    float64 // 应用增益后 RMS 保持（0 = 无标记）
	NoiseThreshold   float64
	NoiseCalibrating bool
}

// micTestState 麦克风测试模式状态。
// 测试录音与正常录音共用同一 AutoGain 与增益，TUI 中的调整对后续正常录音即时生效。
type micTestState struct {
	active                  bool // TUI 调试面板是否打开
	testing                 bool // 测试录音是否进行中
	recorder                *Recorder
	cancel                  context.CancelFunc
	autoGain                *AutoGain
	logger                  *slog.Logger
	device                  string
	cfg                     *config.Config // 非 nil 时手动调增益会持久化到配置
	mu                      sync.Mutex
	level                   MicLevel
	noiseCalibrationVersion uint64
	peakHold                peakHoldTracker
	rmsHold                 peakHoldTracker
	gainedRMSHold           peakHoldTracker
}

var micTest micTestState

// peakHoldTracker 峰值保持：记录有信号期间的峰值并停住，静音超时后回落清零，
// 供麦克风调试面板绘制「最高峰」标记。
type peakHoldTracker struct {
	hold       float64
	lastSignal time.Time
	hasSignal  bool
}

const (
	// peakQuietThreshold 峰值低于该值视为无信号。
	peakQuietThreshold = 0.05
	// peakHoldDuration 无信号持续该时长后，保持的峰值回落清零。
	peakHoldDuration         = 1500 * time.Millisecond
	noiseCalibrationDuration = 3 * time.Second
	defaultNoiseThreshold    = 0.015
)

// StartNoiseCalibration 采集当前设备的底噪，并把建议 RMS 阈值保存到配置。
func StartNoiseCalibration() bool {
	ensureAutoGain()
	micTest.mu.Lock()
	if micTest.testing || micTest.recorder != nil || micTest.level.NoiseCalibrating {
		micTest.mu.Unlock()
		return false
	}
	logger, device, gain, cfg := micTest.logger, micTest.device, micTest.autoGain.Gain(), micTest.cfg
	micTest.level.NoiseCalibrating = true
	micTest.mu.Unlock()

	go func() {
		defer func() {
			micTest.mu.Lock()
			micTest.level.NoiseCalibrating = false
			micTest.mu.Unlock()
		}()
		rec := NewRecorderWithDevice(logger, device, gain)
		if err := rec.Start(); err != nil {
			pout("❌ 底噪检测失败: %v", err)
			return
		}
		defer rec.Stop()
		deadline := time.NewTimer(noiseCalibrationDuration)
		defer deadline.Stop()
		buf := make([]byte, 6400)
		levels := make([]float64, 0, 100)
		for {
			select {
			case <-deadline.C:
				threshold := calibratedNoiseThreshold(levels)
				micTest.mu.Lock()
				micTest.level.NoiseThreshold = threshold
				micTest.noiseCalibrationVersion++
				if cfg != nil {
					cfg.Voice.NoiseThreshold = threshold
				}
				micTest.mu.Unlock()
				if cfg != nil {
					_ = config.Save(cfg)
				}
				if len(levels) == 0 {
					pout("⚠️ 底噪检测完成，但没有采集到有效样本，已使用默认阈值 %.4f", threshold)
				} else {
					pout("✅ 底噪检测完成，噪声阈值已保存为 %.4f", threshold)
				}
				return
			default:
			}
			_, _, _, _, rms, _, err := rec.readWithLevels(buf)
			if err != nil {
				pout("❌ 底噪检测中断: %v", err)
				return
			}
			if rms > 0 {
				levels = append(levels, rms)
			}
		}
	}()
	return true
}

func calibratedNoiseThreshold(levels []float64) float64 {
	if len(levels) == 0 {
		return defaultNoiseThreshold
	}
	sort.Float64s(levels)
	index := int(float64(len(levels)-1) * 0.9)
	threshold := levels[index] * 2.5
	if threshold < 0.005 {
		threshold = 0.005
	}
	if threshold > 0.2 {
		threshold = 0.2
	}
	return threshold
}

// update 喂入一块峰值电平（peak 0..1，now 采样时间），返回当前保持的峰值。
func (p *peakHoldTracker) update(peak float64, now time.Time) float64 {
	if peak >= peakQuietThreshold {
		if peak > p.hold {
			p.hold = peak
		}
		p.lastSignal = now
		p.hasSignal = true
	} else if p.hasSignal && now.Sub(p.lastSignal) >= peakHoldDuration {
		p.hold = 0
		p.hasSignal = false
	}
	return p.hold
}

// setupMicTest 由插件 Init 调用：初始化日志器、设备、配置引用与自适应增益。
func setupMicTest(logger *slog.Logger, cfg *config.Config, gain int, autoGainOn bool) {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	micTest.logger = logger
	micTest.device = cfg.Voice.Device
	micTest.cfg = cfg
	micTest.level.NoiseThreshold = normalizeNoiseThreshold(cfg.Voice.NoiseThreshold)
	if micTest.autoGain == nil {
		micTest.autoGain = NewAutoGainWithParams(autoGainOn, gain, autoGainParamsFromVoice(cfg.Voice))
	}
}

// ensureAutoGain 惰性初始化自适应增益（插件未 Init 时兜底，便于测试）。
func ensureAutoGain() {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	if micTest.autoGain == nil {
		micTest.autoGain = NewAutoGain(false, 1)
	}
}

// StartMicTest 打开调试模式（TUI 面板进入时调用）。
func StartMicTest() {
	ensureAutoGain()
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	if micTest.active {
		return
	}
	micTest.active = true
	micTest.level.Gain = micTest.autoGain.Gain()
	micTest.level.AutoGain = micTest.autoGain.Enabled()
}

// StopMicTest 关闭调试模式并停止测试录音。
func StopMicTest() {
	stopMicTestRecording()
	micTest.mu.Lock()
	micTest.active = false
	micTest.mu.Unlock()
}

// MicTestActive 调试模式是否打开。
func MicTestActive() bool {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	return micTest.active
}

// MicTestTesting 测试录音是否进行中。
func MicTestTesting() bool {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	return micTest.testing
}

// MicTestLevel 返回实时电平快照。
func MicTestLevel() MicLevel {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	return micTest.level
}

// NoiseCalibrationSnapshot 返回最近一次成功完成的底噪校准版本与阈值。
// TUI 用版本号识别异步校准完成，并同步配置输入框和运行时语音设置。
func NoiseCalibrationSnapshot() (uint64, float64) {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	return micTest.noiseCalibrationVersion, micTest.level.NoiseThreshold
}

// GetMicGain 返回当前实际增益（含自适应增益的动态调整）。
func GetMicGain() int {
	ensureAutoGain()
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	return micTest.autoGain.Gain()
}

// SetMicGain 细粒度调整增益（1..100），实时生效并作为自适应增益的新基准；
// 配置引用存在时持久化到 [voice].gain。
func SetMicGain(g int) {
	ensureAutoGain()
	micTest.mu.Lock()
	if !micTest.active {
		micTest.mu.Unlock()
		return
	}
	micTest.autoGain.SetGain(g)
	if micTest.recorder != nil {
		micTest.recorder.SetGain(g)
	}
	micTest.level.Gain = micTest.autoGain.Gain()
	cfg := micTest.cfg
	micTest.mu.Unlock()
	if cfg != nil {
		cfg.Voice.Gain = clampGain(g)
		_ = config.Save(cfg)
	}
}

// MicTestDevice 返回当前调试用录音设备（"" = 自动/默认）。
func MicTestDevice() string {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	return micTest.device
}

// SetMicTestDevice 切换调试与正式录音设备：停止当前测试录音、
// 更新设备并持久化到 [voice].device（配置引用存在时）。
func SetMicTestDevice(dev string) {
	stopMicTestRecording()
	micTest.mu.Lock()
	micTest.device = dev
	cfg := micTest.cfg
	micTest.mu.Unlock()
	if cfg != nil {
		cfg.Voice.Device = dev
		_ = config.Save(cfg)
	}
}

// SetMicAutoGain 开/关自适应增益。
func SetMicAutoGain(on bool) {
	ensureAutoGain()
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	micTest.autoGain.SetEnabled(on)
	micTest.level.AutoGain = on
}

// toggleMicTest 由录音热键触发：开始或停止测试录音。
func toggleMicTest() {
	if MicTestTesting() {
		stopMicTestRecording()
	} else {
		startMicTestRecording()
	}
}

func startMicTestRecording() {
	micTest.mu.Lock()
	if !micTest.active || micTest.testing {
		micTest.mu.Unlock()
		return
	}
	micTest.peakHold = peakHoldTracker{}
	micTest.rmsHold = peakHoldTracker{}
	micTest.gainedRMSHold = peakHoldTracker{}
	gain := micTest.autoGain.Gain()
	device := micTest.device
	logger := micTest.logger
	micTest.mu.Unlock()

	var rec *Recorder
	if device != "" {
		rec = NewRecorderWithDevice(logger, device, gain)
	} else {
		rec = NewRecorder(logger, gain)
	}
	if err := rec.Start(); err != nil {
		pout("❌ 麦克风测试启动失败: %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	micTest.mu.Lock()
	micTest.testing = true
	micTest.recorder = rec
	micTest.cancel = cancel
	micTest.mu.Unlock()
	pout("🎤 麦克风测试开始（再按一次录音热键停止）")
	go micTestLoop(ctx, rec)
}

func micTestLoop(ctx context.Context, rec *Recorder) {
	buf := make([]byte, 6400)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, rawPeak, rawRMS, gainedPeak, gainedRMS, limited, err := rec.readWithLevels(buf)
		if n > 0 {
			now := time.Now()
			if micTest.autoGain.Process(rawPeak, rawRMS, gainedPeak, limited, now) {
				rec.SetGain(micTest.autoGain.Gain())
			}
			micTest.mu.Lock()
			micTest.level.Peak = rawPeak
			micTest.level.RMS = rawRMS
			micTest.level.GainedRMS = gainedRMS
			micTest.level.Gain = micTest.autoGain.Gain()
			micTest.level.Clipping = limited
			micTest.level.AutoGain = micTest.autoGain.Enabled()
			micTest.level.PeakHold = micTest.peakHold.update(rawPeak, now)
			micTest.level.RMSHold = micTest.rmsHold.update(rawRMS, now)
			micTest.level.GainedRMSHold = micTest.gainedRMSHold.update(gainedRMS, now)
			micTest.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func stopMicTestRecording() {
	micTest.mu.Lock()
	rec := micTest.recorder
	cancel := micTest.cancel
	micTest.testing = false
	micTest.recorder = nil
	micTest.cancel = nil
	micTest.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if rec != nil {
		_, _ = rec.Stop()
	}
	pout("🎤 麦克风测试停止")
}
