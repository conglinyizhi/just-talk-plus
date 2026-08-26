// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package androidmic

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// startTestServer 启动服务端（随机端口）并注册清理。
func startTestServer(t *testing.T) (*Server, int) {
	t.Helper()
	s := New(nil)
	if err := s.Start(0); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, s.Port()
}

// dialClient 按 AndroidMic 协议握手，返回就绪的连接。
func dialClient(t *testing.T, port int) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Write([]byte(CHECK_1)); err != nil {
		t.Fatalf("handshake write failed: %v", err)
	}
	var resp [len(CHECK_2)]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		t.Fatalf("handshake read failed: %v", err)
	}
	if string(resp[:]) != CHECK_2 {
		t.Fatalf("handshake response = %q", resp[:])
	}
	return conn
}

func sendPacket(t *testing.T, conn net.Conn, pkt AudioPacket) {
	t.Helper()
	body := encodeAudioPacket(pkt)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := conn.Write(hdr[:]); err != nil {
		t.Fatalf("write hdr failed: %v", err)
	}
	if _, err := conn.Write(body); err != nil {
		t.Fatalf("write body failed: %v", err)
	}
}

// readFull 带超时读取，返回实际字节。
func readFull(src io.Reader, n int, timeout time.Duration) ([]byte, error) {
	got := make([]byte, 0, n)
	tmp := make([]byte, 256)
	deadline := time.Now().Add(timeout)
	for len(got) < n && time.Now().Before(deadline) {
		m, err := src.Read(tmp)
		if err != nil {
			return got, err
		}
		got = append(got, tmp[:m]...)
	}
	if len(got) < n {
		return got, errors.New("androidmic: 读取超时")
	}
	return got, nil
}

func TestServerRejectsBadHandshake(t *testing.T) {
	s, port := startTestServer(t)
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("WRONG!!!!!")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected connection close after bad handshake")
	}
	_ = s.Status() // 不 panic
}

func TestServerAudioFlow(t *testing.T) {
	s, port := startTestServer(t)
	conn := dialClient(t, port)

	src, err := s.OpenSource()
	if err != nil {
		t.Fatalf("open source failed: %v", err)
	}
	defer src.Close()

	pkt := AudioPacket{SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCM16BIT}
	for i := 0; i < 4; i++ {
		buf := make([]byte, 0, 320)
		for j := 0; j < 160; j++ {
			v := int16(1000)
			buf = append(buf, byte(v), byte(v>>8))
		}
		pkt.Buffer = buf
		sendPacket(t, conn, pkt)
	}

	got, err := readFull(src, 1280, 2*time.Second)
	if err != nil {
		t.Fatalf("read audio failed: %v", err)
	}
	if v := int16(got[0]) | int16(got[1])<<8; v != 1000 {
		t.Fatalf("first sample = %d, want 1000", v)
	}
}

