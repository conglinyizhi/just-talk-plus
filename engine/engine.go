// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
	"github.com/fsnotify/fsnotify"
)

// Engine is the application coordinator.
//
// It manages:
//   - The hotkey registry (platform-specific provider)
//   - Plugin loading and lifecycle
//   - Graceful shutdown on OS signals
type Engine struct {
	registry *hotkey.Registry
	provider hotkey.Provider
	plugins  []Plugin
	logger   *slog.Logger
	cfg      *config.Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu sync.Mutex

	reloadMu         sync.Mutex
	reloadTimer      *time.Timer
	reloadCh         chan string
	reloadGeneration uint64
}

// New creates a new Engine with the given hotkey provider, config, and logger.
func New(provider hotkey.Provider, cfg *config.Config, logger *slog.Logger) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		provider: provider,
		registry: hotkey.NewRegistry(provider),
		cfg:      cfg,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Provider returns the underlying hotkey provider.
func (e *Engine) Provider() hotkey.Provider {
	return e.provider
}

// Registry returns the hotkey registry.
func (e *Engine) Registry() *hotkey.Registry {
	return e.registry
}

// Logger returns the engine's logger.
func (e *Engine) Logger() *slog.Logger {
	return e.logger
}

// LoadPlugin adds a plugin to the engine. Must be called before Start().
func (e *Engine) LoadPlugin(p Plugin) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	pluginEnv := &pluginEnvAdapter{
		engine:  e,
		plugin:  p,
		handler: e.registry,
		logger:  e.logger.With("plugin", p.Name()),
	}

	if err := p.Init(pluginEnv); err != nil {
		return fmt.Errorf("plugin %s init: %w", p.Name(), err)
	}

	e.plugins = append(e.plugins, p)
	e.logger.Info("plugin loaded", "name", p.Name(), "version", p.Version())
	return nil
}

// Start begins the engine's event loop.
//
// It starts all plugins in their own goroutines, then starts the
// hotkey registry (which blocks). When the registry returns, the
// engine cancels all plugin contexts and waits for them to stop.
//
// If waitSignal is true, Start also listens for SIGINT/SIGTERM
// and calls Stop() when received.
func (e *Engine) Start(waitSignal bool) error {
	e.logger.Info("starting engine",
		"platform", e.provider.Info().Platform,
		"backend", e.provider.Info().Backend,
	)

	// Start plugins
	for _, p := range e.plugins {
		p := p // capture
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			if err := p.Start(e.ctx); err != nil && err != context.Canceled {
				e.logger.Error("plugin exited with error", "plugin", p.Name(), "error", err)
			}
		}()
	}

	// Signal handling
	if waitSignal {
		go e.handleSignals()
	}

	// Run the hotkey registry (blocks)
	err := e.registry.Start(e.ctx)

	// Shutdown
	e.logger.Info("engine stopping")
	e.cancel() // Cancel all plugin contexts

	// Stop all plugins
	for _, p := range e.plugins {
		if err := p.Stop(); err != nil {
			e.logger.Error("plugin stop error", "plugin", p.Name(), "error", err)
		}
	}

	e.wg.Wait()
	e.logger.Info("engine stopped")

	return err
}

// Stop gracefully shuts down the engine.
// ReloadConfig reloads configuration and notifies all plugins.
func (e *Engine) ReloadConfig(cfg *config.Config) error {
	e.mu.Lock()
	e.cfg = cfg
	plugins := append([]Plugin(nil), e.plugins...)
	e.mu.Unlock()
	var reloadErr error
	for _, p := range plugins {
		if r, ok := p.(Reloader); ok {
			if err := r.OnConfigReload(cfg); err != nil && reloadErr == nil {
				reloadErr = fmt.Errorf("%s: %w", p.Name(), err)
			}
		}
	}
	e.notifyConfigReloadResult(cfg, reloadErr)
	return reloadErr
}

func (e *Engine) notifyConfigReloadResult(cfg *config.Config, reloadErr error) {
	e.mu.Lock()
	plugins := append([]Plugin(nil), e.plugins...)
	e.mu.Unlock()
	for _, p := range plugins {
		if observer, ok := p.(ConfigReloadObserver); ok {
			observer.OnConfigReloadResult(cfg, reloadErr)
		}
	}
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.reloadMu.Lock()
	e.reloadGeneration++
	if e.reloadTimer != nil {
		e.reloadTimer.Stop()
		e.reloadTimer = nil
	}
	e.reloadCh = nil
	e.reloadMu.Unlock()

	e.logger.Info("engine stop requested")
	e.cancel()

	if err := e.registry.Stop(); err != nil {
		e.logger.Error("registry stop error", "error", err)
	}
}

