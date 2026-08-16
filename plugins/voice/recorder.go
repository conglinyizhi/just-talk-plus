package voice

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
)

// 增益范围（s16le 乘数，细粒度调节）。
const (
	MinGain = 1
	MaxGain = 100
)

// Recorder captures audio from the default microphone as PCM 16kHz 16bit mono.
type Recorder struct {
	logger   *slog.Logger
	device   string
	gain     atomic.Int32
	mu       sync.Mutex
	readMu   sync.Mutex
	stdout   io.ReadCloser
	stopFunc func() error
	drainBuf bytes.Buffer
	started  bool
	backend  string
}

func clampGain(g int) int {
	if g < MinGain {
		return MinGain
	}
	if g > MaxGain {
		return MaxGain
	}
	return g
}

func NewRecorder(logger *slog.Logger, gain int) *Recorder {
	r := &Recorder{logger: logger}
	r.gain.Store(int32(clampGain(gain)))
	return r
}

func NewRecorderWithDevice(logger *slog.Logger, device string, gain int) *Recorder {
	r := &Recorder{logger: logger, device: device}
	r.gain.Store(int32(clampGain(gain)))
	return r
}

// Gain 返回当前增益（并发安全）。
func (r *Recorder) Gain() int { return int(r.gain.Load()) }

// SetGain 实时调整增益（并发安全，下次 Read 生效）。
func (r *Recorder) SetGain(g int) { r.gain.Store(int32(clampGain(g))) }

func (r *Recorder) Backend() string { return r.backend }

func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	stdout, name, stopFunc, err := startCaptureWithDevice(r.logger, r.device)
	if err != nil {
		return err
	}
	r.stdout = stdout
	r.stopFunc = stopFunc
	r.started = true
	r.backend = name
	r.logger.Info("recording started", "backend", name)
	return nil
}

func (r *Recorder) Read(p []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.mu.Lock()
	if r.drainBuf.Len() > 0 {
		n, _ := r.drainBuf.Read(p)
		r.mu.Unlock()
		return n, nil
	}
	stdout := r.stdout
	r.mu.Unlock()

	if stdout == nil {
		return 0, io.EOF
	}

	n, err := stdout.Read(p)
	if n > 0 {
		if g := r.Gain(); g > 1 {
			applyGain(p[:n], g)
		}
	}
	if errors.Is(err, os.ErrClosed) {
		err = io.EOF
	}
	return n, err
}

func (r *Recorder) Stop() ([]byte, error) {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return nil, nil
	}
	r.started = false
	stopFunc := r.stopFunc
	stdout := r.stdout
	r.stopFunc = nil
	r.stdout = nil
	r.mu.Unlock()

	if stopFunc != nil {
		_ = stopFunc()
	}

	r.readMu.Lock()
	defer r.readMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	if stdout != nil {
		buf := make([]byte, 640)
		g := r.Gain()
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if g > 1 {
					applyGain(buf[:n], g)
				}
				r.drainBuf.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}

	remaining := r.drainBuf.Bytes()
	r.drainBuf.Reset()
	r.logger.Info("recording stopped", "remaining_bytes", len(remaining))
	return remaining, nil
}

// applyGain multiplies s16le samples by gain factor, clamping to valid range.
func applyGain(pcm []byte, gain int) {
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(pcm[i]) | int16(pcm[i+1])<<8
		s32 := int32(s) * int32(gain)
		if s32 > 32767 {
			s32 = 32767
		} else if s32 < -32768 {
			s32 = -32768
		}
		pcm[i] = byte(s32)
		pcm[i+1] = byte(s32 >> 8)
	}
}

// analyzePCM 计算 s16le PCM 的峰值与 RMS（归一化到 0..1，含增益应用后的实际电平）。
func analyzePCM(pcm []byte) (peak, rms float64) {
	if len(pcm) < 2 {
		return 0, 0
	}
	var sum float64
	n := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		s := float64(int16(pcm[i]) | int16(pcm[i+1])<<8)
		a := s / 32767.0
		if a < 0 {
			a = -a
		}
		if a > peak {
			peak = a
		}
		sum += a * a
		n++
	}
	if n > 0 {
		rms = math.Sqrt(sum / float64(n))
	}
	return peak, rms
}
