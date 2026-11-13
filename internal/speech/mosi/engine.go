// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

// Package mosi 实现 Mosi（api.mosi.cn）音频转写引擎（批处理，同步返回）：
// POST {base_url}/audio/transcriptions，multipart/form-data + Bearer 鉴权。
// 文档：https://platform.mosi.cn/docs/reference/transcriptions
//
// 表单只发 file 与 model：
//   - response_format 不显式发送：接口默认就是 json，少发一个字段少一个出错面
//   - 接口没有 language 参数，语言不转发
//   - 接口不接受内联音频（audio_data / Base64），输入源只能是 file / file_id / url / audio_url；
//     本引擎只用 multipart file，file_id 与 url 形态本轮不实现
//   - 异步任务（async=true + GET /v1/audio/tasks/{task_id}）本轮不实现，只做同步调用
//
// 仅支持批处理（speech.Engine），录音场景由 BatchSession 包装成流式会话。
package mosi

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
	// defaultBaseURL Mosi API 根地址。
	defaultBaseURL = "https://api.mosi.cn/v1"
	// defaultModel 默认转写模型（普通音频转写的稳定 model ID）。
	defaultModel = "moss-transcribe-1.0"
	// maxAudioBytes 接口限制单文件 ≤ 512MB（文档「支持格式与上传限制」）。
	maxAudioBytes = 512 << 20
	// traceIDHeader 上游排障追踪号的响应头；实测假 key 的 403 响应里同样带该头。
	traceIDHeader = "x-trace-id"
)

func init() {
	speech.Register("mosi", NewFromMap)
}

// 配置键（[voice.engine_configs.mosi]）：
//
//	api_key   必填，API Key（Authorization: Bearer；文档里叫 MOSS_API_KEY）
//	base_url  默认 https://api.mosi.cn/v1
//	model     默认 moss-transcribe-1.0

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
		return Config{}, fmt.Errorf("mosi: 需要 api_key")
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
	return "mosi"
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
	if err := oversizeError(len(req.Audio)); err != nil {
		return nil, err
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
	// 只写文档列出的必填项：response_format 默认就是 json，language 接口根本没有。
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
		return nil, fmt.Errorf("mosi transcribe: %w", err)
	}
	defer resp.Body.Close()
	// 两种路径都要用：成功时随结果进识别历史，失败时拼进错误串给用户报障。
	traceID := strings.TrimSpace(resp.Header.Get(traceIDHeader))
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mosi transcribe %s", formatHTTPError(resp.StatusCode, data, traceID))
	}
	text, bodyTrace := parseSuccessBody(resp.Header.Get("Content-Type"), data)
	if traceID == "" {
		// 响应头拿不到时回退响应体的顶层 trace_id（错误体实测带该字段）。
		traceID = bodyTrace
	}
	return &speech.TranscribeResult{
		Text:          text,
		Lang:          strings.TrimSpace(req.Lang),
		Engine:        e.Name(),
		UpstreamLogID: traceID,
	}, nil
}

// parseSuccessBody 解析成功响应，返回文本与（可能存在的）顶层 trace_id。
//
// 接口在 response_format=text 时直接返回裸文本（Content-Type: text/plain），
// 所以两种形态都要认：Content-Type 不含 json，或 JSON 解析失败，一律按纯文本处理。
func parseSuccessBody(contentType string, data []byte) (text, traceID string) {
	if strings.Contains(strings.ToLower(contentType), "json") {
		var out struct {
			Text    string `json:"text"`
			TraceID string `json:"trace_id"`
		}
		if err := json.Unmarshal(data, &out); err == nil {
			return strings.TrimSpace(out.Text), strings.TrimSpace(out.TraceID)
		}
	}
	return strings.TrimSpace(string(data)), ""
}

// errBody 错误响应体。实测（假 key，HTTP 403，Content-Type: application/json; charset=utf-8）：
//
//	{"error":{"code":"102111001","type":"permission_error","message":"iam api key auth failed","legacy_code":4030},
//	 "trace_id":"0d01966ff90280c749b16674acd60775"}
//
// 错误说明嵌在 error 对象里、不在顶层，所以按 error.message / error.code 取；
// 顶层 message 留作网关层平铺结构的兜底。error 偶尔是字符串，两种都兼容。
type errBody struct {
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
	TraceID string          `json:"trace_id"`
}

// errDetail error 对象；code 实测是字符串（"102111001"），数字也认。
type errDetail struct {
	Code    any    `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// parseErrorBody 取错误说明、code 与响应体里的 trace_id。
func parseErrorBody(data []byte) (msg, code, trace string) {
	var body errBody
	if err := json.Unmarshal(data, &body); err != nil {
		return "", "", ""
	}
	trace = strings.TrimSpace(body.TraceID)
	if len(body.Error) > 0 {
		var d errDetail
		if err := json.Unmarshal(body.Error, &d); err == nil {
			if m := strings.TrimSpace(d.Message); m != "" {
				return m, codeString(d.Code), trace
			}
		}
		var s string
		if err := json.Unmarshal(body.Error, &s); err == nil {
			if s = strings.TrimSpace(s); s != "" {
				return s, "", trace
			}
		}
	}
	return strings.TrimSpace(body.Message), "", trace
}

// formatHTTPError 拼可读错误：优先取 error.message（附 error.code），
// 解析不出（网关返回纯文本 / HTML）时回退原始 body 截断。
// trace 优先用响应头，其次响应体的 trace_id；都取不到时输出不带 trace，便于用户直接找 Mosi 排障。
func formatHTTPError(statusCode int, data []byte, headerTrace string) string {
	msg, code, bodyTrace := parseErrorBody(data)
	trace := headerTrace
	if trace == "" {
		trace = bodyTrace
	}
	if msg != "" {
		return fmt.Sprintf("HTTP %d: %s%s", statusCode, msg, errorAttrs(code, trace))
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return fmt.Sprintf("HTTP %d: 空响应%s", statusCode, errorAttrs("", trace))
	}
	return fmt.Sprintf("HTTP %d: %s%s", statusCode, truncate(raw, 300), errorAttrs("", trace))
}

// errorAttrs 把 code / trace 拼成 `(code=...; trace=...)`；都没有时返回空串。
func errorAttrs(code, trace string) string {
	parts := make([]string, 0, 2)
	if code != "" {
		parts = append(parts, "code="+code)
	}
	if trace != "" {
		parts = append(parts, "trace="+trace)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
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

// oversizeError 本地拦截超限音频：512MB 是接口文档的上限，超过就不发请求，
// 没必要为一个必被拒的上传白跑一次往返。独立成函数便于不分配大内存地测边界。
func oversizeError(n int) error {
	if n <= maxAudioBytes {
		return nil
	}
	return fmt.Errorf("mosi: 音频 %.1f MB 超过接口上限 512 MB，未发送请求", float64(n)/(1<<20))
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