// Context returns the engine's context (cancelled on shutdown).
func (e *Engine) Context() context.Context {
	return e.ctx
}

// Wait blocks until all plugins have exited.
func (e *Engine) Wait() {
	e.wg.Wait()
}

func (e *Engine) handleSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		e.logger.Info("received signal", "signal", sig)
		e.Stop()
	case <-e.ctx.Done():
		// Engine already stopping
	}
	signal.Stop(sigCh)
}

const configReloadDebounce = 100 * time.Millisecond

func (e *Engine) scheduleConfigReload(path string) {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	if e.reloadTimer != nil {
		e.reloadTimer.Stop()
	}
	if e.reloadCh == nil {
		e.reloadCh = make(chan string, 1)
	}
	e.reloadGeneration++
	generation := e.reloadGeneration
	e.reloadTimer = time.AfterFunc(configReloadDebounce, func() {
		e.reloadMu.Lock()
		if generation != e.reloadGeneration {
			e.reloadMu.Unlock()
			return
		}
		ch := e.reloadCh
		e.reloadTimer = nil
		e.reloadMu.Unlock()
		select {
		case ch <- path:
		default:
		}
	})
}

func (e *Engine) configReloadChannel() <-chan string {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	return e.reloadCh
}

func (e *Engine) reloadConfigFile(path string) {
	e.logger.Info("config file changed, reloading", "path", path)
	newCfg, err := config.Load(path)
	if err != nil {
		e.logger.Error("failed to reload config", "error", err)
		e.notifyConfigReloadResult(nil, err)
		return
	}
	if e.ctx.Err() != nil {
		return
	}
	e.mu.Lock()
	unchanged := reflect.DeepEqual(e.cfg, newCfg)
	e.mu.Unlock()
	if unchanged {
		return
	}
	if err := e.ReloadConfig(newCfg); err != nil {
		e.logger.Error("config reload failed", "error", err)
	}
}

// WatchConfig starts a file watcher that hot-reloads the config.
func (e *Engine) WatchConfig(path string) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		e.logger.Error("failed to create config watcher", "error", err)
		return
	}

	// Watch the directory, not the file — file truncation (os.Create)
	// removes the old inode and fsnotify loses the file watch.
	if err := watcher.Add(dir); err != nil {
		e.logger.Error("failed to watch config dir", "dir", dir, "error", err)
		watcher.Close()
		return
	}

	e.logger.Info("watching config for changes", "dir", dir, "file", base)
	e.reloadMu.Lock()
	e.reloadCh = make(chan string, 1)
	e.reloadMu.Unlock()

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-e.ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) != base {
					continue
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
					e.scheduleConfigReload(path)
				}
			case reloadPath := <-e.configReloadChannel():
				e.reloadConfigFile(reloadPath)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				e.logger.Error("config watcher error", "error", err)
			}
		}
	}()
}

// ---- pluginEnvAdapter implements PluginEnv ----

type pluginEnvAdapter struct {
	engine  *Engine
	plugin  Plugin
	handler *hotkey.Registry
	cfg     *config.Config
	logger  *slog.Logger
}

func (a *pluginEnvAdapter) RegisterHotkey(combo hotkey.Combo, handler func(hotkey.Event)) error {
	return a.handler.Register(combo, handler)
}

func (a *pluginEnvAdapter) RegisterHotkeyWithOptions(combo hotkey.Combo, opts hotkey.RegisterOptions, handler func(hotkey.Event)) error {
	return a.handler.RegisterWithOptions(combo, opts, handler)
}

func (a *pluginEnvAdapter) UnregisterHotkey(combo hotkey.Combo) error {
	return a.handler.Unregister(combo)
}

func (a *pluginEnvAdapter) Logger() *slog.Logger {
	return a.logger
}

func (a *pluginEnvAdapter) Config() *config.Config {
	return a.engine.cfg // Always return current (supports hot-reload)
}

func (a *pluginEnvAdapter) Engine() *Engine {
	return a.engine
}
