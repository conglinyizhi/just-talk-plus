// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package speech

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
)

// 录音器输出固定为 16 kHz 16-bit 单声道 PCM（s16le）。
const (
	PCMRate        = 16000
	PCMBits        = 16
	PCMChannels    = 1
	PCMBlockAlign  = PCMChannels * PCMBits / 8
	PCMByteRate    = PCMRate * PCMBlockAlign
	PCMContentType = "audio/wav"
)

// BatchSession 把只支持批处理 Transcribe 的引擎包装成 StreamSession：
// 录音期间缓冲 PCM，最后一包触发 Transcribe，完成后输出一条最终结果。
// 供 auralwise / openai 等非流式引擎使用。
type BatchSession struct {
	engine Engine
	params StreamParams

	mu      sync.Mutex
	buf     []byte
	started bool
	ctx     context.Context
	cancel  context.CancelFunc

	resultCh  chan StreamResult
	done      chan struct{}
	final     chan struct{}
	finalOnce sync.Once

	textMu    sync.RWMutex
	lastText  string
	audioSent func([]byte)

	// logMu 保护 upstreamLogID：转写 goroutine 写入，插件在 Final() 关闭后
	// 从别的 goroutine 读取，两侧都过锁，不依赖 channel close 的 happens-before。
	logMu         sync.RWMutex
	upstreamLogID string

	// maxBytes 是引擎声明的单次音频上限（字节），0 = 无上限。缓冲区到达上限
	// 后多余音频直接丢弃：宁可截断一段注定被拒的请求，也不要白跑一次往返。
	maxBytes int
}

// BatchSession 上报上游追踪号，满足 TraceableSession（与豆包流式会话一致）。
var _ TraceableSession = (*BatchSession)(nil)

// SetAudioSentObserver 设置已被批处理会话接受、将作为转写请求音频的数据观察器。
func (s *BatchSession) SetAudioSentObserver(observer func([]byte)) { s.audioSent = observer }

func (s *BatchSession) AudioSentSampleRate() int { return PCMRate }

// NewBatchSession 包装一个批处理引擎。
func NewBatchSession(engine Engine, params StreamParams) *BatchSession {
	ctx, cancel := context.WithCancel(context.Background())
	s := &BatchSession{
		engine:   engine,
		params:   params,
		ctx:      ctx,
		cancel:   cancel,
		resultCh: make(chan StreamResult, 4),
		done:     make(chan struct{}),
		final:    make(chan struct{}),
	}
	// 引擎若声明了时长上限，换算成字节上限并在这里兜底截断：录制侧会提前
	// 自动停止，但停止到真正静默之间还有尾音，绝不能让它把请求推过上限。
	if ms := MaxAudioDurationMs(engine); ms > 0 {
		s.maxBytes = ms * PCMByteRate / 1000
	}
	return s
}

// Connect 校验引擎可用性；不可用返回可读错误。
func (s *BatchSession) Connect(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	ok, detail := s.engine.Available(ctx)
	if !ok {
		return errors.New(detail)
	}
	return nil
}

// roomLocked 返回缓冲区还能接收多少字节；无上限时返回最大值。
func (s *BatchSession) roomLocked() int {
	if s.maxBytes <= 0 {
		return int(^uint(0) >> 1)
	}
	room := s.maxBytes - len(s.buf)
	if room < 0 {
		room = 0
	}
	return room
}

