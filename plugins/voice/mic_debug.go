// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/c/just-talk-go/config"
)

// MicLevel 麦克风实时电平快照（供 TUI 调试面板显示）。
type MicLevel struct {
	Peak     float64 // 峰值 0..1
	RMS      float64 // 有效值 0..1
	Gain     int     // 当前增益
	Clipping bool    // 是否削波
	AutoGain bool    // 自适应增益开关
}

// micTestState 麦克风测试模式状态。
// 测试录音与正常录音共用同一 AutoGain 与增益，TUI 中的调整对后续正常录音即时生效。
type micTestState struct {
	active   bool // TUI 调试面板是否打开
	testing  bool // 测试录音是否进行中
	recorder *Recorder
	cancel   context.CancelFunc
	autoGain *AutoGain
	logger   *slog.Logger
	device   string
	cfg      *config.Config // 非 nil 时手动调增益会持久化到配置
	mu       sync.Mutex
	level    MicLevel
}

var micTest micTestState

// setupMicTest 由插件 Init 调用：初始化日志器、设备、配置引用与自适应增益。
func setupMicTest(logger *slog.Logger, cfg *config.Config, gain int, autoGainOn bool) {
	micTest.mu.Lock()
	defer micTest.mu.Unlock()
	micTest.logger = logger
	micTest.device = cfg.Voice.Device
	micTest.cfg = cfg
	if micTest.autoGain == nil {
		micTest.autoGain = NewAutoGain(autoGainOn, gain)
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
		n, err := rec.Read(buf)
		if n > 0 {
			peak, rms := analyzePCM(buf[:n])
			if micTest.autoGain.Process(peak, rms, time.Now()) {
				rec.SetGain(micTest.autoGain.Gain())
			}
			micTest.mu.Lock()
			micTest.level.Peak = peak
			micTest.level.RMS = rms
			micTest.level.Gain = micTest.autoGain.Gain()
			micTest.level.Clipping = peak >= 0.98
			micTest.level.AutoGain = micTest.autoGain.Enabled()
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
