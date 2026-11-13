// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package deepseek

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

func TestParseConfigDefaults(t *testing.T) {
	c, err := ParseConfig(map[string]string{"api_key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("base = %s", c.BaseURL)
	}
}

func TestParseConfigCustomEndpoint(t *testing.T) {
	c, err := ParseConfig(map[string]string{
		"api_key":  "k",
		"base_url": "http://127.0.0.1:8000/v1/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://127.0.0.1:8000/v1" {
		t.Fatalf("base = %s", c.BaseURL)
	}
}

func TestParseConfigRequiresKey(t *testing.T) {
	if _, err := ParseConfig(map[string]string{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NewFromMap(map[string]string{"api_key": "   "}); err == nil {
		t.Fatal("expected error for blank api_key")
	}
}

// TestParseConfigIgnoresModel 接口没有模型参数，配置里带 model 不影响构建。
func TestParseConfigIgnoresModel(t *testing.T) {
	c, err := ParseConfig(map[string]string{"api_key": "k", "model": "whatever", "language": "zh"})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != defaultBaseURL {
		t.Fatalf("base = %s", c.BaseURL)
	}
}

func TestAvailable(t *testing.T) {
	e, err := NewFromMap(map[string]string{"api_key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	ok, detail := e.Available(nil)
	if !ok {
		t.Fatalf("ok = false, detail = %q", detail)
	}
	if !strings.Contains(detail, "api.deepseek.com") || !strings.Contains(detail, "语种自动识别") {
		t.Fatalf("detail = %q", detail)
	}
	if e.Name() != "deepseek" {
		t.Fatalf("name = %s", e.Name())
	}
	if _, err := e.Speak(context.Background(), speech.SpeakRequest{Text: "hi"}); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("Speak err = %v", err)
	}
	if _, err := e.ListVoices(context.Background()); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("ListVoices err = %v", err)
	}
}

// TestTranscribeRequestShape 成功路径：只发 file，Bearer 鉴权，解析 text 与 request_id。
func TestTranscribeRequestShape(t *testing.T) {
	var gotAuth, gotPath, gotMethod, gotFilename, gotAudio string
	var gotValueFields []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// 表单只允许 file 一个字段：model / language / response_format 接口都没有。
		for name := range r.MultipartForm.Value {
			gotValueFields = append(gotValueFields, name)
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		gotFilename = hdr.Filename
		raw, _ := io.ReadAll(f)
		gotAudio = string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(traceIDHeader, "trace-header")
		_, _ = w.Write([]byte(`{"request_id":"req-123","text":"  上海今天天气怎么样？  ","duration_ms":3200}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "sk-test", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio:       []byte("fake-wav"),
		ContentType: speech.PCMContentType,
		Lang:        "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "上海今天天气怎么样？" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Engine != "deepseek" {
		t.Fatalf("engine = %q", res.Engine)
	}
	// 追踪号优先取响应体的 request_id，请求头那份不是首选。
	if res.UpstreamLogID != "req-123" {
		t.Fatalf("upstream log id = %q, want req-123", res.UpstreamLogID)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/tools/asr" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotFilename != "recording.wav" {
		t.Fatalf("filename = %q", gotFilename)
	}
	if gotAudio != "fake-wav" {
		t.Fatalf("audio = %q", gotAudio)
	}
	if len(gotValueFields) != 0 {
		t.Fatalf("form value fields = %v, want none (only file)", gotValueFields)
	}
}

// TestTranscribeUpstreamLogIDFallsBackToTraceHeader 响应体没有 request_id 时回退响应头。
func TestTranscribeUpstreamLogIDFallsBackToTraceHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(traceIDHeader, "header-trace")
		_, _ = w.Write([]byte(`{"text":"ok","duration_ms":100}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if res.UpstreamLogID != "header-trace" {
		t.Fatalf("upstream log id = %q, want header-trace", res.UpstreamLogID)
	}
}

// TestTranscribeOggFilename 接口只接受 WAV 与 Ogg Opus，非 WAV 按 Ogg 命名，不发 .mp3。
func TestTranscribeOggFilename(t *testing.T) {
	var gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotFilename = hdr.Filename
		_, _ = w.Write([]byte(`{"request_id":"r","text":"ok"}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio:       append([]byte("OggS"), make([]byte, 1024)...),
		ContentType: "audio/ogg",
	}); err != nil {
		t.Fatal(err)
	}
	if gotFilename != "recording.opus" {
		t.Fatalf("filename = %q", gotFilename)
	}
}

// TestTranscribeHTTPErrorNestedError 实测（假 key）响应：HTTP 401，响应头带 x-ds-trace-id，
// body 是 OpenAI 风格的嵌套 error 对象（说明不在顶层，code 是字符串）。
func TestTranscribeHTTPErrorNestedError(t *testing.T) {
	const trace = "d06c0f4b-2f47-4b0e-9f35-6c1f9d0a1e77"
	const msg = "Authentication Fails, Your api key: ****abcd is invalid (request_id: d06c0f4b-2f47-4b0e-9f35-6c1f9d0a1e77)"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(traceIDHeader, trace)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"` + msg + `","type":"authentication_error","param":null,"code":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "bad", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "deepseek transcribe HTTP 401: " + msg + " (code=invalid_request_error; trace=" + trace + ")"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TestTranscribeHTTPError429 并发超限：错误串带 code 与 trace。
func TestTranscribeHTTPError429(t *testing.T) {
	const trace = "trace-429"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(traceIDHeader, trace)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"账号同时进行的请求数超过上限","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "deepseek transcribe HTTP 429: 账号同时进行的请求数超过上限 (code=rate_limit_exceeded; trace=trace-429)"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TestTranscribeHTTPErrorWithoutTrace 响应头没有 trace 时，错误串保持最小。
func TestTranscribeHTTPErrorWithoutTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"服务繁忙，请稍后重试","type":"server_error","code":"service_unavailable"}}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "deepseek transcribe HTTP 503: 服务繁忙，请稍后重试 (code=service_unavailable)"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TestTranscribeHTTPErrorTypeOnly 错误对象只给了 type（没有 message）时退回 type，别只剩状态码。
func TestTranscribeHTTPErrorTypeOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"type":"gateway_error"}}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "deepseek transcribe HTTP 502: gateway_error"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TestTranscribeHTTPErrorPlainTextBody 非 JSON 错误体（网关页面）回退原始内容。
func TestTranscribeHTTPErrorPlainTextBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream connect error"))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "deepseek transcribe HTTP 502: upstream connect error"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TestTranscribeOversizeBoundary 文档上限是「最大 8 MiB」，等号放行、多一字节拦截。
func TestTranscribeOversizeBoundary(t *testing.T) {
	if err := oversizeError(maxAudioBytes); err != nil {
		t.Fatalf("limit itself must pass: %v", err)
	}
	err := oversizeError(maxAudioBytes + 1)
	if err == nil {
		t.Fatal("expected size error")
	}
	if !strings.Contains(err.Error(), "8 MiB") && !strings.Contains(err.Error(), "8.0 MB") {
		t.Fatalf("err = %v", err)
	}
}

// TestTranscribeOversizeLocalBlock 超过 8 MiB 本地拦截，server 一次都不命中。
func TestTranscribeOversizeLocalBlock(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"text":"should not reach"}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: make([]byte, maxAudioBytes+1)})
	if err == nil {
		t.Fatal("expected size error")
	}
	if !strings.Contains(err.Error(), "8 MiB") || !strings.Contains(err.Error(), "未发送请求") {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

// TestTranscribeDurationLocalBlock 超过 120 秒本地拦截，server 一次都不命中。
// 用真实 WAV 头 + 121 秒 16 kHz 单声道 16-bit 音频（约 3.9 MB，仍在大小上限内）。
func TestTranscribeDurationLocalBlock(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"text":"should not reach"}`))
	}))
	defer srv.Close()

	audio := speech.EncodeWAVWithSampleRate(make([]byte, 121*speech.PCMByteRate), speech.PCMRate)
	if len(audio) > maxAudioBytes {
		t.Fatalf("test audio is %d bytes, bigger than the size limit; raise the limit assumption", len(audio))
	}

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: audio, ContentType: speech.PCMContentType})
	if err == nil {
		t.Fatal("expected duration error")
	}
	if !strings.Contains(err.Error(), "121.0 秒") || !strings.Contains(err.Error(), "120 秒") ||
		!strings.Contains(err.Error(), "未发送请求") {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

// TestTranscribeDurationBoundaryPasses 正好 120 秒（文档「最长 120 秒」）必须放行。
func TestTranscribeDurationBoundaryPasses(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"request_id":"r","text":"ok"}`))
	}))
	defer srv.Close()

	audio := speech.EncodeWAVWithSampleRate(make([]byte, 120*speech.PCMByteRate), speech.PCMRate)
	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Transcribe(context.Background(), speech.TranscribeRequest{Audio: audio, ContentType: speech.PCMContentType})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "ok" {
		t.Fatalf("text = %q", res.Text)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hit %d times, want 1", got)
	}
}

// TestTranscribeNonWAVSkipsDurationCheck 非 WAV（Ogg Opus）解析不出时长，
// 不能拿「假定 16k mono」硬除误拦，直接发给接口。
func TestTranscribeNonWAVSkipsDurationCheck(t *testing.T) {
	var hits int32
	var gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotFilename = hdr.Filename
		_, _ = w.Write([]byte(`{"request_id":"r","text":"ok"}`))
	}))
	defer srv.Close()

	// 5 MB 的假 Ogg：若按 16 kHz 单声道硬除会被当成约 156 秒误拦。
	audio := append([]byte("OggS\x00\x02"), make([]byte, 5<<20)...)
	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio: audio, ContentType: "audio/ogg",
	}); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hit %d times, want 1", got)
	}
	if gotFilename != "recording.opus" {
		t.Fatalf("filename = %q", gotFilename)
	}
}

// TestEngineDeclaresAudioLimit deepseek 必须声明 120 秒上限，录制侧与
// BatchSession 都依赖这个值提前收手。
func TestEngineDeclaresAudioLimit(t *testing.T) {
	e, err := NewFromMap(map[string]string{"api_key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	lim, ok := e.(speech.AudioLimiter)
	if !ok {
		t.Fatal("deepseek 引擎应实现 speech.AudioLimiter")
	}
	if got := lim.MaxAudioDurationMs(); got != 120000 {
		t.Fatalf("MaxAudioDurationMs = %d, want 120000", got)
	}
	if got := speech.MaxAudioDurationMs(e); got != 120000 {
		t.Fatalf("辅助函数读到 %d, want 120000", got)
	}
}

// TestTranscribeUsesRequestDuration 调用方给的 DurationMs 是权威时长，
// 优先于对容器头的解析：WAV 头声明 10 秒、DurationMs 报 121 秒时必须拦截。
func TestTranscribeUsesRequestDuration(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"text":"should not reach"}`))
	}))
	defer srv.Close()

	// 头里只有 10 秒音频，但调用方明确说是 121 秒：以调用方为准。
	audio := speech.EncodeWAVWithSampleRate(make([]byte, 10*speech.PCMByteRate), speech.PCMRate)
	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio:       audio,
		ContentType: speech.PCMContentType,
		DurationMs:  121000,
	})
	if err == nil {
		t.Fatal("expected duration error from DurationMs")
	}
	if !strings.Contains(err.Error(), "121.0 秒") || !strings.Contains(err.Error(), "未发送请求") {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

// TestTranscribeDurationMsCoversNonWAV 有了 DurationMs，非 WAV 输入也能做时长
// 限制——这正是它比解析容器头强的地方。
func TestTranscribeDurationMsCoversNonWAV(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"text":"should not reach"}`))
	}))
	defer srv.Close()

	audio := append([]byte("OggS\x00\x02"), make([]byte, 1<<20)...)
	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio:       audio,
		ContentType: "audio/ogg",
		DurationMs:  180000,
	})
	if err == nil {
		t.Fatal("expected duration error for non-WAV with DurationMs")
	}
	if !strings.Contains(err.Error(), "180.0 秒") {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

// TestTranscribeDurationMsBoundaryPasses DurationMs 正好 120000 放行。
func TestTranscribeDurationMsBoundaryPasses(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"request_id":"r","text":"ok"}`))
	}))
	defer srv.Close()

	audio := append([]byte("OggS\x00\x02"), make([]byte, 1<<20)...)
	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio:       audio,
		ContentType: "audio/ogg",
		DurationMs:  120000,
	}); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hit %d times, want 1", got)
	}
}

