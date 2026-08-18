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
	limited  atomic.Bool
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
	n, _, _, _, _, _, err := r.read(p, false)
	return n, err
}

// readWithLevels reads PCM and reports both the unamplified input level and
// the level after the recorder's current gain has been applied. limited reports
// whether the soft limiter compressed this block.
func (r *Recorder) readWithLevels(p []byte) (n int, rawPeak, rawRMS, gainedPeak, gainedRMS float64, limited bool, err error) {
	return r.read(p, true)
}

func (r *Recorder) read(p []byte, measure bool) (n int, rawPeak, rawRMS, gainedPeak, gainedRMS float64, limited bool, err error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.mu.Lock()
	if r.drainBuf.Len() > 0 {
		n, _ = r.drainBuf.Read(p)
		r.mu.Unlock()
		if measure {
			gainedPeak, gainedRMS = analyzePCM(p[:n])
			return n, gainedPeak, gainedRMS, gainedPeak, gainedRMS, r.limited.Load(), nil
		}
		return n, 0, 0, 0, 0, r.limited.Load(), nil
	}
	stdout := r.stdout
	r.mu.Unlock()

	if stdout == nil {
		return 0, 0, 0, 0, 0, false, io.EOF
	}

	n, err = stdout.Read(p)
	if n > 0 {
		if measure {
			rawPeak, rawRMS = analyzePCM(p[:n])
		}
		if g := r.Gain(); g > 1 {
			scale := float64(g)
			if measure {
				// 响亮块先按 safePeakCap/rawPeak 整体线性预缩放，保留 headroom、
				// 避免逐样本软限幅造成的削波失真；limited 供 AGC 下一块回撤。
				scale, limited = effectiveGainForPeak(rawPeak, g)
			}
			limited = applyGain(p[:n], scale) || limited
		}
		r.limited.Store(limited)
		if measure {
			gainedPeak, gainedRMS = analyzePCM(p[:n])
		}
	}
	if errors.Is(err, os.ErrClosed) {
		err = io.EOF
	}
	return n, rawPeak, rawRMS, gainedPeak, gainedRMS, limited, err
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
					applyGain(buf[:n], float64(g))
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

// safePeakCap 是单块放大后的峰值上限。响亮块按 safePeakCap/rawPeak 整体线性
// 缩放并保留该 headroom，避免进入逐样本 soft limiter 造成当前块失真；AGC 在
// 下一块据此回撤增益。
const safePeakCap = 0.80

// effectiveGainForPeak 计算 rawPeak 对应的块可安全使用的实际缩放：
// min(配置/AGC gain, safePeakCap/rawPeak)。rawPeak <= 0（静音）不设上限、
// 不除零，原样返回配置增益。limited 为 true 表示安全增益低于配置增益
// （该块太响，需回撤下一块增益）。
func effectiveGainForPeak(rawPeak float64, configured int) (scale float64, limited bool) {
	if rawPeak <= 0 {
		return float64(configured), false
	}
	safe := safePeakCap / rawPeak
	if float64(configured) <= safe {
		return float64(configured), false
	}
	return safe, true
}

// applyGain multiplies s16le samples by scale and soft-limits any residual
// peaks before they reach hard clipping. It reports whether compression was
// applied to any sample. The measured read path pre-scales loud blocks with
// effectiveGainForPeak so they stay under safePeakCap linearly; the soft
// limiter here is a last-resort guard for unmeasured drain data.
func applyGain(pcm []byte, scale float64) (limited bool) {
	const softLimit = 0.92
	for i := 0; i+1 < len(pcm); i += 2 {
		s := float64(int16(pcm[i])|int16(pcm[i+1])<<8) / 32767.0
		x := s * scale
		abs := math.Abs(x)
		if abs > softLimit {
			limited = true
			compressed := softLimit + (1-softLimit)*(1-math.Exp(-(abs-softLimit)/(1-softLimit)))
			if x < 0 {
				x = -compressed
			} else {
				x = compressed
			}
		}
		v := int32(math.Round(x * 32767.0))
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		pcm[i] = byte(v)
		pcm[i+1] = byte(v >> 8)
	}
	return limited
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
