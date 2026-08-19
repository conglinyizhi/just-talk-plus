// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// AndroidMic TCP 服务端：监听端口接收 Android 设备音频，转成 16 kHz
// 16-bit 单声道 PCM 供 Just Talk Plus 录音管线消费。
//
// 隔离设计：服务端所有 goroutine 自带 panic 恢复，任何内部错误只影响
// AndroidMic 服务本身，不会把主应用带崩。

package androidmic

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"
)

// ErrNotRunning 表示服务端未启动（录音指向 Android 设备时需要先启动服务端）。
var ErrNotRunning = errors.New("androidmic: 服务端未运行，请先在输入设置启动")

// handshakeTimeout 客户端握手超时；防御连接后不发送校验串的僵尸连接。
const handshakeTimeout = 5 * time.Second

// Status 是服务端状态快照（供 TUI 展示与管理）。
type Status struct {
	Running bool
	Port    int
	Addr    string   // 监听地址（含端口）
	Clients []string // 已连接客户端地址
	Active  bool     // 当前是否有录音消费者挂接
}

// Server 是 AndroidMic TCP 服务端。所有方法并发安全。
type Server struct {
	logger *slog.Logger
	mu     sync.Mutex
	ln     net.Listener
	port   int
	conns  map[net.Conn]struct{}
	active *pipe
	closed chan struct{} // 关闭信号：通知 accept 循环退出
}

// New 创建服务端实例。logger 为 nil 时使用默认 logger。
func New(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{logger: logger, conns: make(map[net.Conn]struct{})}
}

// Start 启动监听。已在监听时幂等返回 nil。
func (s *Server) Start(port int) error {
	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		return nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("androidmic: 端口 %d 监听失败: %w", port, err)
	}
	s.ln = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.closed = make(chan struct{})
	s.mu.Unlock()

	s.logger.Info("androidmic server listening", "port", port)
	go s.acceptLoop(ln)
	return nil
}

// Stop 停止监听、断开所有客户端并解除消费者挂接。幂等。
func (s *Server) Stop() error {
	s.mu.Lock()
	ln := s.ln
	s.ln = nil
	s.port = 0
	if s.closed != nil {
		close(s.closed)
		s.closed = nil
	}
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = make(map[net.Conn]struct{})
	active := s.active
	s.active = nil
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if active != nil {
		_ = active.Close()
	}
	s.logger.Info("androidmic server stopped")
	return nil
}

// Port 返回当前监听端口（未运行返回 0）。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Status 返回状态快照。
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Running: s.ln != nil, Port: s.port, Active: s.active != nil}
	if s.ln != nil {
		st.Addr = s.ln.Addr().String()
	}
	for c := range s.conns {
		st.Clients = append(st.Clients, c.RemoteAddr().String())
	}
	sort.Strings(st.Clients)
	return st
}

// OpenSource 挂接一个音频消费者（一次录音会话一个），返回 16 kHz 16-bit
// 单声道 PCM 的读取端。服务端未运行时返回 ErrNotRunning。
// 同一时刻只允许一个消费者：再次调用会关闭上一个挂接。
func (s *Server) OpenSource() (io.ReadCloser, error) {
	s.mu.Lock()
	if s.ln == nil {
		s.mu.Unlock()
		return nil, ErrNotRunning
	}
	old := s.active
	s.active = nil
	s.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	p := &pipe{server: s, ch: make(chan []byte, queueFrames)}
	s.mu.Lock()
	s.active = p
	s.mu.Unlock()
	return p, nil
}

// writeAudio 把一帧 PCM 交给当前消费者；无消费者或队列满则丢弃。
// 持锁期间完成非阻塞写入，与 detach 的 close 互斥，杜绝写已关闭 channel。
func (s *Server) writeAudio(pcm []byte) {
	s.mu.Lock()
	p := s.active
	if p == nil || p.closed.Load() {
		s.mu.Unlock()
		return
	}
	select {
	case p.ch <- pcm:
	default:
	}
	s.mu.Unlock()
}

// detach 解除 pipe 引用并关闭其 channel。幂等；close 在锁外执行，
// 因 active 已置空，writeAudio 不可能再向该 channel 写入。
func (s *Server) detach(p *pipe) {
	s.mu.Lock()
	if s.active == p {
		s.active = nil
	}
	s.mu.Unlock()
	close(p.ch)
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed == nil
}

// acceptLoop 接受连接并派发处理 goroutine。panic 只停掉服务端，不波及主应用。
func (s *Server) acceptLoop(ln net.Listener) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("androidmic: 内部错误已隔离，服务端停止", "panic", r)
			_ = s.Stop()
		}
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.isClosed() {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			s.logger.Error("androidmic: accept 失败", "error", err)
			_ = s.Stop()
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handleConn(conn)
	}
}

// handleConn 处理单个客户端：握手 → 循环读帧 → 转换 → 投递。
func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("androidmic: 连接处理异常已隔离",
				"remote", conn.RemoteAddr(), "panic", r)
		}
	}()
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	remote := conn.RemoteAddr().String()
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	var buf [len(CHECK_1)]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		s.logger.Debug("androidmic: 握手读取失败", "remote", remote, "error", err)
		return
	}
	if string(buf[:]) != CHECK_1 {
		s.logger.Warn("androidmic: 握手校验失败", "remote", remote)
		return
	}
	if _, err := conn.Write([]byte(CHECK_2)); err != nil {
		s.logger.Debug("androidmic: 握手回写失败", "remote", remote, "error", err)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	s.logger.Info("androidmic: 客户端已连接", "remote", remote)

	conv := newConverter()
	for {
		frame, err := readFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Debug("androidmic: 读帧失败", "remote", remote, "error", err)
			}
			return
		}
		pkt, err := decodeAudioPacket(frame)
		if err != nil {
			s.logger.Debug("androidmic: 音频包解码失败", "remote", remote, "error", err)
			continue
		}
		pcm, err := convertPacket(pkt, conv)
		if err != nil {
			s.logger.Debug("androidmic: 音频转换失败", "remote", remote, "error", err)
			continue
		}
		s.writeAudio(pcm)
	}
}