// TestTranscribeEmptyAudio 空音频不发请求，直接返回 ErrInvalidInput。
func TestTranscribeEmptyAudio(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"text":"should not reach"}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), speech.TranscribeRequest{}); !errors.Is(err, speech.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

// ---- WAV 头解析 ----

// wavChunk 测试用 chunk：id（4 字节）+ payload。
type wavChunk struct {
	id      string
	payload []byte
}

// buildWAV 手工拼一个 RIFF/WAVE 文件。chunks 顺序由调用方决定（可把 LIST 放在 fmt 前面），
// dataSizeField 可被覆盖成 0 / 0xFFFFFFFF，用来测「未回填 length」的回退；
// dataLen 是 data chunk 实际带的音频字节数。
func buildWAV(chunks []wavChunk, dataSizeField uint32, dataLen int) []byte {
	var body bytes.Buffer
	body.WriteString("WAVE")
	for _, c := range chunks {
		body.WriteString(c.id)
		var sz [4]byte
		binary.LittleEndian.PutUint32(sz[:], uint32(len(c.payload)))
		body.Write(sz[:])
		body.Write(c.payload)
		if len(c.payload)%2 == 1 {
			body.WriteByte(0) // chunk 奇数长度后的填充字节
		}
	}
	body.WriteString("data")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], dataSizeField)
	body.Write(sz[:])
	body.Write(make([]byte, dataLen))

	var out bytes.Buffer
	out.WriteString("RIFF")
	var riff [4]byte
	binary.LittleEndian.PutUint32(riff[:], uint32(body.Len()))
	out.Write(riff[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

// fmtPayload 构造 16 字节 PCM fmt 块。
func fmtPayload(sampleRate uint32, channels, bits uint16, byteRate uint32, blockAlign uint16) []byte {
	p := make([]byte, 16)
	binary.LittleEndian.PutUint16(p[0:2], 1) // PCM
	binary.LittleEndian.PutUint16(p[2:4], channels)
	binary.LittleEndian.PutUint32(p[4:8], sampleRate)
	binary.LittleEndian.PutUint32(p[8:12], byteRate)
	binary.LittleEndian.PutUint16(p[12:14], blockAlign)
	binary.LittleEndian.PutUint16(p[14:16], bits)
	return p
}

func TestWavDurationSeconds(t *testing.T) {
	fmt16k := fmtPayload(16000, 1, 16, 32000, 2)

	t.Run("标准 44 字节头", func(t *testing.T) {
		audio := speech.EncodeWAVWithSampleRate(make([]byte, 3*speech.PCMByteRate), speech.PCMRate)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 2.99 || got > 3.01 {
			t.Fatalf("duration = %f, want ~3", got)
		}
	})

	t.Run("fmt 前面带奇数长度的 LIST 块", func(t *testing.T) {
		// LIST/INFO 很常见；payload 长度 5 是奇数，下一块起点要跳过 1 字节填充。
		audio := buildWAV([]wavChunk{
			{id: "LIST", payload: []byte("ab\x00\x00c")},
			{id: "fmt ", payload: fmt16k},
		}, uint32(2*speech.PCMByteRate), 2*speech.PCMByteRate)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 1.99 || got > 2.01 {
			t.Fatalf("duration = %f, want ~2", got)
		}
	})

	t.Run("data size 为 0 时回退总长度", func(t *testing.T) {
		audio := buildWAV([]wavChunk{
			{id: "fmt ", payload: fmt16k},
		}, 0, 2*speech.PCMByteRate)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 1.99 || got > 2.01 {
			t.Fatalf("duration = %f, want ~2 (不能直接判 0 秒)", got)
		}
	})

	t.Run("data size 为 0xFFFFFFFF 时回退总长度", func(t *testing.T) {
		audio := buildWAV([]wavChunk{
			{id: "fmt ", payload: fmt16k},
		}, 0xFFFFFFFF, 2*speech.PCMByteRate)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 1.99 || got > 2.01 {
			t.Fatalf("duration = %f, want ~2", got)
		}
	})

	t.Run("data size 超过实际字节数时按实算", func(t *testing.T) {
		audio := buildWAV([]wavChunk{
			{id: "fmt ", payload: fmt16k},
		}, uint32(10*speech.PCMByteRate), 2*speech.PCMByteRate)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 1.99 || got > 2.01 {
			t.Fatalf("duration = %f, want ~2", got)
		}
	})

	t.Run("byteRate 为 0 时用采样率×声道×位深折算", func(t *testing.T) {
		// 48 kHz 双声道 16-bit，byteRate 故意写 0。
		audio := buildWAV([]wavChunk{
			{id: "fmt ", payload: fmtPayload(48000, 2, 16, 0, 4)},
		}, uint32(48000*4*2), 48000*4*2)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 1.99 || got > 2.01 {
			t.Fatalf("duration = %f, want ~2", got)
		}
	})

	t.Run("byteRate 与声道位深都缺失时用 blockAlign", func(t *testing.T) {
		audio := buildWAV([]wavChunk{
			{id: "fmt ", payload: fmtPayload(8000, 0, 0, 0, 4)},
		}, uint32(8000*4), 8000*4)
		got, ok := wavDurationSeconds(audio)
		if !ok {
			t.Fatal("ok = false")
		}
		if got < 0.99 || got > 1.01 {
			t.Fatalf("duration = %f, want ~1", got)
		}
	})

	t.Run("非 WAV 返回 false", func(t *testing.T) {
		if _, ok := wavDurationSeconds([]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00")); ok {
			t.Fatal("Ogg 不该被当成 WAV")
		}
		if _, ok := wavDurationSeconds([]byte("RIFF")); ok {
			t.Fatal("短头不该被当成 WAV")
		}
		if _, ok := wavDurationSeconds([]byte("RIFF\x00\x00\x00\x00AVI ")); ok {
			t.Fatal("非 WAVE 的 RIFF 不该被当成 WAV")
		}
	})

	t.Run("缺 fmt 或 data 返回 false", func(t *testing.T) {
		audio := buildWAV([]wavChunk{
			{id: "LIST", payload: []byte("INFOxxxx")},
		}, 0, 1024)
		if _, ok := wavDurationSeconds(audio); ok {
			t.Fatal("没有 fmt 不该给出时长")
		}
	})
}
