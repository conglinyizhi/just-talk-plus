// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package androidmic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// appendVarint 编码 varint 到 buf。
func appendVarint(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}

// encodeAudioPacket 按 protobuf wire format 编码 AudioPacketMessage。
func encodeAudioPacket(pkt AudioPacket) []byte {
	var buf []byte
	if len(pkt.Buffer) > 0 {
		buf = append(buf, 0x0a) // field 1, wire type 2
		buf = appendVarint(buf, uint64(len(pkt.Buffer)))
		buf = append(buf, pkt.Buffer...)
	}
	if pkt.SampleRate > 0 {
		buf = append(buf, 0x10) // field 2, wire type 0
		buf = appendVarint(buf, uint64(pkt.SampleRate))
	}
	if pkt.ChannelCount > 0 {
		buf = append(buf, 0x18) // field 3, wire type 0
		buf = appendVarint(buf, uint64(pkt.ChannelCount))
	}
	if pkt.AudioFormat > 0 {
		buf = append(buf, 0x20) // field 4, wire type 0
		buf = appendVarint(buf, uint64(pkt.AudioFormat))
	}
	return buf
}

func TestDecodeAudioPacket(t *testing.T) {
	// 与 AndroidMic Android 客户端一致：16k i16 单声道
	frame := encodeAudioPacket(AudioPacket{
		Buffer:       []byte{0x01, 0x02, 0x03, 0x04},
		SampleRate:   16000,
		ChannelCount: 1,
		AudioFormat:  encPCM16BIT,
	})
	pkt, err := decodeAudioPacket(frame)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !bytes.Equal(pkt.Buffer, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Fatalf("buffer = %v", pkt.Buffer)
	}
	if pkt.SampleRate != 16000 || pkt.ChannelCount != 1 || pkt.AudioFormat != encPCM16BIT {
		t.Fatalf("fields = %+v", pkt)
	}
}

func TestDecodeAudioPacketSkipsUnknownFields(t *testing.T) {
	// 在合法字段中间夹一个未知字段（field 9, wire 2），prost 会跳过
	frame := encodeAudioPacket(AudioPacket{Buffer: []byte{1, 2}, SampleRate: 48000, AudioFormat: encPCM16BIT})
	frame = append(frame, 0x4a) // field 9, wire 2
	frame = appendVarint(frame, 3)
	frame = append(frame, 'x', 'y', 'z')
	frame = append(frame, 0x18, 2) // channel_count = 2

	pkt, err := decodeAudioPacket(frame)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if pkt.SampleRate != 48000 || pkt.ChannelCount != 2 || pkt.AudioFormat != encPCM16BIT {
		t.Fatalf("fields = %+v", pkt)
	}
	if !bytes.Equal(pkt.Buffer, []byte{1, 2}) {
		t.Fatalf("buffer = %v", pkt.Buffer)
	}
}

func TestDecodeAudioPacketErrors(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		{0x0a, 0x05, 0x01}, // 长度越界
		{0x0a},             // 截断
		{0x80},             // varint 截断
	}
	for _, c := range cases {
		if _, err := decodeAudioPacket(c); err == nil {
			t.Fatalf("expected error for frame %v", c)
		}
	}
}

func TestReadFrame(t *testing.T) {
	body := []byte("hello androidmic")
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(body)))
	buf.Write(body)

	got, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("readFrame failed: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
}

func TestReadFrameRejectsHugeLength(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint32(maxFrameLen+1))
	if _, err := readFrame(&buf); err == nil {
		t.Fatal("expected error for oversized frame")
	}
}

func TestReadFrameEOF(t *testing.T) {
	if _, err := readFrame(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}
