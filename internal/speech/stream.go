package speech

import "context"

// StreamResult 流式识别过程中的一条结果。
type StreamResult struct {
	Text    string
	IsFinal bool
	Error   error
}

// StreamParams 开启流式识别会话的参数。
type StreamParams struct {
	Lang     string   // BCP-47；空 = 引擎默认/自动
	Hotwords []string // 热词（引擎支持时生效）
}

// StreamSession 一次流式识别会话。实现方应保证：
//   - Connect 建立连接（可重试语义由调用方控制），失败返回可读错误
//   - SendAudio 并发安全（内部串行化写）
//   - Results/Done/Final 只读通道，调用方不得关闭
//   - LastText 返回当前累积文本（并发安全）
//   - Close 释放连接；调用后 Done 应关闭
type StreamSession interface {
	Connect(ctx context.Context) error
	SendAudio(ctx context.Context, pcm []byte, isLast bool) error
	Results() <-chan StreamResult
	Done() <-chan struct{}
	Final() <-chan struct{}
	LastText() string
	Close() error
}

// StreamingEngine 由支持流式识别的引擎实现；批处理引擎可被 BatchSession 包装。
type StreamingEngine interface {
	Engine
	// NewStream 创建一个新会话（不连接；连接在 Session.Connect 中进行）。
	NewStream(ctx context.Context, params StreamParams) (StreamSession, error)
}
