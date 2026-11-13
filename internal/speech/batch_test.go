// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type stubEngine struct {
	text    string
	avail   bool
	err     error
	logID   string // 非空时随 TranscribeResult 上报
	gotLang string
	gotCT   string
	gotWAV  []byte
	gotDur  int // 收到的 DurationMs
}

func (s *stubEngine) Name() string               { return "stub" }
func (s *stubEngine) Capabilities() Capabilities { return Capabilities{STT: true} }
func (s *stubEngine) Available(context.Context) (bool, string) {
	return s.avail, "stub detail"
}
func (s *stubEngine) Speak(context.Context, SpeakRequest) (*SpeakResult, error) {
	return nil, ErrUnsupported
}
func (s *stubEngine) ListVoices(context.Context) ([]Voice, error) {
	return nil, ErrUnsupported
}
func (s *stubEngine) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	s.gotLang = req.Lang
	s.gotCT = req.ContentType
	s.gotWAV = req.Audio
	s.gotDur = req.DurationMs
	if s.err != nil {
		return nil, s.err
	}
	return &TranscribeResult{Text: s.text, Engine: s.Name(), UpstreamLogID: s.logID}, nil
}

// blockingStub 阻塞直到 ctx 取消（模拟慢转写）。
type blockingStub struct{}

func (blockingStub) Name() string               { return "blocking" }
func (blockingStub) Capabilities() Capabilities { return Capabilities{STT: true} }
func (blockingStub) Available(context.Context) (bool, string) {
	return true, "ok"
}
func (blockingStub) Speak(context.Context, SpeakRequest) (*SpeakResult, error) {
	return nil, ErrUnsupported
}
func (blockingStub) ListVoices(context.Context) ([]Voice, error) {
	return nil, ErrUnsupported
}
func (blockingStub) Transcribe(ctx context.Context, _ TranscribeRequest) (*TranscribeResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEncodeWAV(t *testing.T) {
	pcm := []byte{0x01, 0x02, 0x03, 0x04}
	wav := EncodeWAV(pcm)
	if len(wav) != 44+len(pcm) {
		t.Fatalf("len=%d", len(wav))
	}
	if !bytes.Equal(wav[:4], []byte("RIFF")) || !bytes.Equal(wav[8:12], []byte("WAVE")) {
		t.Fatal("bad RIFF header")
	}
	if binary.LittleEndian.Uint32(wav[40:44]) != uint32(len(pcm)) {
		t.Fatal("bad data size")
	}
	if binary.LittleEndian.Uint32(wav[24:28]) != PCMRate {
		t.Fatalf("sample rate = %d", binary.LittleEndian.Uint32(wav[24:28]))
	}
	if !bytes.Equal(wav[44:], pcm) {
		t.Fatal("pcm payload mismatch")
	}
}

func TestBatchSessionTranscribesOnLast(t *testing.T) {
	eng := &stubEngine{avail: true, text: "你好世界"}
	sess := NewBatchSession(eng, StreamParams{Lang: "zh-CN"})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendAudio(ctx, []byte{1, 2, 3, 4}, false); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendAudio(ctx, []byte{5, 6}, true); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-sess.Results():
		if !res.IsFinal || res.Text != "你好世界" {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no result published")
	}

	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed")
	}
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed")
	}
	if sess.LastText() != "你好世界" {
		t.Fatalf("LastText = %q", sess.LastText())
	}
	if eng.gotLang != "zh-CN" {
		t.Fatalf("lang = %q", eng.gotLang)
	}
	if eng.gotCT != PCMContentType {
		t.Fatalf("content type = %q", eng.gotCT)
	}
	if len(eng.gotWAV) != 44+6 || !bytes.Equal(eng.gotWAV[:4], []byte("RIFF")) {
		t.Fatalf("engine did not receive wav payload (%d bytes)", len(eng.gotWAV))
	}
}

