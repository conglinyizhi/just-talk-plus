package overlay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

type backend interface {
	Show(label string, color statusColor) error
	Hide() error
	Close() error
}

type statusColor struct {
	R uint16
	G uint16
	B uint16
}

func capsuleContentLayout(width, dotSize, gap, textWidth int) (dotX, textX int) {
	contentWidth := dotSize + gap + textWidth
	dotX = (width - contentWidth) / 2
	if dotX < 0 {
		dotX = 0
	}
	return dotX, dotX + dotSize + gap
}

type Plugin struct {
	logger      *slog.Logger
	cfg         config.OverlayConfig
	backend     backend
	lastState   string
	lastLabel   string
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

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	p.logger.Info("overlay started")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
	label, color, visible := displayForStatus(status, p.cfg.IdleVisible)
	if status.State == p.lastState && label == p.lastLabel && visible == p.lastVisible {
		return
	}
	p.lastState, p.lastLabel, p.lastVisible = status.State, label, visible

	if !visible {
		if err := p.backend.Hide(); err != nil {
			p.logger.Debug("overlay hide failed", "error", err)
		}
		return
	}
	if err := p.backend.Show(label, color); err != nil {
		p.logger.Debug("overlay show failed", "error", err)
	}
}

func displayForStatus(status voice.TUIVoiceStatus, idleVisible bool) (string, statusColor, bool) {
	switch status.State {
	case "connecting":
		return captureASRLabel(status), statusColor{R: 245 << 8, G: 190 << 8, B: 70 << 8}, true
	case "recording":
		return captureASRLabel(status), statusColor{R: 255 << 8, G: 65 << 8, B: 65 << 8}, true
	case "stopping_delayed":
		return stopDelayLabel(status), statusColor{R: 255 << 8, G: 140 << 8, B: 60 << 8}, true
	case "stopping":
		return "ASR WAIT", statusColor{R: 255 << 8, G: 160 << 8, B: 70 << 8}, true
	case "error":
		return "ERR", statusColor{R: 255 << 8, G: 65 << 8, B: 65 << 8}, true
	default:
		return "IDL", statusColor{R: 145 << 8, G: 145 << 8, B: 145 << 8}, idleVisible
	}
}

func captureASRLabel(status voice.TUIVoiceStatus) string {
	if status.RecorderReady && status.ASRReady {
		return "MIC ON  ASR ON"
	}
	if status.RecorderReady {
		return "MIC ON  ASR .."
	}
	return "MIC ..  ASR .."
}

func stopDelayLabel(status voice.TUIVoiceStatus) string {
	remaining := 0.0
	if status.StopDelayMs > 0 && !status.StopAt.IsZero() {
		remaining = time.Until(status.StopAt).Seconds()
	}
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 99.9 {
		remaining = 99.9
	}
	return fmt.Sprintf("STOP %04.1fs", remaining)
}
