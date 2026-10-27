// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package siliconflow

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
	if c.BaseURL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("base = %s", c.BaseURL)
	}
	if c.Model != "FunAudioLLM/SenseVoiceSmall" {
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
		"model":    "TeleAI/TeleSpeechASR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://127.0.0.1:8000/v1" {
		t.Fatalf("base = %s", c.BaseURL)
	}
	if c.Model != "TeleAI/TeleSpeechASR" {
		t.Fatalf("model = %s", c.Model)
	}
}

// TestParseConfigIgnoresGlobalLanguage asrcfg 会把共享 language 注入配置 map，接口不接受该字段。
func TestParseConfigIgnoresGlobalLanguage(t *testing.T) {
	c, err := ParseConfig(map[string]string{"api_key": "k", "language": "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "FunAudioLLM/SenseVoiceSmall" {
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
	if !strings.Contains(detail, "FunAudioLLM/SenseVoiceSmall") || !strings.Contains(detail, "api.siliconflow.cn") {
		t.Fatalf("detail = %q", detail)
	}
	if e.Name() != "siliconflow" {
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
	var gotModel, gotFilename, gotUsage string

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
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		gotFilename = hdr.Filename
		raw, _ := io.ReadAll(f)
		gotUsage = string(raw)
		w.Header().Set("Content-Type", "application/json")
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
	if res.Engine != "siliconflow" {
		t.Fatalf("engine = %q", res.Engine)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotModel != "FunAudioLLM/SenseVoiceSmall" {
		t.Fatalf("model field = %q", gotModel)
	}
	if gotFilename != "recording.wav" {
		t.Fatalf("filename = %q", gotFilename)
	}
	if gotUsage != "fake-wav" {
		t.Fatalf("audio = %q", gotUsage)
	}
	// 表单只允许 file + model：language / response_format 等未列入文档的字段不能出现
	if len(gotFields) != 1 || gotFields[0] != "model" {
		t.Fatalf("form fields = %v, want only [model]", gotFields)
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

// TestTranscribeHTTPErrorUsesMessage 实测错误体：{"code":30014,"data":null,"message":"Token is invalid."}
func TestTranscribeHTTPErrorUsesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":30014,"data":null,"message":"Token is invalid."}`))
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
	msg := err.Error()
	want := "siliconflow transcribe HTTP 401: Token is invalid. (code=30014)"
	if msg != want {
		t.Fatalf("error = %q, want %q", msg, want)
	}
}

// TestTranscribeHTTPErrorFallsBackToBody 非 JSON 错误体（网关 502 页面等）回退原始内容。
func TestTranscribeHTTPErrorFallsBackToBody(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "upstream connect error") ||
		!strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err = %v", err)
	}
}

// TestTranscribeRejectsOversizeLocally 超过 50MB 本地拦截，不发请求。
func TestTranscribeRejectsOversizeLocally(t *testing.T) {
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
	_, err = e.Transcribe(context.Background(), speech.TranscribeRequest{
		Audio: make([]byte, maxAudioBytes+1),
	})
	if err == nil {
		t.Fatal("expected size error")
	}
	if !strings.Contains(err.Error(), "50 MB") {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("server hit %d times, want 0", got)
	}
}

func TestTranscribeEmptyAudio(t *testing.T) {
	e, err := NewFromMap(map[string]string{"api_key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), speech.TranscribeRequest{}); !errors.Is(err, speech.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
}
