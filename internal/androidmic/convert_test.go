// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package androidmic

import (
	"math"
	"testing"
)

func TestConvertI16SameRatePassthrough(t *testing.T) {
	// 16k i16 单声道：不应重采样，输出与原值一致
	in := make([]byte, 0, 64)
	var samples []int16
	for i := int16(0); i < 32; i++ {
		samples = append(samples, i*1000-16000)
	}
	for _, s := range samples {
		in = append(in, byte(s), byte(s>>8))
	}
	conv := newConverter()
	out, err := convertPacket(AudioPacket{
		Buffer: in, SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCM16BIT,
	}, conv)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len = %d, want %d", len(out), len(in))
	}
	for i := 0; i < len(out); i++ {
		if out[i] != in[i] {
			t.Fatalf("sample %d = %d, want %d", i, out[i], in[i])
		}
	}
}

func TestConvertStereoDownmix(t *testing.T) {
	// 双声道，左 0 右 20000 → 单声道 ≈ 10000
	var in []byte
	for i := 0; i < 16; i++ {
		left, right := int16(0), int16(20000)
		in = append(in, byte(left), byte(left>>8), byte(right), byte(right>>8))
	}
	conv := newConverter()
	out, err := convertPacket(AudioPacket{
		Buffer: in, SampleRate: 16000, ChannelCount: 2, AudioFormat: encPCM16BIT,
	}, conv)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	if len(out) != 32 { // 16 mono 帧 × 2 字节
		t.Fatalf("len = %d, want 32", len(out))
	}
	first := int16(out[0]) | int16(out[1])<<8
	if first != 10000 {
		t.Fatalf("downmix sample = %d, want 10000", first)
	}
}

func TestConvertResample48000To16000(t *testing.T) {
	// 48k 正弦 → 16k：输出长度约为输入 1/3
	const rate = 48000
	n := 4800 // 0.1s
	in := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		s := int16(math.Sin(2*math.Pi*440*float64(i)/rate) * 12000)
		in = append(in, byte(s), byte(s>>8))
	}
	conv := newConverter()
	out, err := convertPacket(AudioPacket{
		Buffer: in, SampleRate: rate, ChannelCount: 1, AudioFormat: encPCM16BIT,
	}, conv)
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	if len(out) < n*2/4 || len(out) > n*2/3+2 {
		t.Fatalf("resampled len = %d, want ~%d", len(out), n*2/3)
	}
}

func TestConvertResamplerContinuityAcrossFrames(t *testing.T) {
	// 跨帧连续：48k 常数信号重采样不应有阶跃（每帧末尾与开头应连续）
	conv := newConverter()
	const rate = 48000
	frame := make([]byte, 0, 480*2) // 10ms
	for i := 0; i < 480; i++ {
		s := int16(12000)
		frame = append(frame, byte(s), byte(s>>8))
	}
	var all []int16
	for i := 0; i < 10; i++ {
		out, err := convertPacket(AudioPacket{
			Buffer: frame, SampleRate: rate, ChannelCount: 1, AudioFormat: encPCM16BIT,
		}, conv)
		if err != nil {
			t.Fatalf("convert failed: %v", err)
		}
		for j := 0; j < len(out); j += 2 {
			all = append(all, int16(out[j])|int16(out[j+1])<<8)
		}
	}
	if len(all) < 100 {
		t.Fatalf("too few samples: %d", len(all))
	}
	// 常数输入输出应恒等于 12000（±1 量化误差）
	for i, v := range all {
		if v < 11999 || v > 12001 {
			t.Fatalf("sample %d = %d, want ~12000", i, v)
		}
	}
}

func TestConvertF32AndU8(t *testing.T) {
	conv := newConverter()
	// F32 满幅 1.0 → 32767
	var f32buf []byte
	for i := 0; i < 32; i++ {
		f32buf = append(f32buf, 0, 0, 0x80, 0x3f) // 1.0f
	}
	out, err := convertPacket(AudioPacket{
		Buffer: f32buf, SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCMFLOAT,
	}, conv)
	if err != nil {
		t.Fatalf("f32 convert failed: %v", err)
	}
	if v := int16(out[0]) | int16(out[1])<<8; v != 32767 {
		t.Fatalf("f32 1.0 = %d, want 32767", v)
	}

	// U8 255 → ~0.992 幅值 → ~32507
	conv2 := newConverter()
	u8buf := make([]byte, 32)
	for i := range u8buf {
		u8buf[i] = 255
	}
	out2, err := convertPacket(AudioPacket{
		Buffer: u8buf, SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCM8BIT,
	}, conv2)
	if err != nil {
		t.Fatalf("u8 convert failed: %v", err)
	}
	if v := int16(out2[0]) | int16(out2[1])<<8; v < 32400 || v > 32600 {
		t.Fatalf("u8 255 = %d, want ~32507", v)
	}
}

func TestConvertRejectsBadInput(t *testing.T) {
	conv := newConverter()
	cases := []AudioPacket{
		{Buffer: []byte{1, 2}, SampleRate: 16000, ChannelCount: 1, AudioFormat: 999},            // 未知格式
		{Buffer: []byte{1, 2, 3}, SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCM16BIT}, // 奇数长度
		{Buffer: []byte{1, 2}, SampleRate: 0, ChannelCount: 1, AudioFormat: encPCM16BIT},        // 采样率 0
	}
	for _, c := range cases {
		if _, err := convertPacket(c, conv); err == nil {
			t.Fatalf("expected error for %+v", c)
		}
	}
}

func TestResamplerIndependentInstances(t *testing.T) {
	// 不同采样率各用独立 converter，互不污染
	c48 := newConverter()
	c44 := newConverter()
	pkt := AudioPacket{Buffer: make([]byte, 480*2), SampleRate: 48000, ChannelCount: 1, AudioFormat: encPCM16BIT}
	for i := range pkt.Buffer {
		pkt.Buffer[i] = 0
	}
	out48, _ := convertPacket(pkt, c48)
	pkt.SampleRate = 44100
	pkt.Buffer = make([]byte, 441*2)
	out44, _ := convertPacket(pkt, c44)
	if len(out48) != 320 { // 480 采样 @48k → 160 输出 → 320 字节
		t.Fatalf("48k out len = %d, want 320", len(out48))
	}
	if len(out44) != 320 { // 441 采样 @44.1k → 160 输出 → 320 字节
		t.Fatalf("44.1k out len = %d, want 320", len(out44))
	}
}
