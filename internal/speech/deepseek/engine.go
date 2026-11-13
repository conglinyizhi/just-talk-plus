// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

// Package deepseek 实现 DeepSeek 语音识别引擎（批处理，同步返回）：
// POST {base_url}/tools/asr，multipart/form-data + Bearer 鉴权。
// 文档：https://api-docs.deepseek.com/zh-cn/guides/asr/
//
// 表单只有一个 file 字段：接口没有 model、language、response_format 参数，
// 语种根据音频自动识别（文档列出 21 种语种，但没有入参可指定）。
//
// 接口限制 8 MiB / 120 秒，两项都在本地拦截：上传超限必被 413 拒掉，
// 超时长也只会白等一次往返，没必要让用户等一个注定失败的请求。
// 时长靠解析 WAV 头算真实秒数（不是「假定 16 kHz 单声道」硬除），
// 非 WAV（例如 Ogg Opus）解析失败时跳过时长检查，只靠大小上限兜底。
//
// 仅支持批处理（speech.Engine），录音场景由 BatchSession 包装成流式会话。
package deepseek

import (
	"bytes"
	"context"
	"encoding/binary"
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
	// defaultBaseURL DeepSeek API 根地址。
	defaultBaseURL = "https://api.deepseek.com/v1"
	// maxAudioBytes 接口限制单文件 ≤ 8 MiB；超过本地拦截，不白跑一次往返。
	maxAudioBytes = 8 << 20
	// maxAudioSeconds 接口限制单次音频 ≤ 120 秒；超过同样本地拦截。
	maxAudioSeconds = 120
	// traceIDHeader DeepSeek 的排障追踪号响应头；实测假 key 的 401 响应里同样带该头。
	traceIDHeader = "x-ds-trace-id"
)

func init() {
	speech.Register("deepseek", NewFromMap)
}

// 配置键（[voice.engine_configs.deepseek]）：
//
//	api_key   必填，DeepSeek API Key（Authorization: Bearer）
//	base_url  默认 https://api.deepseek.com/v1
//
// 没有 model 配置项：接口没有模型参数，语种也自动识别，多一个键只会让人以为设了有用。
type Config struct {
	APIKey  string
	BaseURL string
}

func ParseConfig(m map[string]string) (Config, error) {
	if m == nil {
		m = map[string]string{}
	}
	c := Config{
		APIKey:  strings.TrimSpace(m["api_key"]),
		BaseURL: strings.TrimRight(strings.TrimSpace(m["base_url"]), "/"),
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.APIKey == "" {
		return Config{}, fmt.Errorf("deepseek: 需要 api_key")
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
			// 音频本身最长 120 秒，识别加排队给 3 分钟余量；
			// 不按文档「上传需在 10 分钟内完成」的窗口等，否则录音收尾会长时间挂着。
			Timeout: 180 * time.Second,
		},
	}, nil
}

func (e *Engine) Name() string {
	return "deepseek"
}

func (e *Engine) Capabilities() speech.Capabilities {
	return speech.Capabilities{STT: true}
}

