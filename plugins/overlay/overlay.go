package overlay

import (
	"context"
	"log/slog"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
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
	cfg         config.OverlayConfig
	backend     backend
	lastState   string
	lastLeft    string
	lastRight   string
	lastVisible bool
}

func NewOverlayPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "overlay" }
func (p *Plugin) Version() string { return "0.1.0" }

func (p *Plugin) Init(env engine.PluginEnv) error {
	p.logger = env.Logger()
	p.cfg = env.Config().Overlay
	return nil
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
	p.sync(voice.TUIStatus())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-statusChanges:
			p.sync(voice.TUIStatus())
		case <-ticker.C:
			p.sync(voice.TUIStatus())
		}
	}
}

func (p *Plugin) Stop() error {
	if p.backend != nil {
		return p.backend.Close()
	}
	return nil
}

func (p *Plugin) sync(status voice.TUIVoiceStatus) {
	content := contentForStatus(status, p.cfg.IdleVisible)
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