// SendAudio 缓冲音频；isLast=true 时触发批处理转写。
func (s *BatchSession) SendAudio(ctx context.Context, pcm []byte, isLast bool) error {
	if err := ctx.Err(); err != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	if len(pcm) > 0 {
		// 到达引擎上限后只收能装下的部分：审计录音（audioSent）与被转写的
		// 音频必须一致，所以截断在两者之前做。
		if room := s.roomLocked(); room < len(pcm) {
			pcm = pcm[:room]
		}
		if len(pcm) > 0 {
			s.buf = append(s.buf, pcm...)
			if s.audioSent != nil {
				s.audioSent(pcm)
			}
		}
	}
	if !isLast {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	audio := s.buf
	s.buf = nil
	sessCtx := s.ctx
	s.mu.Unlock()

	go s.transcribe(sessCtx, audio)
	return nil
}

func (s *BatchSession) transcribe(ctx context.Context, audio []byte) {
	defer close(s.done)
	defer close(s.resultCh)
	s.finish(ctx, audio)
}

func (s *BatchSession) finish(ctx context.Context, audio []byte) {
	// 空录音直接结束，不调用引擎。
	if len(audio) == 0 {
		s.finalOnce.Do(func() { close(s.final) })
		return
	}
	req := TranscribeRequest{
		Audio:       EncodeWAV(audio),
		ContentType: PCMContentType,
		Lang:        s.params.Lang,
		// 时长是已知量：缓冲的 PCM 字节数除以每秒字节数，不必让引擎解析 WAV。
		DurationMs: pcmDurationMs(len(audio)),
	}
	result, err := s.engine.Transcribe(ctx, req)
	// 引擎返回的追踪号先落盘再发结果：插件在 Final() 关闭后立刻读取。
	if result != nil && result.UpstreamLogID != "" {
		s.logMu.Lock()
		s.upstreamLogID = result.UpstreamLogID
		s.logMu.Unlock()
	}
	if err != nil {
		select {
		case s.resultCh <- StreamResult{Error: err}:
		case <-ctx.Done():
		}
		s.finalOnce.Do(func() { close(s.final) })
		return
	}
	text := result.Text
	if text != "" {
		s.textMu.Lock()
		s.lastText = text
		s.textMu.Unlock()
	}
	select {
	case s.resultCh <- StreamResult{Text: text, IsFinal: true}:
	case <-ctx.Done():
	}
	s.finalOnce.Do(func() { close(s.final) })
}

// Results 返回结果流（批处理引擎仅一条最终结果）。
func (s *BatchSession) Results() <-chan StreamResult { return s.resultCh }

func (s *BatchSession) Done() <-chan struct{}  { return s.done }
func (s *BatchSession) Final() <-chan struct{} { return s.final }

func (s *BatchSession) LastText() string {
	s.textMu.RLock()
	defer s.textMu.RUnlock()
	return s.lastText
}

// UpstreamLogID 返回本次转写的上游追踪 ID；引擎未提供时为空字符串。
// 插件在 Final() 关闭后调用，因此读路径与 finish() 的写入都加锁。
func (s *BatchSession) UpstreamLogID() string {
	s.logMu.RLock()
	defer s.logMu.RUnlock()
	return s.upstreamLogID
}

// Close 取消进行中的转写。
func (s *BatchSession) Close() error {
	s.cancel()
	return nil
}

// pcmDurationMs 由 16 kHz 16-bit 单声道 PCM 的字节数算出毫秒时长。
// 录制器固定输出该格式，所以这里不需要解析容器；向下取整，不足 1 毫秒记 0。
func pcmDurationMs(n int) int {
	if n <= 0 {
		return 0
	}
	return n * 1000 / PCMByteRate
}

// EncodeWAV 把 16 kHz 16-bit 单声道 PCM 打包成 WAV（RIFF/WAVE）。
func EncodeWAV(pcm []byte) []byte { return EncodeWAVWithSampleRate(pcm, PCMRate) }

// EncodeWAVWithSampleRate 把单声道 16-bit PCM 打包成给定采样率的 WAV。
func EncodeWAVWithSampleRate(pcm []byte, sampleRate int) []byte {
	if sampleRate <= 0 {
		sampleRate = PCMRate
	}
	dataLen := len(pcm)
	out := make([]byte, 44+dataLen)
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataLen))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)                               // fmt chunk size
	binary.LittleEndian.PutUint16(out[20:22], 1)                                // PCM
	binary.LittleEndian.PutUint16(out[22:24], PCMChannels)                      // channels
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))               // sample rate
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*PCMBlockAlign)) // byte rate
	binary.LittleEndian.PutUint16(out[32:34], PCMBlockAlign)
	binary.LittleEndian.PutUint16(out[34:36], PCMBits)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataLen))
	copy(out[44:], pcm)
	return out
}