// TestBatchSessionReportsExactDuration 时长由缓冲的 PCM 字节数直接算出，
// 不依赖引擎解析容器：10 秒 PCM 应报 10000 毫秒。
func TestBatchSessionReportsExactDuration(t *testing.T) {
	const seconds = 10
	eng := &stubEngine{avail: true, text: "ok"}
	sess := NewBatchSession(eng, StreamParams{})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendAudio(ctx, make([]byte, PCMByteRate*seconds), true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed")
	}
	if eng.gotDur != seconds*1000 {
		t.Fatalf("DurationMs = %d, want %d", eng.gotDur, seconds*1000)
	}
}

// limitedStub 声明单次音频上限的引擎桩。
type limitedStub struct {
	stubEngine
	maxMs int
}

func (l *limitedStub) MaxAudioDurationMs() int { return l.maxMs }

// TestMaxAudioDurationMsHelper 上限读取：未实现或非正数都视为无上限。
func TestMaxAudioDurationMsHelper(t *testing.T) {
	if got := MaxAudioDurationMs(&stubEngine{}); got != 0 {
		t.Fatalf("未实现 AudioLimiter 应为 0，实际 %d", got)
	}
	if got := MaxAudioDurationMs(&limitedStub{maxMs: 120000}); got != 120000 {
		t.Fatalf("应读到 120000，实际 %d", got)
	}
	if got := MaxAudioDurationMs(&limitedStub{maxMs: -1}); got != 0 {
		t.Fatalf("负数上限应视为无上限，实际 %d", got)
	}
	if got := MaxAudioDurationMs(nil); got != 0 {
		t.Fatalf("nil 引擎应为 0，实际 %d", got)
	}
}

// TestBatchSessionTruncatesToEngineLimit 引擎声明上限后，超出部分的音频必须在
// 本地丢掉：宁可截断一段注定被拒的请求，也不要白跑一次往返。
func TestBatchSessionTruncatesToEngineLimit(t *testing.T) {
	const limitMs = 1000 // 1 秒 = PCMByteRate 字节
	eng := &limitedStub{stubEngine: stubEngine{avail: true, text: "ok"}, maxMs: limitMs}
	sess := NewBatchSession(eng, StreamParams{})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	// 送 3 秒音频，只应留下 1 秒。
	payload := make([]byte, PCMByteRate*3)
	if err := sess.SendAudio(ctx, payload, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed")
	}
	// 引擎收到的 WAV 减去 44 字节头就是实际转写的 PCM。
	gotPCM := len(eng.gotWAV) - 44
	if gotPCM != PCMByteRate {
		t.Fatalf("转写 PCM = %d 字节, want %d（1 秒）", gotPCM, PCMByteRate)
	}
	if eng.gotDur != limitMs {
		t.Fatalf("DurationMs = %d, want %d", eng.gotDur, limitMs)
	}
}

// TestBatchSessionNoLimitKeepsAll 未声明上限的引擎不受截断影响（回归保护）。
func TestBatchSessionNoLimitKeepsAll(t *testing.T) {
	eng := &stubEngine{avail: true, text: "ok"}
	sess := NewBatchSession(eng, StreamParams{})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendAudio(ctx, make([]byte, PCMByteRate*3), true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed")
	}
	if gotPCM := len(eng.gotWAV) - 44; gotPCM != PCMByteRate*3 {
		t.Fatalf("无上限引擎不应被截断，收到 %d 字节", gotPCM)
	}
}

// TestBatchSessionDurationZeroForEmpty 空音频不调用引擎，时长恒为 0。
func TestBatchSessionDurationZeroForEmpty(t *testing.T) {
	if got := pcmDurationMs(0); got != 0 {
		t.Fatalf("pcmDurationMs(0) = %d, want 0", got)
	}
	if got := pcmDurationMs(-5); got != 0 {
		t.Fatalf("pcmDurationMs(negative) = %d, want 0", got)
	}
	// 不足 1 毫秒的零头向下取整
	if got := pcmDurationMs(1); got != 0 {
		t.Fatalf("pcmDurationMs(1) = %d, want 0", got)
	}
}

