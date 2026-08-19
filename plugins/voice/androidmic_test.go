// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/internal/androidmic"
)

// encodeTestPacket 编码 AudioPacketMessage protobuf 帧（测试用）。
func encodeTestPacket(buffer []byte, rate uint32) []byte {
	var buf []byte
	buf = append(buf, 0x0a) // field 1
	var l [binary.MaxVarintLen32]byte
	n := binary.PutUvarint(l[:], uint64(len(buffer)))
	buf = append(buf, l[:n]...)
	buf = append(buf, buffer...)
	buf = append(buf, 0x10) // field 2 sample_rate
	n = binary.PutUvarint(l[:], uint64(rate))
	buf = append(buf, l[:n]...)
	buf = append(buf, 0x18, 0x01) // field 3 channel_count = 1
	buf = append(buf, 0x20, 0x02) // field 4 audio_format = 2 (PCM_16BIT)
	return buf
}

// TestRecorderFromAndroidMicSource 端到端：device=androidmic 时，
// Recorder 从内嵌服务端读取客户端（模拟 Android 端）发送的音频。
// 用随机空闲端口启动服务端，避免与用户正在运行的实例（默认 54345）争端口。
func TestRecorderFromAndroidMicSource(t *testing.T) {
	_ = AndroidMicStop()
	port := freePort(t)
	if err := AndroidMicStart(port); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() { _ = AndroidMicStop() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := NewRecorderWithDevice(logger, AndroidMicDeviceName(), 1)
	if err := rec.Start(); err != nil {
		t.Fatalf("recorder start: %v", err)
	}
	defer func() { _, _ = rec.Stop() }()

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(androidmic.CHECK_1)); err != nil {
		t.Fatal(err)
	}
	var resp [len(androidmic.CHECK_2)]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		t.Fatal(err)
	}

	// 发一帧 16k i16 单声道（160 采样 = 10ms）
	body := encodeTestPacket(make([]byte, 320), 16000)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := conn.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(body); err != nil {
		t.Fatal(err)
	}

	// Recorder.Read 应拿到非零字节
	buf := make([]byte, 6400)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := rec.Read(buf)
		if n > 0 {
			return // 成功读到音频
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for audio from androidmic source")
}

// freePort 返回一个当前空闲的随机端口（先监听 :0 取号再释放）。
// 测试用它启动内嵌服务端，避免与用户正在运行的实例（默认 54345）争端口。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
