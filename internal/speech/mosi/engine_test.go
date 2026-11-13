// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package mosi

import (
	"context"
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
	if c.BaseURL != "https://api.mosi.cn/v1" {
		t.Fatalf("base = %s", c.BaseURL)
	}
	if c.Model != "moss-transcribe-1.0" {
		t.Fatalf("model = %s", c.Model)
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

func TestParseConfigCustomEndpoint(t *testing.T) {
	c, err := ParseConfig(map[string]string{
		"api_key":  "k",
		"base_url": "http://127.0.0.1:8000/v1/",
		"model":    "moss-transcribe-diarize-pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://127.0.0.1:8000/v1" {
		t.Fatalf("base = %s", c.BaseURL)
	}
	if c.Model != "moss-transcribe-diarize-pro" {
		t.Fatalf("model = %s", c.Model)
	}
}

// TestParseConfigIgnoresLanguage 接口没有 language 参数；配置里带 language 不影响构建。
func TestParseConfigIgnoresLanguage(t *testing.T) {
	c, err := ParseConfig(map[string]string{"api_key": "k", "language": "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != defaultModel {
		t.Fatalf("model = %s", c.Model)
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
	if !strings.Contains(detail, "moss-transcribe-1.0") || !strings.Contains(detail, "api.mosi.cn") {
		t.Fatalf("detail = %q", detail)
	}
	if e.Name() != "mosi" {
		t.Fatalf("name = %s", e.Name())
	}
	if _, err := e.Speak(context.Background(), speech.SpeakRequest{Text: "hi"}); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("Speak err = %v", err)
	}
	if _, err := e.ListVoices(context.Background()); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("ListVoices err = %v", err)
	}
}

// TestTranscribeRequestShape 成功路径：只发 file + model，Bearer 鉴权，解析 text。
func TestTranscribeRequestShape(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	var gotFields []string
	var gotModel, gotFilename, gotAudio, gotResponseFormat string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for name := range r.MultipartForm.Value {
			gotFields = append(gotFields, name)
		}
		gotModel = r.FormValue("model")
		gotResponseFormat = r.FormValue("response_format")
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(traceIDHeader, "trace-ok")
		_, _ = w.Write([]byte(`{"text":"  你好，世界  "}`))
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
	if res.Text != "你好，世界" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Engine != "mosi" {
		t.Fatalf("engine = %q", res.Engine)
	}
	// 成功路径的追踪号随结果上报，插件会把它写进识别历史。
	if res.UpstreamLogID != "trace-ok" {
		t.Fatalf("upstream log id = %q, want %q", res.UpstreamLogID, "trace-ok")
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotModel != "moss-transcribe-1.0" {
		t.Fatalf("model field = %q", gotModel)
	}
	if gotFilename != "recording.wav" {
		t.Fatalf("filename = %q", gotFilename)
	}
	if gotAudio != "fake-wav" {
		t.Fatalf("audio = %q", gotAudio)
	}
	// 表单只允许 file + model：response_format 默认就是 json，不发；
	// language / async 等未验证的字段也不能出现。
	if len(gotFields) != 1 || gotFields[0] != "model" {
		t.Fatalf("form fields = %v, want only [model]", gotFields)
	}
	if gotResponseFormat != "" {
		t.Fatalf("response_format must not be sent, got %q", gotResponseFormat)
	}
}

// TestTranscribePlainTextResponse response_format=text 时接口返回裸文本
// （文档：接口返回 text/plain，响应体就是转写文本，不是 JSON 对象）。
func TestTranscribePlainTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("  这是一段纯文本转写结果\n"))
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
	if res.Text != "这是一段纯文本转写结果" {
		t.Fatalf("text = %q", res.Text)
	}
}

// TestTranscribePlainTextWithoutContentType 有的网关会把 text 响应的 Content-Type
// 写成 application/octet-stream 或干脆不带；JSON 解析失败也要按纯文本处理。
func TestTranscribePlainTextWithoutContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("裸文本，没有 json"))
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
	if res.Text != "裸文本，没有 json" {
		t.Fatalf("text = %q", res.Text)
	}
}

// TestTranscribeMp3Filename ContentType 决定上传文件名后缀。
func TestTranscribeMp3Filename(t *testing.T) {
	var gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotFilename = hdr.Filename
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	e, err := NewFromMap(map[string]string{"api_key": "k", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio:       []byte("fake-mp3"),
		ContentType: "audio/mpeg",
	}); err != nil {
		t.Fatal(err)
	}
	if gotFilename != "recording.mp3" {
		t.Fatalf("filename = %q", gotFilename)
	}
}

// TestTranscribeHTTPErrorUsesNestedError 实测（假 key）响应：
// HTTP 403，Content-Type: application/json; charset=utf-8，
// 响应头 x-trace-id: 78e6…，
// body: {"error":{"code":"102111001","type":"permission_error","message":"iam api key auth failed","legacy_code":4030},"trace_id":"78e6…"}
// 说明嵌在 error 对象里（不是顶层 message），code 是字符串；错误路径同样要带 trace。
func TestTranscribeHTTPErrorUsesNestedError(t *testing.T) {
	const trace = "78e6a2ff70b1625e9a3a9756bdb80bae"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(traceIDHeader, trace)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"102111001","type":"permission_error","message":"iam api key auth failed","legacy_code":4030},"trace_id":"` + trace + `"}`))
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
	want := "mosi transcribe HTTP 403: iam api key auth failed (code=102111001; trace=" + trace + ")"
	if msg := err.Error(); msg != want {
		t.Fatalf("error = %q, want %q", msg, want)
	}
}

// TestTranscribeHTTPErrorTraceFromBody 响应头没有 trace 时，回退用错误体顶层的 trace_id。
func TestTranscribeHTTPErrorTraceFromBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limit_exceeded","type":"rate_limit_error","message":"too many requests"},"trace_id":"trace-body"}`))
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
	want := "mosi transcribe HTTP 429: too many requests (code=rate_limit_exceeded; trace=trace-body)"
	if msg := err.Error(); msg != want {
		t.Fatalf("error = %q, want %q", msg, want)
	}
}