func TestStatusIncludesBoundedSessionDiagnostics(t *testing.T) {
	s, port := startTestServer(t)
	for i := 0; i < maxRecentSessions+2; i++ {
		conn := dialClient(t, port)
		src, err := s.OpenSource()
		if err != nil {
			t.Fatal(err)
		}
		sendPacket(t, conn, AudioPacket{SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCM16BIT, Buffer: []byte{1, 0, 2, 0}})
		if _, err := readFull(src, 4, 2*time.Second); err != nil {
			t.Fatalf("read audio: %v", err)
		}
		_ = src.Close()
		_ = conn.Close()
	}

	deadline := time.Now().Add(2 * time.Second)
	var recent []SessionDiagnostic
	for time.Now().Before(deadline) {
		recent = s.Status().RecentSessions
		allDisconnected := len(recent) == maxRecentSessions
		for _, session := range recent {
			allDisconnected = allDisconnected && !session.DisconnectedAt.IsZero()
		}
		if allDisconnected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(recent) != maxRecentSessions {
		t.Fatalf("recent sessions = %d, want %d", len(recent), maxRecentSessions)
	}
	last := recent[len(recent)-1]
	if !last.HandshakeOK || last.PacketsReceived != 1 || last.PCMBytes != 4 || last.DeliveredFrames != 1 {
		t.Fatalf("last diagnostic = %+v", last)
	}
	if last.ConnectedAt.IsZero() || last.DisconnectedAt.IsZero() || last.LastPacketAt.IsZero() {
		t.Fatalf("timestamps were not recorded: %+v", last)
	}
}

func TestServerResamples48000(t *testing.T) {
	s, port := startTestServer(t)
	conn := dialClient(t, port)
	src, err := s.OpenSource()
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// 4800 采样 @48k = 0.1s → 1600 采样 @16k = 3200 字节
	buf := make([]byte, 0, 9600)
	for i := 0; i < 4800; i++ {
		v := int16(500)
		buf = append(buf, byte(v), byte(v>>8))
	}
	sendPacket(t, conn, AudioPacket{Buffer: buf, SampleRate: 48000, ChannelCount: 1, AudioFormat: encPCM16BIT})

	got, err := readFull(src, 3200, 2*time.Second)
	if err != nil {
		t.Fatalf("read resampled audio failed: %v", err)
	}
	if v := int16(got[0]) | int16(got[1])<<8; v != 500 {
		t.Fatalf("first sample = %d, want 500", v)
	}
}

func TestOpenSourceBeforeStart(t *testing.T) {
	s := New(nil)
	if _, err := s.OpenSource(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

func TestOpenSourceReplace(t *testing.T) {
	s, port := startTestServer(t)
	conn := dialClient(t, port)

	first, err := s.OpenSource()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.OpenSource() // 替换 first
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	sendPacket(t, conn, AudioPacket{SampleRate: 16000, ChannelCount: 1, AudioFormat: encPCM16BIT,
		Buffer: []byte{0x01, 0x00, 0x02, 0x00}})

	if _, err := first.Read(make([]byte, 4)); !errors.Is(err, io.EOF) {
		t.Fatalf("first read = %v, want EOF", err)
	}
	got, err := readFull(second, 4, 2*time.Second)
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	_ = got
}

func TestServerStopClosesEverything(t *testing.T) {
	s, port := startTestServer(t)
	conn := dialClient(t, port)
	src, err := s.OpenSource()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected client connection closed after Stop")
	}
	if _, err := src.Read(make([]byte, 4)); !errors.Is(err, io.EOF) {
		t.Fatalf("source read = %v, want EOF", err)
	}
	if err := s.Start(0); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	if !s.Status().Running {
		t.Fatal("server should be running after restart")
	}
}

func TestServerClientDisconnectKeepsServerAlive(t *testing.T) {
	s, port := startTestServer(t)
	conn := dialClient(t, port)
	_ = conn.Close()

	time.Sleep(50 * time.Millisecond)
	if !s.Status().Running {
		t.Fatal("server should keep running after client disconnect")
	}
	conn2 := dialClient(t, port)
	_ = conn2
	if !s.Status().Running {
		t.Fatal("server should keep running with new client")
	}
}

func TestWriteAudioAfterClose(t *testing.T) {
	s, _ := startTestServer(t)
	src, err := s.OpenSource()
	if err != nil {
		t.Fatal(err)
	}
	_ = src.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("writeAudio panicked: %v", r)
		}
	}()
	s.writeAudio(nil, []byte{1, 2, 3, 4})
	if !s.Status().Running {
		t.Fatal("server should stay running")
	}
}

func TestServerHandshakeTimeout(t *testing.T) {
	_, port := startTestServer(t)
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// 连接后不发送任何数据，服务端应在握手超时后关闭
	_ = conn.SetReadDeadline(time.Now().Add(7 * time.Second))
	start := time.Now()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected close after handshake timeout")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatalf("handshake timeout took too long: %v", time.Since(start))
	}
}