func (e *Engine) Available(context.Context) (bool, string) {
	if e.cfg.APIKey == "" {
		return false, "未配置 api_key"
	}
	// 没有模型可选，摘要里说明语种是自动识别的。
	return true, fmt.Sprintf("base=%s（无模型参数，语种自动识别）", e.cfg.BaseURL)
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
	// 先查大小（便宜），再查时长。
	if err := oversizeError(len(req.Audio)); err != nil {
		return nil, err
	}
	// 时长优先用调用方给的 DurationMs：录制侧直接知道 PCM 长度与采样率，
	// 算出来比反推容器可靠，对非 WAV 输入同样有效。
	//
	// 只有调用方没给时才退回去解析 WAV 头（例如外部直接调用引擎、塞了一段
	// 裸音频）；两者都拿不到就跳过时长检查——没有可靠来源时硬估会误伤能用的
	// 音频，交给接口自己判断，大小上限仍然生效。
	if req.DurationMs > 0 {
		if err := durationError(float64(req.DurationMs) / 1000); err != nil {
			return nil, err
		}
	} else if seconds, ok := wavDurationSeconds(req.Audio); ok {
		if err := durationError(seconds); err != nil {
			return nil, err
		}
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
	// 接口只有 file 一个入参，不再写 model / language / response_format。
	if err := mw.Close(); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.BaseURL+"/tools/asr", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())
	httpReq.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek transcribe: %w", err)
	}
	defer resp.Body.Close()
	// 两种路径都要用：成功时回退填进识别历史，失败时拼进错误串给用户报障。
	traceID := strings.TrimSpace(resp.Header.Get(traceIDHeader))
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deepseek transcribe %s", formatHTTPError(resp.StatusCode, data, traceID))
	}
	var out struct {
		RequestID string `json:"request_id"`
		Text      string `json:"text"`
		// DurationMS 上游返回的音频时长（毫秒）；TranscribeResult 目前没有对应字段，
		// 这里只按响应结构解析，没有消费方。
		DurationMS int `json:"duration_ms"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("deepseek transcribe json: %w", err)
	}
	// 追踪号优先用响应体的 request_id（实测成功响应必带），拿不到才回退响应头。
	upstreamLogID := strings.TrimSpace(out.RequestID)
	if upstreamLogID == "" {
		upstreamLogID = traceID
	}
	return &speech.TranscribeResult{
		Text:          strings.TrimSpace(out.Text),
		Lang:          strings.TrimSpace(req.Lang),
		Engine:        e.Name(),
		UpstreamLogID: upstreamLogID,
	}, nil
}

// errBody 错误响应体。实测（假 key 打真实端点，HTTP 401）为 OpenAI 风格的嵌套结构：
//
//	{"error":{"message":"Authentication Fails, Your api key: ****abcd is invalid (request_id: d06c0f4b-…)",
//	          "type":"authentication_error","param":null,"code":"invalid_request_error"}}
//
// 错误说明嵌在 error 对象里、不在顶层，所以按 error.message 取；
// 顶层 message 留作网关层平铺结构的兜底。error 偶尔是字符串，两种都兼容。
type errBody struct {
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
}

// errDetail error 对象；code 实测是字符串（invalid_request_error），数字也认。
type errDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// parseErrorBody 取错误说明与 code。错误对象只给了 type 时退回 type，
// 免得只显示一个 HTTP 状态码。
func parseErrorBody(data []byte) (msg, code string) {
	var body errBody
	if err := json.Unmarshal(data, &body); err != nil {
		return "", ""
	}
	if len(body.Error) > 0 {
		var d errDetail
		if err := json.Unmarshal(body.Error, &d); err == nil {
			code = codeString(d.Code)
			if m := strings.TrimSpace(d.Message); m != "" {
				return m, code
			}
			if t := strings.TrimSpace(d.Type); t != "" {
				return t, code
			}
		}
		var s string
		if err := json.Unmarshal(body.Error, &s); err == nil {
			if s = strings.TrimSpace(s); s != "" {
				return s, ""
			}
		}
	}
	return strings.TrimSpace(body.Message), ""
}

// formatHTTPError 拼可读错误：优先取嵌套的 error.message（附 error.code），
// 解析不出（网关返回纯文本 / HTML）时回退原始 body 截断。
// trace 非空时作为最后一个属性追加，便于用户直接拿去找 DeepSeek 排障；
// 取不到 trace 时输出不带 trace，保持最小。
func formatHTTPError(statusCode int, data []byte, trace string) string {
	msg, code := parseErrorBody(data)
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

// oversizeError 本地拦截超限音频：超过 8 MiB 的上传必被接口 413 拒掉，
// 不发请求。独立成函数便于不分配大内存地测边界。
func oversizeError(n int) error {
	if n <= maxAudioBytes {
		return nil
	}
	return fmt.Errorf("deepseek: 音频 %.1f MB，超过 DeepSeek ASR 单次上限 8 MiB（未发送请求）；"+
		"请缩短单次录音，或改用 doubao-stream / siliconflow 等引擎", float64(n)/(1<<20))
}

// durationError 本地拦截超时长音频。文案带上实测秒数与上限，用户才知道该切多短。
func durationError(seconds float64) error {
	if seconds <= maxAudioSeconds {
		return nil
	}
	return fmt.Errorf("deepseek: 音频约 %.1f 秒，超过 DeepSeek ASR 单次上限 %d 秒（未发送请求）；"+
		"请缩短单次录音，或改用 doubao-stream / siliconflow 等引擎", seconds, maxAudioSeconds)
}

// wavDurationSeconds 解析 WAV（RIFF/WAVE）头，按 data chunk 长度算真实时长（秒）。
// ok=false 表示这不是一个能解析的 WAV（例如 Ogg Opus），调用方据此跳过时长检查。
//
// 不能假设 44 字节头或 fmt 紧跟 RIFF，真实文件里有四个坑：
//   - chunk 必须遍历：从 offset 12 开始逐块读（4 字节 ID + 4 字节小端 size），
//     很多 WAV 在 fmt 前面带 LIST/INFO 等块
//   - chunk size 为奇数时后面跟 1 字节填充，下一块起点要 +1
//   - data chunk 的 size 可能是 0 或 0xFFFFFFFF（流式写入未回填），这时回退用
//     「总长度 − data 头偏移」估算，而不是直接判 0 秒
//   - 字节率优先用 fmt 里的 byteRate；该字段为 0 时用 采样率 × 声道数 × 位深/8 自己算
func wavDurationSeconds(data []byte) (float64, bool) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, false
	}
	var (
		haveFmt    bool
		haveData   bool
		sampleRate uint32
		channels   uint16
		bits       uint16
		blockAlign uint16
		byteRate   uint32
		dataSize   int64
	)
	total := int64(len(data))
loop:
	for off := int64(12); off+8 <= total; {
		id := string(data[off : off+4])
		size := int64(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		switch id {
		case "fmt ":
			// WAVEFORMATEX 至少 16 字节；不足的块按损坏处理，继续找下一个块。
			if size >= 16 && body+16 <= total {
				channels = binary.LittleEndian.Uint16(data[body+2 : body+4])
				sampleRate = binary.LittleEndian.Uint32(data[body+4 : body+8])
				byteRate = binary.LittleEndian.Uint32(data[body+8 : body+12])
				blockAlign = binary.LittleEndian.Uint16(data[body+12 : body+14])
				bits = binary.LittleEndian.Uint16(data[body+14 : body+16])
				haveFmt = true
			}
		case "data":
			dataSize = size
			switch {
			case size == 0 || size == int64(^uint32(0)):
				// 流式写入没回填 size：剩余字节就是音频本体。
				dataSize = total - body
			case body+size > total:
				// 声明长度超过实际字节数（文件被截断）：按实际可读部分算。
				dataSize = total - body
			}
			haveData = true
			if haveFmt {
				// data 之后是音频本体，不必再遍历。
				break loop
			}
		}
		next := body + size
		if size%2 == 1 {
			next++ // chunk 奇数长度后的填充字节
		}
		if next <= off {
			return 0, false // 防御损坏的 size 造成死循环
		}
		off = next
	}
	if !haveFmt || !haveData {
		return 0, false
	}
	// fmt 的 byteRate 是权威值；缺失时自行折算每帧字节数。
	rate := int64(byteRate)
	if rate <= 0 {
		frame := int64(channels) * int64(bits) / 8
		if frame <= 0 {
			frame = int64(blockAlign)
		}
		rate = int64(sampleRate) * frame
	}
	if rate <= 0 {
		return 0, false
	}
	if dataSize < 0 {
		dataSize = 0
	}
	return float64(dataSize) / float64(rate), true
}

// filenameFromContentType 按 ContentType 决定上传文件名。
// 接口只接受 16 位 PCM WAV 与 Ogg Opus（由文件内容判断），mp3 等格式会被 400 拒掉；
// 所以非 WAV 一律按 Ogg Opus 命名。BatchSession 正常路径恒为 audio/wav。
func filenameFromContentType(ct string) string {
	ct = strings.ToLower(ct)
	if strings.Contains(ct, "wav") || strings.Contains(ct, "pcm") {
		return "recording.wav"
	}
	return "recording.opus"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
