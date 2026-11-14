package overlay

import (
	"context"
	"log/slog"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
	"github.com/conglinyizhi/just-talk-plus/internal/speech"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

type backend interface {
	Show(content capsuleContent) error
	Hide() error
	Close() error
}

type statusColor struct {
	R uint16
	G uint16
	B uint16
}

type Plugin struct {
	logger      *slog.Logger
	env         engine.PluginEnv
	cfg         config.OverlayConfig
	backend     backend
	lastState   string
	lastLeft    string
	lastRight   string
	lastVisible bool

	// 当前引擎的单次音频上限（毫秒，0 表示没声明）。按引擎类型缓存，
	// 避免每 50 毫秒重新构造一次引擎。
	limitEngine string
	limitMs     int
}

func NewOverlayPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "overlay" }
func (p *Plugin) Version() string { return "0.1.0" }

func (p *Plugin) Init(env engine.PluginEnv) error {
	p.env = env
	p.logger = env.Logger()
	p.cfg = env.Config().Overlay
	return nil
}

// envConfig 取当前生效的配置；未初始化时返回 nil（测试路径）。
func (p *Plugin) envConfig() *config.Config {
	if p.env == nil {
		return nil
	}
	return p.env.Config()
}

func (p *Plugin) Start(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	b, err := newBackend(p.cfg)
	if err != nil {
		p.logger.Warn("overlay unavailable", "error", err)
		return nil
	}
	p.backend = b
	defer p.backend.Close()

	statusChanges, unsubscribe := voice.SubscribeStatus()
	defer unsubscribe()
	// Draw the current state immediately, then react to state changes instead
	// of waiting for a polling interval. The ticker keeps the stop-delay
	// countdown and time-based error expiry visually current.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	p.logger.Info("overlay started")
	p.sync(voice.TUIStatus(), p.envConfig())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-statusChanges:
			p.sync(voice.TUIStatus(), p.envConfig())
		case <-ticker.C:
			p.sync(voice.TUIStatus(), p.envConfig())
		}
	}
}

func (p *Plugin) Stop() error {
	if p.backend != nil {
		return p.backend.Close()
	}
	return nil
}

// audioLimitMs 返回当前 ASR 引擎能接受的最长单次音频。
//
// 左栏的倒计时就靠它：DeepSeek 这类引擎有硬上限（120 秒），超过了请求会被
// 直接拒绝，所以必须在胶囊上让用户看到还剩多少。
func (p *Plugin) audioLimitMs(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	vc := cfg.Voice
	engType := asrcfg.EngineType(vc)
	if engType == p.limitEngine {
		return p.limitMs
	}
	limit := 0
	if eng, err := asrcfg.Build(vc); err == nil {
		limit = speech.MaxAudioDurationMs(eng)
	}
	p.limitEngine, p.limitMs = engType, limit
	return limit
}

func (p *Plugin) sync(status voice.TUIVoiceStatus, cfg *config.Config) {
	content := contentForStatus(status, p.cfg.IdleVisible, p.audioLimitMs(cfg))
	if status.State == p.lastState && content.Left == p.lastLeft &&
		content.Right == p.lastRight && content.Visible == p.lastVisible {
		return
	}
	p.lastState, p.lastLeft, p.lastRight, p.lastVisible = status.State, content.Left, content.Right, content.Visible

	if !content.Visible {
		if err := p.backend.Hide(); err != nil {
			p.logger.Debug("overlay hide failed", "error", err)
		}
		return
	}
	if err := p.backend.Show(content); err != nil {
		p.logger.Debug("overlay show failed", "error", err)
	}
}
