// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package androidmic

import (
	"io"
	"sync/atomic"
)

// queueFrames 消费者（Recorder）缓冲的音频帧数上限。队列满时丢弃新帧：
// 实时语音宁可丢帧也不要阻塞服务端或积累延迟。
const queueFrames = 64

// pipe 把服务端收到的 16 kHz 16-bit 单声道 PCM 暴露为 io.ReadCloser，
// 供 voice.Recorder 作为录音输入源消费。
type pipe struct {
	server *Server
	ch     chan []byte
	cur    []byte
	pos    int
	closed atomic.Bool
}

// Read 返回下一段 PCM，可跨服务端帧拼接；管道关闭后返回 io.EOF。
func (p *pipe) Read(dst []byte) (int, error) {
	for {
		if p.pos < len(p.cur) {
			n := copy(dst, p.cur[p.pos:])
			p.pos += n
			return n, nil
		}
		pcm, ok := <-p.ch
		if !ok {
			return 0, io.EOF
		}
		p.cur, p.pos = pcm, 0
	}
}

// Close 解除服务端引用并唤醒阻塞的 Read。幂等。
func (p *pipe) Close() error {
	if p.closed.CompareAndSwap(false, true) {
		p.server.detach(p)
	}
	return nil
}
