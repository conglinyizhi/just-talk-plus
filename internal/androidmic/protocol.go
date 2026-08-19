// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// AndroidMic TCP 协议的 Go 实现。
// 协议规范与握手常量参考 AndroidMic（https://github.com/teamclouday/AndroidMic，
// GPL-3.0-only，作者 wiiznokes / teamclouday），按 Just Talk Plus 需求重写。

package androidmic

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// AndroidMic 桌面端 TCP 协议常量（与 AndroidMic 保持一致，Android 客户端
// 按此握手并发送长度前缀的 protobuf 消息）。
const (
	// CHECK_1 客户端连接后首先发送的魔术串，服务端据此校验。
	CHECK_1 = "AndroidMic1"
	// CHECK_2 服务端校验通过后回发的魔术串。
	CHECK_2 = "AndroidMic2"
	// DefaultPort 与 AndroidMic 桌面端默认端口一致。
	DefaultPort = 54345
	// DeviceName 是 voice 配置中 device 字段指向本服务端时使用的取值。
	DeviceName = "androidmic"
)

// Android 音频编码（AudioFormat.ENCODING_*）与字节大小。
const (
	encPCM16BIT = 2  // ENCODING_PCM_16BIT
	encPCM8BIT  = 3  // ENCODING_PCM_8BIT
	encPCMFLOAT = 4  // ENCODING_PCM_FLOAT
	encPCM24BIT = 21 // ENCODING_PCM_24BIT
	encPCM32BIT = 22 // ENCODING_PCM_32BIT
)

// AudioPacket 是 Android 客户端每次发送的音频帧（对应 AndroidMic 的
// AudioPacketMessage：buffer + sample_rate + channel_count + audio_format）。
type AudioPacket struct {
	Buffer       []byte
	SampleRate   uint32
	ChannelCount uint32
	AudioFormat  uint32
}

var errBadPacket = errors.New("androidmic: 无法解析音频包")

// decodeAudioPacket 解码一帧 protobuf AudioPacketMessage（prost 编码的 wire
// format）。字段缺失时按 proto3 默认值处理（空 buffer / 0 采样率等）；
// 未知字段一律跳过，与 prost 行为一致。
func decodeAudioPacket(frame []byte) (AudioPacket, error) {
	var pkt AudioPacket
	i := 0
	for i < len(frame) {
		key, n, err := readVarint(frame[i:])
		if err != nil {
			return pkt, errBadPacket
		}
		i += n
		field := key >> 3
		wire := key & 7
		if field == 0 {
			return pkt, errBadPacket
		}
		switch wire {
		case 0: // varint
			v, n, err := readVarint(frame[i:])
			if err != nil {
				return pkt, errBadPacket
			}
			i += n
			switch field {
			case 2:
				pkt.SampleRate = uint32(v)
			case 3:
				pkt.ChannelCount = uint32(v)
			case 4:
				pkt.AudioFormat = uint32(v)
			}
		case 2: // length-delimited
			l, n, err := readVarint(frame[i:])
			if err != nil || l > uint64(len(frame)-i-n) {
				return pkt, errBadPacket
			}
			i += n
			if field == 1 {
				pkt.Buffer = frame[i : i+int(l)]
			}
			i += int(l)
		case 1: // 64-bit
			if i+8 > len(frame) {
				return pkt, errBadPacket
			}
			i += 8
		case 5: // 32-bit
			if i+4 > len(frame) {
				return pkt, errBadPacket
			}
			i += 4
		default:
			return pkt, errBadPacket
		}
	}
	if len(pkt.Buffer) == 0 {
		return pkt, errBadPacket
	}
	return pkt, nil
}

// readVarint 读取一个 base-128 varint，返回数值与消耗字节数。
func readVarint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errBadPacket
}

// readFrame 从 r 读取一个长度前缀帧：4 字节大端长度 + 消息体。
// 返回消息体；连接关闭且无完整帧时返回 io.EOF。
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(hdr[:])
	if length == 0 || length > maxFrameLen {
		return nil, fmt.Errorf("androidmic: 非法帧长度 %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// maxFrameLen 单帧上限（64 MiB），防御异常客户端。
const maxFrameLen = 64 << 20
