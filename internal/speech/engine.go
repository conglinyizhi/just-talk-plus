// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

// Package speech 提供语音引擎抽象层（TTS / STT / 流式 STT），供接入具体厂商实现。
//
// 设计对齐 go-human-handler 的 backend/internal/speech：
//   - 核心是 Engine 接口；业务只依赖接口，不依赖具体供应商
//   - 默认 NullEngine：未配置时 Available=false，调用返回 ErrNotConfigured
//   - 新增引擎 = Register(type, Factory)，不要改调用方
//   - 流式识别通过可选的 StreamingEngine 扩展；批处理引擎由 BatchSession 包装成流式会话
package speech

import (
	"context"
	"errors"
)

// 常见错误：调用方可 errors.Is 判断。
var (
	// ErrNotConfigured 未启用/未配置引擎（Null 或工厂未装）。
	ErrNotConfigured = errors.New("speech: engine not configured")
	// ErrUnsupported 当前引擎不支持该能力（例如仅 STT 却调 TTS）。
	ErrUnsupported = errors.New("speech: operation not supported")
	// ErrInvalidInput 请求缺文本/音频等。
	ErrInvalidInput = errors.New("speech: invalid input")
)

// Capabilities 描述引擎能力，供设置页/前端决定是否展示麦克风、朗读等入口。
type Capabilities struct {
	TTS          bool `json:"tts"`
	STT          bool `json:"stt"`
	StreamingTTS bool `json:"streaming_tts,omitempty"`
	StreamingSTT bool `json:"streaming_stt,omitempty"`
	// Voices 是否支持列举音色
	Voices bool `json:"voices,omitempty"`
}

// SpeakRequest 文本转语音入参。空 Format/Voice 由引擎选默认。
type SpeakRequest struct {
	Text   string
	Voice  string  // 引擎侧音色 ID
	Lang   string  // BCP-47，如 zh-CN
	Format string  // wav | mp3 | opus | pcm；空 = 引擎默认
	Speed  float64 // 1.0 正常；0 表示默认
}

// SpeakResult 合成结果。Audio 为完整字节；流式引擎未来可另开接口。
type SpeakResult struct {
	Audio       []byte
	ContentType string // 如 audio/wav
	DurationMs  int    // 未知则为 0
	Engine      string
	Voice       string
}

// TranscribeRequest 语音转文本入参。
type TranscribeRequest struct {
	Audio       []byte
	ContentType string // 如 audio/wav；以 url: 前缀约定公网 URL 时引擎可走文件 API
	Lang        string // 空 = 自动/引擎默认
	// DurationMs 音频时长（毫秒），0 = 未知。
	//
	// 由录制侧直接给出，而不是让引擎从容器格式反推：Buffer 的 PCM 长度与
	// 采样率都是已知量，算出来精确到字节。引擎应当优先用它做时长限制判断，
	// 容器解析只作为兜底（例如调用方直接塞了一段外部音频、没有 DurationMs）。
	DurationMs int
}

// TranscribeResult 识别结果。
type TranscribeResult struct {
	Text       string
	Lang       string
	Confidence float64 // 0–1；未知为 0
	Engine     string
	// UpstreamLogID 上游服务返回的排障追踪 ID（如豆包 X-Tt-Logid、
	// 硅基流动 x-siliconcloud-trace-id）；未知为空字符串。
	UpstreamLogID string
}

// Voice 可选音色条目。
type Voice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Lang   string `json:"lang,omitempty"`
	Gender string `json:"gender,omitempty"`
}

// Engine 语音引擎。实现方应：
//   - 不在构造时强连外网；把探测放进 Available
//   - Speak/Transcribe 尊重 ctx 取消
//   - 不支持的操作返回 ErrUnsupported，而不是 panic
type Engine interface {
	// Name 稳定短名，如 null / doubao-stream / auralwise / openai
	Name() string
	Capabilities() Capabilities
	// Available 环境是否可用；detail 给人看（缺 key、无二进制等）
	Available(ctx context.Context) (ok bool, detail string)
	Speak(ctx context.Context, req SpeakRequest) (*SpeakResult, error)
	Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error)
	// ListVoices 不支持时返回 (nil, ErrUnsupported)
	ListVoices(ctx context.Context) ([]Voice, error)
}

// Status 供状态探测展示。
type Status struct {
	Engine       string       `json:"engine"`
	Available    bool         `json:"available"`
	Detail       string       `json:"detail,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

// Probe 汇总引擎状态。
func Probe(ctx context.Context, e Engine) Status {
	if e == nil {
		e = Null{}
	}
	ok, detail := e.Available(ctx)
	return Status{
		Engine:       e.Name(),
		Available:    ok,
		Detail:       detail,
		Capabilities: e.Capabilities(),
	}
}
