// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

func TestAudioQueuePreservesCaptureOrderBeforeConsumerStarts(t *testing.T) {
	queue := newAudioQueue()
	queue.Push(audioChunk{pcm: []byte{1, 2}})
	queue.Push(audioChunk{pcm: []byte{3}})
	queue.Push(audioChunk{pcm: []byte{4, 5, 6}})
	queue.Close(nil)

	var got []byte
	for range 3 {
		chunk, err := queue.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, chunk.pcm...)
	}
	if want := []byte{1, 2, 3, 4, 5, 6}; !bytes.Equal(got, want) {
		t.Fatalf("captured audio = %v, want %v", got, want)
	}
}

func TestStreamAudioDrainsQueuedAudioAfterDelayedConnection(t *testing.T) {
	queue := newAudioQueue()
	queue.Push(audioChunk{pcm: []byte{3, 4}, gainedPeak: 0.1, gainedRMS: 0.1})
	queue.Push(audioChunk{pcm: []byte{5, 6}, gainedPeak: 0.1, gainedRMS: 0.1})
	queue.Close(nil)
	client := &recordingStreamSession{}
	p := NewVoicePlugin()

	if err := p.streamAudio(context.Background(), client, []byte{1, 2}, queue, nil, time.Now(), 0, 0); err != nil {
		t.Fatal(err)
	}
	if want := []byte{1, 2, 3, 4, 5, 6}; !bytes.Equal(client.audio(), want) {
		t.Fatalf("sent audio = %v, want %v", client.audio(), want)
	}
}

// TestStreamAudioRecordsBelowThresholdAsIs 验证低于噪声阈值的块也原样发送并录制：
// 阈值仅用于开头开始录音与结束停止判定，中间不丢弃内容。
func TestStreamAudioSendsKeepaliveDuringCaptureGap(t *testing.T) {
	queue := newAudioQueue()
	client := &recordingStreamSession{}
	p := NewVoicePlugin()

	go func() {
		time.Sleep(audioKeepaliveEvery + 100*time.Millisecond)
		queue.Close(nil)
	}()
	if err := p.streamAudio(context.Background(), client, nil, queue, nil, time.Now(), 0, 0); err != nil {
		t.Fatal(err)
	}
	got := client.audio()
	if len(got) != speech.PCMByteRate/10 {
		t.Fatalf("keepalive bytes = %d, want %d", len(got), speech.PCMByteRate/10)
	}
	for i, b := range got {
		if b != 0 {
			t.Fatalf("keepalive byte %d = %d, want silence", i, b)
		}
	}
}

func TestStreamAudioRecordsBelowThresholdAsIs(t *testing.T) {
	queue := newAudioQueue()
	queue.Push(audioChunk{pcm: []byte{3, 4}, gainedPeak: 0.001, gainedRMS: 0.001})
	queue.Push(audioChunk{pcm: []byte{5, 6}, gainedPeak: 0.1, gainedRMS: 0.1})
	queue.Close(nil)
	client := &recordingStreamSession{}
	var audit bytes.Buffer
	client.SetAudioSentObserver(func(pcm []byte) { _, _ = audit.Write(pcm) })
	p := NewVoicePlugin()

	if err := p.streamAudio(context.Background(), client, []byte{1, 2}, queue, &audit, time.Now(), 0, 0); err != nil {
		t.Fatal(err)
	}
	if want := []byte{1, 2, 3, 4, 5, 6}; !bytes.Equal(audit.Bytes(), want) {
		t.Fatalf("audited audio = %v, want %v", audit.Bytes(), want)
	}
}

