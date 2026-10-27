// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

// Package siliconflow 实现硅基流动（SiliconFlow）语音转写引擎（批处理）：
// POST {base_url}/audio/transcriptions，multipart/form-data + Bearer 鉴权。
// 文档：https://docs.siliconflow.cn/docs/api/audio-transcriptions-post
//
// 表单只发 file 与 model：接口文档没有 response_format / language 等字段，
// 全局 [voice].language 会被 asrcfg 注入配置 map，这里读取后忽略、不转发。
//
// 仅支持批处理（speech.Engine），录音场景由 BatchSession 包装成流式会话。
package siliconflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

const (
	// defaultBaseURL 硅基流动 API 根地址。
	defaultBaseURL = "https://api.siliconflow.cn/v1"
	// defaultModel 默认转写模型；可改配置换 TeleAI/TeleSpeechASR 等。
	defaultModel = "FunAudioLLM/SenseVoiceSmall"
	// maxAudioBytes 接口限制单文件 ≤ 50MB（≤1 小时音频）；超限本地拦截，不白跑一次往返。
	maxAudioBytes = 50 << 20
)

func init() {
	speech.Register("siliconflow", NewFromMap)
}

// 配置键（[voice.engine_configs.siliconflow]）：
//
//	api_key   必填，账户 API Key（Authorization: Bearer）
//	base_url  默认 https://api.siliconflow.cn/v1
//	model     默认 FunAudioLLM/SenseVoiceSmall

type Config struct {
	APIKey  string
	BaseURL string
	Model   string
}

func ParseConfig(m map[string]string) (Config, error) {
	if m == nil {
		m = map[string]string{}
	}
	c := Config{
		APIKey:  strings.TrimSpace(m["api_key"]),
		BaseURL: strings.TrimRight(strings.TrimSpace(m["base_url"]), "/"),
		Model:   strings.TrimSpace(m["model"]),
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.Model == "" {
		c.Model = defaultModel
	}
	if c.APIKey == "" {
		return Config{}, fmt.Errorf("siliconflow: 需要 api_key")
	}
	return c, nil
}

// Engine 实现 speech.Engine（仅 STT，批处理）。
type Engine struct {
	cfg    Config
	client *http.Client
}

// NewFromMap 工厂：map 配置 → Engine。
func NewFromMap(m map[string]string) (speech.Engine, error) {
	cfg, err := ParseConfig(m)
	if err != nil {
		return nil, err
	}
	return &Engine{
		cfg: cfg,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

func (e *Engine) Name() string {
	return "siliconflow"
}

func (e *Engine) Capabilities() speech.Capabilities {
	return speech.Capabilities{STT: true}
}

func (e *Engine) Available(context.Context) (bool, string) {
	if e.cfg.APIKey == "" {
		return false, "未配置 api_key"
	}
	return true, fmt.Sprintf("base=%s model=%s", e.cfg.BaseURL, e.cfg.Model)
}

func (e *Engine) Speak(context.Context, speech.SpeakRequest) (*speech.SpeakResult, error) {
	return nil, speech.ErrUnsupported
}

func (e *Engine) ListVoices(context.Context) ([]speech.Voice, error) {
	return nil, speech.ErrUnsupported
}

func (e *Engine) Transcribe(ctx context.Context, req speech.TranscribeRequest) (*speech.TranscribeResult, error) {
	if len(req.Audio) == 0 {
		return nil, speech.ErrInvalidInput
	}
	if len(req.Audio) > maxAudioBytes {
		return nil, fmt.Errorf("siliconflow: 音频 %.1f MB 超过接口上限 50 MB（1 小时），未发送请求",
			float64(len(req.Audio))/(1<<20))
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filenameFromContentType(req.ContentType))
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(req.Audio); err != nil {
		return nil, err
	}
	// 只写文档列出的字段：多写 response_format / language 可能被端点拒绝。
	_ = mw.WriteField("model", e.cfg.Model)
	if err := mw.Close(); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.BaseURL+"/audio/transcriptions", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())
	httpReq.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("siliconflow transcribe: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("siliconflow transcribe %s", formatHTTPError(resp.StatusCode, data))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("siliconflow transcribe json: %w", err)
	}
	return &speech.TranscribeResult{
		Text:   strings.TrimSpace(out.Text),
		Lang:   strings.TrimSpace(req.Lang),
		Engine: e.Name(),
	}, nil
}

// errBody 错误响应体：实测为 {"code":30014,"data":null,"message":"Token is invalid."}。
type errBody struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

// formatHTTPError 拼可读错误：优先取响应 JSON 的 message（附 code），
// 解析不出（例如网关返回纯文本 / HTML）时回退原始 body 截断。
func formatHTTPError(statusCode int, data []byte) string {
	var e errBody
	if err := json.Unmarshal(data, &e); err == nil {
		msg := strings.TrimSpace(e.Message)
		code := codeString(e.Code)
		switch {
		case msg != "" && code != "":
			return fmt.Sprintf("HTTP %d: %s (code=%s)", statusCode, msg, code)
		case msg != "":
			return fmt.Sprintf("HTTP %d: %s", statusCode, msg)
		}
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return fmt.Sprintf("HTTP %d: 空响应", statusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", statusCode, truncate(raw, 300))
}

// codeString 兼容 code 为数字或字符串两种形态。
func codeString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func filenameFromContentType(ct string) string {
	ct = strings.ToLower(ct)
	if strings.Contains(ct, "mp3") || strings.Contains(ct, "mpeg") {
		return "recording.mp3"
	}
	return "recording.wav"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
