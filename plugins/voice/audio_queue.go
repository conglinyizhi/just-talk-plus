// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"context"
	"io"
	"sync"
)

// audioChunk 是采集端产出的一段已应用增益的 PCM 数据及其电平。
type audioChunk struct {
	pcm        []byte
	rawPeak    float64
	rawRMS     float64
	gainedPeak float64
	gainedRMS  float64
	limited    bool
}

// audioQueue 将持续采集与 ASR 连接/发送解耦。它在单次录音期间保留按原始
// 顺序到达的音频，避免网络拨号或上游短暂阻塞反过来阻塞麦克风读取。
type audioQueue struct {
	mu     sync.Mutex
	chunks []audioChunk
	closed bool
	err    error
	notify chan struct{}
}

func newAudioQueue() *audioQueue {
	return &audioQueue{notify: make(chan struct{}, 1)}
}

func (q *audioQueue) Push(chunk audioChunk) {
	q.mu.Lock()
	if !q.closed {
		q.chunks = append(q.chunks, chunk)
	}
	q.mu.Unlock()
	q.signal()
}

func (q *audioQueue) Close(err error) {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		q.err = err
	}
	q.mu.Unlock()
	q.signal()
}

func (q *audioQueue) Next(ctx context.Context) (audioChunk, error) {
	for {
		q.mu.Lock()
		if len(q.chunks) > 0 {
			chunk := q.chunks[0]
			q.chunks[0] = audioChunk{}
			q.chunks = q.chunks[1:]
			q.mu.Unlock()
			return chunk, nil
		}
		if q.closed {
			err := q.err
			q.mu.Unlock()
			if err == nil {
				err = io.EOF
			}
			return audioChunk{}, err
		}
		notify := q.notify
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return audioChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

func (q *audioQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