// TestStreamAudioRecordsVoiceToSendLatency 验证「按下录音 → 音频开始发送」的总延迟
// 在第一次音频成功发送后写入 p.voiceToSendMs：起点是 startedAt（按下录音时刻），
// 而不是检测到第一声的时刻。
func TestStreamAudioRecordsVoiceToSendLatency(t *testing.T) {
	queue := newAudioQueue()
	queue.Push(audioChunk{pcm: []byte{3, 4}, gainedPeak: 0.1, gainedRMS: 0.1})
	queue.Close(nil)
	client := &recordingStreamSession{}
	p := NewVoicePlugin()

	startedAt := time.Now().Add(-1500 * time.Millisecond)
	if err := p.streamAudio(context.Background(), client, []byte{1, 2}, queue, nil, startedAt, 0, 0); err != nil {
		t.Fatal(err)
	}
	if p.voiceToSendMs < 1400 || p.voiceToSendMs > 5000 {
		t.Fatalf("voiceToSendMs = %dms, want ~1500ms measured from startedAt", p.voiceToSendMs)
	}
}

type recordingStreamSession struct {
	mu        sync.Mutex
	pcm       []byte
	done      chan struct{}
	audioSent func([]byte)
}

func (s *recordingStreamSession) Connect(context.Context) error { return nil }
func (s *recordingStreamSession) SendAudio(_ context.Context, pcm []byte, _ bool) error {
	s.mu.Lock()
	s.pcm = append(s.pcm, pcm...)
	observer := s.audioSent
	s.mu.Unlock()
	if observer != nil {
		observer(pcm)
	}
	return nil
}
func (s *recordingStreamSession) SetAudioSentObserver(observer func([]byte)) {
	s.mu.Lock()
	s.audioSent = observer
	s.mu.Unlock()
}
func (s *recordingStreamSession) Results() <-chan speech.StreamResult { return nil }
func (s *recordingStreamSession) Done() <-chan struct{}               { return s.done }
func (s *recordingStreamSession) Final() <-chan struct{}              { return s.done }
func (s *recordingStreamSession) LastText() string                    { return "" }
func (s *recordingStreamSession) Close() error                        { return nil }
func (s *recordingStreamSession) audio() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.pcm...)
}

// TestStreamAudioStopsAtEngineAudioLimit 达到引擎单次上限时，streamAudio 必须
// 主动收手并触发停止，而不是继续录到用户自己按停。
func TestStreamAudioStopsAtEngineAudioLimit(t *testing.T) {
	// 上限设成 3 个 chunk 的量，队列里塞 10 个，验证它在第 3 个之后停下。
	const chunkBytes = 3200
	const limitMs = 3 * chunkBytes * 1000 / speech.PCMByteRate

	queue := newAudioQueue()
	for i := 0; i < 10; i++ {
		queue.Push(audioChunk{pcm: make([]byte, chunkBytes), gainedPeak: 0.1, gainedRMS: 0.1})
	}
	queue.Close(nil)

	client := &recordingStreamSession{}
	p := NewVoicePlugin()
	p.logger = slog.Default()
	p.recording = true
	p.sessionID = 1

	if err := p.streamAudio(context.Background(), client, nil, queue, nil, time.Now(), 1, limitMs); err != nil {
		t.Fatal(err)
	}

	if got := len(client.audio()); got != 3*chunkBytes {
		t.Fatalf("送达引擎 %d 字节, want %d（达到上限即停）", got, 3*chunkBytes)
	}
	if !p.audioLimitHit {
		t.Fatal("应记录上限命中，供状态/日志使用")
	}
}

// TestStreamAudioNoLimitKeepsDraining 未声明上限时照旧把队列读完（回归保护）。
func TestStreamAudioNoLimitKeepsDraining(t *testing.T) {
	const chunkBytes = 3200
	queue := newAudioQueue()
	for i := 0; i < 10; i++ {
		queue.Push(audioChunk{pcm: make([]byte, chunkBytes), gainedPeak: 0.1, gainedRMS: 0.1})
	}
	queue.Close(nil)

	client := &recordingStreamSession{}
	p := NewVoicePlugin()

	if err := p.streamAudio(context.Background(), client, nil, queue, nil, time.Now(), 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := len(client.audio()); got != 10*chunkBytes {
		t.Fatalf("无上限时应全部送达，实际 %d 字节", got)
	}
	if p.audioLimitHit {
		t.Fatal("无上限时不应标记命中")
	}
}
