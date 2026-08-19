// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// AndroidMic 音频帧到 Just Talk Plus 录音管线的转换：
// 任意 Android 编码/采样率/声道 → 16 kHz 16-bit 单声道 PCM（ASR 标准输入）。

package androidmic

import (
	"fmt"
	"math"
)

// targetRate 是 Just Talk Plus ASR 管线要求的采样率（与本地 Recorder 一致）。
const targetRate = 16000

// sampleBytes 返回 Android 编码的每采样字节数；未知编码返回 0。
func sampleBytes(format uint32) int {
	switch format {
	case encPCM8BIT:
		return 1
	case encPCM16BIT:
		return 2
	case encPCM24BIT:
		return 3
	case encPCM32BIT, encPCMFLOAT:
		return 4
	}
	return 0
}

// convertPacket 把一帧 Android 音频转为 16 kHz 16-bit 单声道 PCM。
// conv 持有跨帧重采样状态（一次录音会话内复用）。
func convertPacket(pkt AudioPacket, conv *converter) ([]byte, error) {
	if conv == nil {
		conv = newConverter()
	}
	size := sampleBytes(pkt.AudioFormat)
	if size == 0 {
		return nil, fmt.Errorf("androidmic: 不支持的音频格式 %d", pkt.AudioFormat)
	}
	ch := int(pkt.ChannelCount)
	if ch < 1 {
		ch = 1
	}
	if pkt.SampleRate == 0 {
		return nil, fmt.Errorf("androidmic: 采样率为 0")
	}
	if size*ch == 0 || len(pkt.Buffer)%(size*ch) != 0 {
		return nil, fmt.Errorf("androidmic: 音频帧长度 %d 与格式不匹配", len(pkt.Buffer))
	}

	// 1) 解码为 f32 交错采样（-1..1）
	interleaved := decodeToF32(pkt.Buffer, pkt.AudioFormat, size)

	// 2) 下混到单声道
	var mono []float64
	if ch == 1 {
		mono = interleaved
	} else {
		frames := len(interleaved) / ch
		mono = make([]float64, frames)
		for i := 0; i < frames; i++ {
			var sum float64
			for c := 0; c < ch; c++ {
				sum += interleaved[i*ch+c]
			}
			mono[i] = sum / float64(ch)
		}
	}

	// 3) 重采样到 16 kHz
	var resampled []float64
	if int(pkt.SampleRate) == targetRate {
		resampled = mono
	} else {
		conv.rs.setRate(int(pkt.SampleRate), targetRate)
		resampled = conv.rs.process(mono)
	}

	// 4) 转 s16le
	out := make([]byte, 0, len(resampled)*2)
	for _, s := range resampled {
		v := s * 32767.0
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		i := int16(math.Round(v))
		out = append(out, byte(i), byte(i>>8))
	}
	return out, nil
}

// decodeToF32 按格式解码交错采样为 f32（-1..1）。
func decodeToF32(buf []byte, format uint32, size int) []float64 {
	n := len(buf) / size
	out := make([]float64, 0, n)
	switch format {
	case encPCM8BIT: // unsigned 8-bit
		for _, b := range buf {
			out = append(out, (float64(b)-128.0)/128.0)
		}
	case encPCM16BIT:
		for i := 0; i+1 < len(buf); i += 2 {
			s := int16(buf[i]) | int16(buf[i+1])<<8
			out = append(out, float64(s)/32767.0)
		}
	case encPCM24BIT:
		for i := 0; i+2 < len(buf); i += 3 {
			s := int32(buf[i]) | int32(buf[i+1])<<8 | int32(buf[i+2])<<16
			if s&0x800000 != 0 {
				s |= ^0xffffff // 符号扩展
			}
			out = append(out, float64(s)/8388607.0)
		}
	case encPCM32BIT:
		for i := 0; i+3 < len(buf); i += 4 {
			s := int32(uint32(buf[i]) | uint32(buf[i+1])<<8 | uint32(buf[i+2])<<16 | uint32(buf[i+3])<<24)
			out = append(out, float64(s)/2147483647.0)
		}
	case encPCMFLOAT:
		for i := 0; i+3 < len(buf); i += 4 {
			bits := uint32(buf[i]) | uint32(buf[i+1])<<8 | uint32(buf[i+2])<<16 | uint32(buf[i+3])<<24
			out = append(out, float64(math.Float32frombits(bits)))
		}
	}
	return out
}

// converter 是一次录音会话的转换状态（重采样器跨帧连续）。
type converter struct {
	rs resampler
}

func newConverter() *converter { return &converter{} }

// resampler 是流式线性插值重采样器，跨帧保持插值位置与上一帧末尾采样，
// 避免帧边界产生咔哒声。目标采样率低于输入时线性插值会引入混叠；
// 对语音识别（特征集中在低频段）影响可接受，后续可按需加前置低通。
type resampler struct {
	srcRate int
	dstRate int
	step    float64
	pos     float64 // 下一个输出采样对应的输入位置（相对当前帧）
	prev    float64 // 上一帧最后一个输入采样
	valid   bool    // 是否已有上一帧
}

func (r *resampler) setRate(src, dst int) {
	if r.srcRate != src || r.dstRate != dst {
		r.srcRate, r.dstRate = src, dst
		if dst > 0 {
			r.step = float64(src) / float64(dst)
		}
	}
}

func (r *resampler) process(in []float64) []float64 {
	n := len(in)
	if n == 0 || r.step <= 0 {
		return nil
	}
	count := 0
	for p := r.pos; p < float64(n); p += r.step {
		count++
	}
	if count == 0 {
		// 本帧不足以产出输出采样：更新 prev 后等待下一帧
		r.prev = in[n-1]
		r.valid = true
		r.pos -= float64(n)
		return nil
	}
	out := make([]float64, 0, count)
	p := r.pos
	for p < float64(n) {
		idx := int(p)
		frac := p - float64(idx)
		var y float64
		switch {
		case idx == 0 && r.valid:
			y = r.prev*(1-frac) + in[0]*frac
		case idx+1 < n:
			y = in[idx]*(1-frac) + in[idx+1]*frac
		default: // 帧尾常数延拓，下一帧 idx=0 时与 prev 衔接
			y = in[n-1]
		}
		out = append(out, y)
		p += r.step
	}
	r.pos = p - float64(n)
	r.prev = in[n-1]
	r.valid = true
	return out
}