// TestTranscribeHTTPErrorWithoutTrace 响应头与响应体都没有 trace 时，错误串保持原样。
func TestTranscribeHTTPErrorWithoutTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"service_unavailable","type":"service","message":"服务不可用"}}`))
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
	want := "mosi transcribe HTTP 503: 服务不可用 (code=service_unavailable)"
	if msg := err.Error(); msg != want {
		t.Fatalf("error = %q, want %q", msg, want)
	}
}

// TestTranscribeHTTPErrorPlainTextBody 非 JSON 错误体（网关 502 页面等）回退原始内容。
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
	want := "mosi transcribe HTTP 502: upstream connect error"
	if msg := err.Error(); msg != want {
		t.Fatalf("error = %q, want %q", msg, want)
	}
}

// TestTranscribeOversizeBoundary 超过接口上限（512MB）本地拦截，不发送请求。
// 直接用 oversizeError 测边界，不真的分配 512MB。
func TestTranscribeOversizeBoundary(t *testing.T) {
	if err := oversizeError(maxAudioBytes); err != nil {
		t.Fatalf("limit itself must pass: %v", err)
	}
	err := oversizeError(maxAudioBytes + 1)
	if err == nil {
		t.Fatal("expected size error")
	}
	if !strings.Contains(err.Error(), "512 MB") {
		t.Fatalf("err = %v", err)
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

// TestTranscribeBodyTraceFallbackOnSuccess 成功响应体里带 trace_id 而响应头没有时，
// 也要把它填进 UpstreamLogID，别把能拿到的排障号丢了。
func TestTranscribeBodyTraceFallbackOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok","trace_id":"body-trace"}`))
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
	if res.UpstreamLogID != "body-trace" {
		t.Fatalf("upstream log id = %q, want body-trace", res.UpstreamLogID)
	}
}
