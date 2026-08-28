// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
)

type reloadTestPlugin struct {
	reloadErr error
	seen      *config.Config
	resultErr error
	results   int
}

func (p *reloadTestPlugin) Name() string                { return "reload-test" }
func (p *reloadTestPlugin) Version() string             { return "test" }
func (p *reloadTestPlugin) Init(PluginEnv) error        { return nil }
func (p *reloadTestPlugin) Start(context.Context) error { return nil }
func (p *reloadTestPlugin) Stop() error                 { return nil }
func (p *reloadTestPlugin) OnConfigReload(cfg *config.Config) error {
	p.seen = cfg
	return p.reloadErr
}
func (p *reloadTestPlugin) OnConfigReloadResult(cfg *config.Config, err error) {
	if cfg != nil && p.seen != cfg {
		panic("observer received a different config pointer")
	}
	p.resultErr = err
	p.results++
}

func TestReloadConfigNotifiesObserverOnSuccess(t *testing.T) {
	provider := hotkey.NewMockProvider()
	e := New(provider, config.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	plugin := &reloadTestPlugin{}
	if err := e.LoadPlugin(plugin); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if err := e.ReloadConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if plugin.results != 1 || plugin.resultErr != nil || plugin.seen != cfg {
		t.Fatalf("reload result = count:%d err:%v seen:%p want success and %p", plugin.results, plugin.resultErr, plugin.seen, cfg)
	}
}

func TestScheduleConfigReloadCoalescesEvents(t *testing.T) {
	e := New(hotkey.NewMockProvider(), config.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.reloadCh = make(chan string, 1)
	e.scheduleConfigReload("/tmp/config.toml")
	time.Sleep(configReloadDebounce / 2)
	e.scheduleConfigReload("/tmp/config.toml")
	select {
	case path := <-e.reloadCh:
		if path != "/tmp/config.toml" {
			t.Fatalf("reload path = %q", path)
		}
	case <-time.After(time.Second):
		t.Fatal("coalesced config reload was not scheduled")
	}
	select {
	case path := <-e.reloadCh:
		t.Fatalf("duplicate reload was delivered for %q", path)
	case <-time.After(configReloadDebounce * 2):
	}
}

func TestReloadConfigNotifiesObserverOnFailure(t *testing.T) {
	provider := hotkey.NewMockProvider()
	e := New(provider, config.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	wantErr := errors.New("bad hotkey")
	plugin := &reloadTestPlugin{reloadErr: wantErr}
	if err := e.LoadPlugin(plugin); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if err := e.ReloadConfig(cfg); !errors.Is(err, wantErr) {
		t.Fatalf("ReloadConfig error = %v, want %v", err, wantErr)
	}
	if plugin.results != 1 || !errors.Is(plugin.resultErr, wantErr) {
		t.Fatalf("reload result = count:%d err:%v", plugin.results, plugin.resultErr)
	}
}