// transcribeOnce 跑一次完整批处理会话（单包即最后一包），返回已拿到 Final 的会话。
func transcribeOnce(t *testing.T, eng *stubEngine) *BatchSession {
	t.Helper()
	sess := NewBatchSession(eng, StreamParams{})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendAudio(ctx, []byte{1, 2, 3, 4}, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed")
	}
	return sess
}

// TestBatchSessionExposesUpstreamLogID 批处理会话要把引擎给的追踪号透出去，
// 插件靠 speech.UpstreamLogID(session) 写进识别历史。
func TestBatchSessionExposesUpstreamLogID(t *testing.T) {
	sess := transcribeOnce(t, &stubEngine{avail: true, text: "ok", logID: "trace-abc"})
	if _, ok := any(sess).(TraceableSession); !ok {
		t.Fatal("*BatchSession should implement TraceableSession")
	}
	if got := UpstreamLogID(sess); got != "trace-abc" {
		t.Fatalf("UpstreamLogID = %q, want %q", got, "trace-abc")
	}
}

// TestBatchSessionUpstreamLogIDEmpty 引擎没给追踪号时必须是空串（历史不写该行）。
func TestBatchSessionUpstreamLogIDEmpty(t *testing.T) {
	sess := transcribeOnce(t, &stubEngine{avail: true, text: "ok"})
	if got := UpstreamLogID(sess); got != "" {
		t.Fatalf("UpstreamLogID = %q, want empty", got)
	}
}

// TestBatchSessionUpstreamLogIDConcurrentRead 插件在 Final() 关闭前后从别的
// goroutine 读追踪号；-race 下必须无数据竞争。
func TestBatchSessionUpstreamLogIDConcurrentRead(t *testing.T) {
	eng := &stubEngine{avail: true, text: "ok", logID: "trace-race"}
	sess := NewBatchSession(eng, StreamParams{})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = UpstreamLogID(sess)
				}
			}
		}()
	}

	if err := sess.SendAudio(ctx, []byte{1, 2, 3, 4}, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed")
	}
	got := UpstreamLogID(sess)
	close(stop)
	readers.Wait()
	if got != "trace-race" {
		t.Fatalf("UpstreamLogID = %q, want %q", got, "trace-race")
	}
}

func TestBatchSessionConnectUnavailable(t *testing.T) {
	sess := NewBatchSession(&stubEngine{avail: false}, StreamParams{})
	if err := sess.Connect(context.Background()); err == nil {
		t.Fatal("expected connect error for unavailable engine")
	}
}

func TestBatchSessionEmptyAudio(t *testing.T) {
	eng := &stubEngine{avail: true, text: "x"}
	sess := NewBatchSession(eng, StreamParams{})
	ctx := context.Background()
	_ = sess.Connect(ctx)
	if err := sess.SendAudio(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Final():
	case <-time.After(2 * time.Second):
		t.Fatal("Final not closed for empty audio")
	}
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed for empty audio")
	}
	select {
	case res, ok := <-sess.Results():
		if ok {
			t.Fatalf("unexpected result for empty audio: %+v", res)
		}
	default:
		t.Fatal("Results should be closed for empty audio")
	}
	if eng.gotWAV != nil {
		t.Fatal("engine should not be called for empty audio")
	}
}

func TestBatchSessionCloseCancelsTranscribe(t *testing.T) {
	sess := NewBatchSession(blockingStub{}, StreamParams{})
	ctx := context.Background()
	if err := sess.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendAudio(ctx, []byte{1, 2}, true); err != nil {
		t.Fatal(err)
	}
	// 给转写 goroutine 一点启动时间，然后关闭会话
	time.Sleep(20 * time.Millisecond)
	_ = sess.Close()
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed after Close")
	}
}

func TestEncodeWAVWithSampleRate(t *testing.T) {
	wav := EncodeWAVWithSampleRate([]byte{0, 0}, 8000)
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 8000 {
		t.Fatalf("sample rate = %d, want 8000", got)
	}
	if got := binary.LittleEndian.Uint32(wav[28:32]); got != 16000 {
		t.Fatalf("byte rate = %d, want 16000", got)
	}
}
