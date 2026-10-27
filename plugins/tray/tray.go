// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

// Package tray implements a cross-platform system tray (menu bar) control
// surface for just-talk-plus so the background daemon can be operated without
// occupying a terminal window.
//
// It is only active when running without the terminal UI (--gui). The full
// terminal UI (just-talk-plus --tui) keeps its own in-window controls; opening
// it from the tray is a convenience hint, since a separate TUI process must not
// run simultaneously (both would fight over the global hotkeys).
//
// Menu actions reuse the existing public APIs where possible — engine switching
// goes through config reload (asrcfg.Build reads the config on every recording
// session), device switching through voice.ListDevices, and status through
// voice.TUIStatus / voice.AndroidMicStatus. Nothing here routes through the TUI.
//
// The daemon is a small control surface, not the core: if there is no tray host
// (headless session or broken D-Bus) the tray simply fails and recording keeps
// working. The status is surfaced by swapping the tray icon (a colored dot) and
// the tooltip rather than an editable menu row, and the menu items are only
// refreshed when something actually changes — writing to a MenuItem every
// second would trigger a D-Bus menu version bump and make KDE redraw (and thus
// flicker) the open menu.
//
// systray is fyne.io/systray, not energye/systray. On Linux it implements
// com.canonical.dbusmenu.ItemsPropertiesUpdated so in-process label changes
// (history / stats / device state) propagate to the host even when the target
// is a second-level submenu item — the older energye fork only emitted a
// top-level LayoutUpdated and Plasma kept the submenu content frozen at the
// first expansion.
package tray

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
	"github.com/conglinyizhi/just-talk-plus/internal/clipboard"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

// Plugin is the system tray control plugin.
type Plugin struct {
	logger  *slog.Logger
	cfgPath string
	env     engine.PluginEnv

	quitOnce sync.Once
	stateMu  sync.Mutex
	// manualReload marks an in-progress manual "刷新配置" reload so
	// OnConfigReloadResult suppresses its default toast (the refresh handler
	// shows the list of changed options instead). Automatic hot-reload is
	// unaffected.
	manualReload atomic.Bool

	// Cached state so we only write to the SNI properties / menu items when
	// something actually changed (writing a MenuItem triggers a D-Bus menu
	// redraw — doing it every tick is what made the menu flicker).
	lastIconBytes       []byte
	lastTooltip         string
	lastAndroidState    string
	lastHistorySnapshot string
	lastStatsSnapshot   string
	configStatusItem    *systray.MenuItem
	statsItems          [4]*systray.MenuItem

	engineMenu  *systray.MenuItem
	engineItems map[string]*systray.MenuItem
	// modelItems 是三级菜单「引擎 → 模型」的勾选项：engine → model id → item。
	// 目前只有 siliconflow 有模型子菜单。
	modelItems  map[string]map[string]*systray.MenuItem
	deviceMenu  *systray.MenuItem
	deviceItems map[string]*systray.MenuItem
	pttItem     *systray.MenuItem

	androidMenu       *systray.MenuItem
	androidStatusItem *systray.MenuItem
	androidAutoItem   *systray.MenuItem
	historyMenu       *systray.MenuItem
	historySlots      []*systray.MenuItem

	advancedMenu   *systray.MenuItem
	helpMenu       *systray.MenuItem
	openFull       *systray.MenuItem
	openConfigItem *systray.MenuItem
	restartItem    *systray.MenuItem
	quitItem       *systray.MenuItem
}

// NewPlugin creates a tray plugin that saves configuration to cfgPath.
func NewPlugin(cfgPath string) *Plugin {
	return &Plugin{
		cfgPath:     cfgPath,
		engineItems: make(map[string]*systray.MenuItem),
		modelItems:  make(map[string]map[string]*systray.MenuItem),
		deviceItems: make(map[string]*systray.MenuItem),
	}
}

// Name returns the plugin name.
func (p *Plugin) Name() string { return "tray" }

// Version returns the plugin version.
func (p *Plugin) Version() string { return "0.1.0" }

// Init stores the plugin environment.
func (p *Plugin) Init(env engine.PluginEnv) error {
	p.env = env
	p.logger = env.Logger()
	p.registerHotkeys()
	return nil
}

// Start runs the tray until the engine context is cancelled.
func (p *Plugin) Start(ctx context.Context) error {
	if !p.env.Config().Tray.Enabled {
		<-ctx.Done()
		return nil
	}
	// Engine shutdown (from the quit item or an OS signal) must also tear down
	// the tray; systray.Run only returns when Quit is called.
	go func() {
		<-ctx.Done()
		p.quit()
	}()
	p.runSystraySafe()
	return nil
}

// runSystraySafe runs the tray but contains any panic. The tray is only a
// control surface, not the core: if there is no tray host (e.g. a headless
// session or a broken D-Bus), the daemon must keep recording and the engine
// must survive without it.
func (p *Plugin) runSystraySafe() {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("tray: 系统托盘运行失败，已忽略（不影响录音）", "panic", r)
		}
	}()
	systray.Run(p.onReady, nil)
}

// Stop tears down the tray.
func (p *Plugin) Stop() error {
	p.quit()
	return nil
}

func (p *Plugin) quit() {
	p.quitOnce.Do(func() { systray.Quit() })
}

// click wires an energye-style `Click(fn)` API onto fyne.io/systray, which
// exposes a ClickedCh channel instead. Exactly one reader per menu item.
func click(item *systray.MenuItem, fn func()) {
	if item == nil {
		return
	}
	go func() {
		for range item.ClickedCh {
			fn()
		}
	}()
}

func (p *Plugin) onReady() {
	systray.SetIcon(statusIcon("idle"))
	systray.SetTooltip("Just Talk Plus")
	cfg := p.env.Config()

	// Usage stats shown at the very top of the main menu (no parent submenu).
	p.statsItems[0] = systray.AddMenuItem("总使用次数：0", "Just Talk Plus 语音输入统计")
	p.statsItems[1] = systray.AddMenuItem("输入字数：0", "")
	p.statsItems[2] = systray.AddMenuItem("平均速度：0 字/分钟", "")
	p.statsItems[3] = systray.AddMenuItem("最近速度：0 字/分钟", "")
	for _, item := range p.statsItems {
		item.Disable()
	}
	p.syncStatsMenu()

	systray.AddSeparator()

	// ASR engine picker. Checkable submenu items so the current engine shows a
	// check mark.
	p.engineMenu = systray.AddMenuItem("引擎", "切换语音识别引擎")
	for _, name := range asrcfg.Types() {
		name := name
		item := p.engineMenu.AddSubMenuItemCheckbox(name, "", name == cfg.Voice.Engine)
		click(item, func() { p.setEngine(name) })
		p.engineItems[name] = item
		// 三级菜单：支持选模型的引擎在引擎项下再挂一层模型勾选项。
		p.buildModelItems(item, name, cfg)
	}

	// Microphone device picker.
	p.deviceMenu = systray.AddMenuItem("麦克风", "选择输入设备")
	autoItem := p.deviceMenu.AddSubMenuItemCheckbox("自动（系统默认设备）", "", cfg.Voice.Device == "")
	click(autoItem, func() { p.setDevice("") })
	p.deviceItems[""] = autoItem
	// Virtual microphone (AndroidMic) is always selectable, whether or not the
	// server is running — it points the recorder at the embedded server.
	amItem := p.deviceMenu.AddSubMenuItemCheckbox("虚拟麦克风（AndroidMic）", "device=androidmic", cfg.Voice.Device == voice.AndroidMicDeviceName())
	click(amItem, func() { p.setDevice(voice.AndroidMicDeviceName()) })
	p.deviceItems[voice.AndroidMicDeviceName()] = amItem
	if devs, err := voice.ListDevices(); err == nil {
		for _, d := range devs {
			d := d
			item := p.deviceMenu.AddSubMenuItemCheckbox(d, "", d == cfg.Voice.Device)
			click(item, func() { p.setDevice(d) })
			p.deviceItems[d] = item
		}
	}

	// Recording behaviour: push-to-talk (hold) vs. toggle.
	p.pttItem = systray.AddMenuItemCheckbox("按住快捷键说话", "按住热键说话，松开停止", p.currentMode() == "hold")
	click(p.pttItem, func() { p.setMode(p.toggleMode()) })

	systray.AddSeparator()

	// Android virtual microphone server control.
	p.androidMenu = systray.AddMenuItem("Android 虚拟麦克风", "把手机当麦克风（内嵌服务端）")
	p.androidStatusItem = p.androidMenu.AddSubMenuItem("状态: 未运行", "")
	p.androidStatusItem.Disable()
	click(p.androidMenu.AddSubMenuItem("启动/停止服务端", "切换内嵌服务端的运行状态"), func() { p.toggleAndroidMic() })
	click(p.androidMenu.AddSubMenuItem("重启服务端", "停止并重新启动内嵌服务端，已连接的设备会重连"), func() { p.restartAndroidMic() })
	click(p.androidMenu.AddSubMenuItem("设为当前输入设备", "device=androidmic"), func() {
		p.setDevice(voice.AndroidMicDeviceName())
		p.notifyAndroidMicDeviceSet()
	})
	androidGuideItem := p.androidMenu.AddSubMenuItem("配置指南", "说明连接方式与使用步骤（当前仅支持 TCP/WIFI）")
	click(androidGuideItem, func() { p.showAndroidMicGuide() })
	p.androidAutoItem = p.androidMenu.AddSubMenuItemCheckbox("自动启用服务端", "启动时自动开启（androidmic_auto_start）", p.autoStart())
	click(p.androidAutoItem, func() { p.setAutoStart(!p.autoStart()) })

	p.buildHistoryMenu()

	systray.AddSeparator()

	// 配置 (open config mode) stays as a top-level item.
	p.openConfigItem = systray.AddMenuItem("配置", "在终端打开 just-talk-plus 配置模式")
	click(p.openConfigItem, func() { p.openConfigTerminal() })

	// Advanced submenu, placed above 帮助. Holds the less-frequent operations:
	// manual config reload, viewing the log, and restarting the process.
	p.advancedMenu = systray.AddMenuItem("高级", "高级操作")
	p.configStatusItem = p.advancedMenu.AddSubMenuItem("刷新配置", "重新读取配置文件并应用变更")
	click(p.configStatusItem, func() { p.refreshConfig() })
	advancedModelsItem := p.advancedMenu.AddSubMenuItem("刷新模型列表", "重新拉取 siliconflow 语音转写模型列表（下次启动核心后菜单生效）")
	click(advancedModelsItem, func() { p.refreshModels(true) })
	advancedLogItem := p.advancedMenu.AddSubMenuItem("查看核心日志", "查看运行日志文件")
	click(advancedLogItem, func() { p.openLog() })
	p.restartItem = p.advancedMenu.AddSubMenuItem("重启核心", "重新启动当前版本，不重启 systemd 用户服务")
	click(p.restartItem, func() { p.restart() })
	advancedDiagItem := p.advancedMenu.AddSubMenuItem("复制麦克风服务端日志", "复制最近 10 次 AndroidMic 连接的收包与丢帧统计")
	click(advancedDiagItem, func() { p.copyAndroidMicDiagnostics() })

	// Help submenu: holds config-related entries, placed above 关于.
	p.helpMenu = systray.AddMenuItem("帮助", "使用说明与配置入口")
	p.openFull = p.helpMenu.AddSubMenuItem("完整设置", "修改配置的几种方式")
	click(p.openFull, func() {
		p.logger.Info("完整设置菜单点击：向用户说明如何修改配置")
		go notifyMessage("修改配置", "你可以通过多种方式修改配置：\n\n· 配置模式：在终端运行 just-talk-plus --tui-config\n· 直接编辑配置文件：~/.config/just-talk-plus/config.toml\n· 托盘菜单：切换引擎 / 麦克风 / push-to-talk 等")
	})
	androidGuideFullItem := p.helpMenu.AddSubMenuItem("Android 麦克风配置指南", "Android 虚拟麦克风完整配置步骤")
	click(androidGuideFullItem, func() { p.showAndroidMicFullGuide() })
	click(p.helpMenu.AddSubMenuItem("下载 Android 客户端", "AndroidMic 官方客户端"), func() { p.openURL(aboutAmURL) })

	p.buildAboutMenu()

	p.quitItem = systray.AddMenuItem("退出", "退出 just-talk-plus")
	click(p.quitItem, func() { go p.env.Engine().Stop() })

	// Initial consistency pass, then react to voice state changes immediately.
	// The low-frequency tick only probes Android service state.
	p.syncChecks()
	p.updateAndroidMic()
	statusChanges, unsubscribeStatus := voice.SubscribeStatus()
	historyChanges, unsubscribeHistory := voice.SubscribeHistory()
	statsChanges, unsubscribeStats := voice.SubscribeStats()
	p.syncStatus(voice.TUIStatus())
	p.syncHistoryMenu()
	p.syncStatsMenu()
	go p.tickLoop(statusChanges, unsubscribeStatus, historyChanges, unsubscribeHistory, statsChanges, unsubscribeStats)

	// 模型列表的后台拉取放在菜单构建完成之后：构建阶段只用缓存/内置兜底，
	// 这里异步刷一次缓存（成功打日志，失败只 Warn），不阻塞也不改菜单。
	p.refreshModels(false)
}

func (p *Plugin) tickLoop(statusChanges <-chan struct{}, unsubscribeStatus func(), historyChanges <-chan struct{}, unsubscribeHistory func(), statsChanges <-chan struct{}, unsubscribeStats func()) {
	defer unsubscribeStatus()
	defer unsubscribeHistory()
	defer unsubscribeStats()
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-statusChanges:
			p.syncStatus(voice.TUIStatus())
		case <-historyChanges:
			p.syncHistoryMenu()
		case <-statsChanges:
			p.syncStatsMenu()
		case <-t.C:
			p.syncStatus(voice.TUIStatus())
			p.updateAndroidMic()
		case <-p.env.Engine().Context().Done():
			return
		}
	}
}

func (p *Plugin) syncStatus(st voice.TUIVoiceStatus) {
	p.setStatusIcon(st.State)
	p.setTooltip(st.State)
}

func (p *Plugin) setStatusIcon(state string) {
	b := statusIcon(state)
	if bytes.Equal(b, p.lastIconBytes) {
		return
	}
	p.lastIconBytes = b
	systray.SetIcon(b)
}

func (p *Plugin) setTooltip(_ string) {
	// The tray tooltip is intentionally just the app name — status/engine/mode
	// detail is conveyed by the icon colour, and a verbose tooltip would be
	// noisy when hovering the tray icon.
	tip := "Just Talk Plus"
	if tip == p.lastTooltip {
		return
	}
	p.lastTooltip = tip
	systray.SetTooltip(tip)
}

// syncChecks brings every checkable menu item in line with the current config.
// It is called at startup and after an action, never every tick.
func (p *Plugin) syncChecks() {
	cfg := p.env.Config()
	for name, item := range p.engineItems {
		if name == cfg.Voice.Engine {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	// 三级菜单的勾选态跟着当前引擎的 model 走：切引擎 / 改模型后都要对上。
	for engine, items := range p.modelItems {
		cur := currentModel(cfg, engine)
		for id, item := range items {
			if id == cur {
				item.Check()
			} else {
				item.Uncheck()
			}
		}
	}
	for name, item := range p.deviceItems {
		if name == cfg.Voice.Device {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	if p.pttItem != nil {
		if p.currentMode() == "hold" {
			p.pttItem.Check()
		} else {
			p.pttItem.Uncheck()
		}
	}
}

func (p *Plugin) setEngine(name string) {
	cfg := p.reloadConfig()
	if cfg == nil || cfg.Voice.Engine == name {
		return
	}
	cfg.Voice.Engine = name
	if err := p.apply(cfg); err != nil {
		return
	}
	p.logger.Info("tray: 切换引擎", "engine", name)
}

// buildModelItems 给支持选模型的引擎挂三级菜单「引擎 → 模型」。
// 候选来自缓存或内置兜底，构建阶段绝不发网络请求。
//
// 已知库限制：GTK 系托盘（Cinnamon / Xfce / GNOME）首次 GetLayout 会丢孙项，
// 靠 host 端 depth=1 再刷新兜底；Plasma（libdbusmenu-qt）不受影响。这里不做
// 绕行 hack（不预先展开、不改库），因为菜单本身在 Plasma 上已验证可用。
func (p *Plugin) buildModelItems(parent *systray.MenuItem, engine string, cfg *config.Config) {
	cur := currentModel(cfg, engine)
	choices := modelMenuChoices(engine, cur)
	if len(choices) == 0 {
		return
	}
	items := make(map[string]*systray.MenuItem, len(choices))
	for _, choice := range choices {
		choice := choice
		item := parent.AddSubMenuItemCheckbox(choice.ID, choice.Label, choice.ID == cur)
		click(item, func() { p.setEngineModel(engine, choice.ID) })
		items[choice.ID] = item
	}
	p.modelItems[engine] = items
}

// setEngineModel 把某个引擎的模型切到 model 并写回配置。选模型等于同时把当前
// 引擎切到该引擎（用户在引擎子菜单里点模型，显然是想用它识别）。
func (p *Plugin) setEngineModel(engine, model string) {
	cfg := p.reloadConfig()
	if cfg == nil {
		return
	}
	if !applyEngineModel(cfg, engine, model) {
		p.syncChecks()
		return
	}
	if err := p.apply(cfg); err != nil {
		return
	}
	p.logger.Info("tray: 切换模型", "engine", engine, "model", model)
}

// refreshModels 在后台拉一次硅基流动的语音转写模型列表并写缓存。
// explicit=false 是启动时的静默刷新（失败只 Warn）；explicit=true 是用户点
// 「刷新模型列表」，成功或失败都弹桌面提示。
//
// 菜单项本身只在 onReady 构建一次，所以这里只更新缓存并告知用户重启核心后
// 生效，不动态增删菜单项（避免运行中改动 D-Bus 菜单结构）。
func (p *Plugin) refreshModels(explicit bool) {
	cfg := p.env.Config()
	var baseURL, apiKey string
	if cfg != nil {
		per := cfg.Voice.EngineConfigFor(siliconflowEngine)
		baseURL, apiKey = per["base_url"], per["api_key"]
	}
	if strings.TrimSpace(apiKey) == "" {
		// 没配 api_key 不算拉取失败（多数用户根本没用 siliconflow）：启动时的
		// 静默刷新只记 Debug，别让每次启动都在日志里刷一行 Warn；用户主动点
		// 刷新时才 Warn + 提示。
		if !explicit {
			p.logger.Debug("tray: 未配置 siliconflow api_key，跳过模型列表拉取")
			return
		}
		p.logger.Warn("tray: 未配置 siliconflow api_key，无法拉取模型列表")
		go notifyMessage("刷新模型列表", "还没有配置 siliconflow 的 api_key，无法拉取模型列表。\n\n模型菜单继续使用内置候选，不影响录音。")
		return
	}
	if !explicit && len(cachedModels(siliconflowEngine, time.Now())) > 0 {
		// 缓存还在有效期内就跳过启动拉取：daemon 在开发中会被反复重启，每次启动
		// 打一发 GET 没有收益。要强制刷新走「高级 › 刷新模型列表」。
		p.logger.Debug("tray: 模型缓存仍在有效期，跳过启动拉取", "engine", siliconflowEngine)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), modelsFetchTimeout)
		defer cancel()
		models, err := fetchModels(ctx, baseURL, apiKey)
		if err != nil {
			p.logger.Warn("tray: 拉取模型列表失败，继续使用缓存/内置候选", "engine", siliconflowEngine, "error", err)
			if explicit {
				go notifyMessage("刷新模型列表失败", err.Error()+"\n\n模型菜单继续使用上次缓存或内置候选，不影响录音。")
			}
			return
		}
		if err := writeModelsCache(siliconflowEngine, models, time.Now()); err != nil {
			p.logger.Warn("tray: 写入模型缓存失败", "engine", siliconflowEngine, "error", err)
		}
		p.logger.Info("tray: 模型列表已更新", "engine", siliconflowEngine, "models", len(models))
		if explicit {
			go notifyMessage("模型列表已刷新", fmt.Sprintf("已刷新 %d 个模型，重启核心后菜单生效。", len(models)))
		}
	}()
}

func (p *Plugin) setDevice(name string) {
	cfg := p.reloadConfig()
	if cfg == nil || cfg.Voice.Device == name {
		return
	}
	cfg.Voice.Device = name
	if err := p.apply(cfg); err != nil {
		return
	}
	p.logger.Info("tray: 切换设备", "device", name)
}

func (p *Plugin) setMode(mode string) {
	cfg := p.reloadConfig()
	if cfg == nil {
		return
	}
	if cfg.Voice.Mode == mode {
		p.syncChecks()
		return
	}
	cfg.Voice.Mode = mode
	if err := p.apply(cfg); err != nil {
		return
	}
	p.logger.Info("tray: 切换语音模式", "mode", mode)
}

func (p *Plugin) toggleMode() string {
	if p.currentMode() == "hold" {
		return "toggle"
	}
	return "hold"
}

func (p *Plugin) currentMode() string {
	if m := p.env.Config().Voice.Mode; m != "" {
		return m
	}
	return "toggle"
}

func modeLabel(mode string) string {
	if mode == "hold" {
		return "Push-to-talk"
	}
	return "切换"
}

// ---- Android virtual microphone ----

// toggleAndroidMic starts the embedded server if it is stopped, or stops it if
// it is running. The menu item is a single "启动/停止服务端" entry.
func (p *Plugin) toggleAndroidMic() {
	if voice.AndroidMicStatus().Running {
		p.stopAndroidMic()
		return
	}
	p.startAndroidMic()
}

func (p *Plugin) startAndroidMic() {
	cfg := p.reloadConfig()
	port := androidmicPort(cfg)
	if err := voice.AndroidMicStart(port); err != nil {
		p.logger.Error("tray: 启动 AndroidMic 服务端失败", "error", err)
		go notifyMessage("启动失败", "Android 虚拟麦克风服务端启动失败：\n"+err.Error())
		return
	}
	p.updateAndroidMic()
	p.logger.Info("tray: 已启动 AndroidMic 服务端", "port", port)
	go notifyMessage("服务端已启动", fmt.Sprintf("Android 虚拟麦克风服务端已在端口 %d 启动，可在手机端连接。", port))
}

func (p *Plugin) stopAndroidMic() {
	if err := voice.AndroidMicStop(); err != nil {
		p.logger.Error("tray: 停止 AndroidMic 服务端失败", "error", err)
		go notifyMessage("停止失败", "Android 虚拟麦克风服务端停止失败：\n"+err.Error())
		return
	}
	p.updateAndroidMic()
	p.logger.Info("tray: 已停止 AndroidMic 服务端")
	go notifyMessage("服务端已停止", "Android 虚拟麦克风服务端已停止，已断开连接的设备。")
}

// restartAndroidMic 停止并重新启动内嵌 AndroidMic 服务端。断开已连接的客户端
// 让其在同一端口重新握手，用于设备连接异常或想要重置接收缓冲时。
func (p *Plugin) restartAndroidMic() {
	cfg := p.reloadConfig()
	port := androidmicPort(cfg)
	if err := voice.AndroidMicStop(); err != nil {
		p.logger.Error("tray: 重启 AndroidMic 服务端（停止阶段）失败", "error", err)
		go notifyMessage("AndroidMic 重启失败", "停止服务端失败：\n"+err.Error())
		return
	}
	if err := voice.AndroidMicStart(port); err != nil {
		p.logger.Error("tray: 重启 AndroidMic 服务端（启动阶段）失败", "error", err)
		go notifyMessage("AndroidMic 重启失败", "重新监听端口失败：\n"+err.Error())
		return
	}
	p.updateAndroidMic()
	p.logger.Info("tray: 已重启 AndroidMic 服务端", "port", port)
	go notifyMessage("AndroidMic 已重启", fmt.Sprintf("服务端已在端口 %d 重新启动，已连接的 Android 设备需要重新握手连接。", port))
}

// notifyAndroidMicDeviceSet explains what switching the recording input to the
// embedded Android virtual microphone affects, so the tray menu does not flip
// the recording source without telling the user.
func (p *Plugin) notifyAndroidMicDeviceSet() {
	go notifyMessage("已设为当前输入设备", "录音输入已切换到「Android 虚拟麦克风」（内嵌服务端）。\n\n影响：\n· 下次按下录音热键时，声音将从连接的 Android 设备采集（自动转换为 16 kHz / 单声道 / i16），而不是电脑麦克风。\n· 请在子菜单确认「Android 虚拟麦克风」服务端处于「运行中」，否则录音将采集不到声音。\n· 「麦克风」菜单中的勾选已同步到「虚拟麦克风（AndroidMic）」。")
}

// showAndroidMicGuide pops a brief "配置指南" from the Android 虚拟麦克风 submenu.
// It stays short; the full step-by-step guide lives under 帮助 › Android 麦克风配置指南.
func (p *Plugin) showAndroidMicGuide() {
	go notifyMessage("Android 虚拟麦克风配置", "当前版本仅支持 TCP 连接（Android 客户端上显示为 WIFI），还没适配 adb 等其他方式。\n\n音频格式请设为 16000 Hz / 单声道 / i16（PCM16）。\n\n下载 Android 客户端（AndroidMic）：点菜单「帮助 › 下载 Android 客户端」在浏览器打开，或访问 https://github.com/teamclouday/AndroidMic\n\n详细连接步骤见「帮助 › Android 麦克风配置指南」。")
}

// showAndroidMicFullGuide pops the full step-by-step AndroidMic configuration
// guide from the 帮助 submenu.
func (p *Plugin) showAndroidMicFullGuide() {
	port := p.androidPort()
	msg := fmt.Sprintf(`「Android 虚拟麦克风」配置指南

当前版本仅支持使用 TCP 的方式连接（Android 客户端上显示为 WIFI），暂时没有适配 adb 等其他连接方式。

使用步骤：
1. 让手机与电脑连接同一个局域网（WIFI）。
2. 点「启动服务端」，确认状态为「运行中（端口 %d）」。
3. 在 AndroidMic 客户端里填写电脑的局域网 IP 和端口 %d。
4. 连接成功后点「设为当前输入设备」，录音即从手机采集。

音频格式建议设为 16000 Hz / 单声道 / i16（PCM16）。其它格式难以保证识别质量，此前 F32 短读就出现过缓冲区尾部残留，会导致捕获的录音有概率完全无法识别。

端口可在配置 [voice] androidmic_port 修改（重启生效）。

下载 Android 客户端（AndroidMic）：https://github.com/teamclouday/AndroidMic（也可点菜单「帮助 › 下载 Android 客户端」在浏览器打开）
`, port, port)
	go notifyMessage("Android 虚拟麦克风配置指南", msg)
}

func (p *Plugin) copyAndroidMicDiagnostics() {
	report := voice.AndroidMicDiagnosticReport()
	cb, err := clipboard.New()
	if err == nil {
		err = cb.Set(report)
	}
	if err != nil {
		p.logger.Error("tray: 复制 AndroidMic 诊断信息失败", "error", err)
		go notifyMessage("AndroidMic 诊断", "复制失败，请查看系统日志")
		return
	}
	p.logger.Info("tray: 已复制 AndroidMic 诊断信息")
	go notifyMessage("AndroidMic 诊断", "最近 10 次连接摘要已复制，不包含音频、识别文本或密钥")
}

func (p *Plugin) setAutoStart(on bool) {
	cfg := p.reloadConfig()
	if cfg == nil || cfg.Voice.AndroidMicAutoStart == on {
		p.updateAndroidMic()
		return
	}
	cfg.Voice.AndroidMicAutoStart = on
	if err := p.apply(cfg); err != nil {
		return
	}
	p.logger.Info("tray: AndroidMic 自动启用", "on", on)
	if on {
		go notifyMessage("自动启用已开启", "启动 Just Talk Plus 时将自动启动 Android 虚拟麦克风服务端。")
	} else {
		go notifyMessage("自动启用已关闭", "启动 Just Talk Plus 时将不再自动启动 Android 虚拟麦克风服务端。")
	}
}

func (p *Plugin) autoStart() bool {
	return p.env.Config().Voice.AndroidMicAutoStart
}

func androidmicPort(cfg *config.Config) int {
	if cfg != nil && cfg.Voice.AndroidMicPort > 0 {
		return cfg.Voice.AndroidMicPort
	}
	return 54345
}

// OnConfigReloadResult shows the result of a configuration hot reload in the tray.
func (p *Plugin) OnConfigReloadResult(_ *config.Config, err error) {
	if err != nil {
		p.setConfigStatus("配置状态: 应用失败")
		go notifyMessage("配置修改失败", "配置文件已读取，但运行时应用失败：\n"+err.Error())
		return
	}
	p.setConfigStatus("配置状态: 已应用")
	// During a manual refresh the handler shows the list of changed options, so
	// suppress the generic toast to avoid two dialogs. Automatic hot-reload
	// (WatchConfig) still reports here as usual.
	if p.manualReload.Load() {
		return
	}
	go notifyMessage("配置已生效", "配置修改已应用到当前运行中的 Just Talk Plus")
}

func (p *Plugin) setConfigStatus(_ string) {
	// The former "配置状态" state row is now the clickable "刷新配置" action; keep
	// this as a no-op so the periodic rewrite of that row's title (which used to
	// be the feedback surface) no longer clobbers the action label. Feedback
	// moves to the reload notification dialogs.
}

// refreshConfig manually re-reads the config file on disk, applies it to the
// engine, and reports which options actually changed. Automatic hot-reload
// (WatchConfig) is untouched — this is an explicit user-initiated reload.
func (p *Plugin) refreshConfig() {
	diskCfg := p.reloadConfig()
	if diskCfg == nil {
		go notifyMessage("刷新配置失败", "无法读取配置文件")
		return
	}
	changes := diffConfig(p.env.Config(), diskCfg)
	if len(changes) == 0 {
		go notifyMessage("刷新配置", "完成重载，未发现配置变化")
		return
	}
	p.logger.Info("tray: 手动刷新配置", "changes", len(changes))
	p.manualReload.Store(true)
	err := p.env.Engine().ReloadConfig(diskCfg)
	p.manualReload.Store(false)
	if err != nil {
		p.logger.Error("tray: 刷新配置失败", "error", err)
		go notifyMessage("刷新配置失败", "应用配置失败：\n"+err.Error())
		return
	}
	go notifyMessage("配置已重载", "以下选项已改变并生效：\n\n"+strings.Join(changes, "\n"))
}

// diffConfig returns user-facing lines describing the options that differ
// between old and new config. Sensitive credential fields are reported only as
// "已变更" without exposing their values.
func diffConfig(oldCfg, newCfg *config.Config) []string {
	var changes []string
	diffConfigValue("", reflect.ValueOf(oldCfg).Elem(), reflect.ValueOf(newCfg).Elem(), &changes)
	return changes
}

func diffConfigValue(prefix string, oldV, newV reflect.Value, changes *[]string) {
	t := oldV.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		key := f.Tag.Get("toml")
		if key == "" || key == "-" {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		of := oldV.Field(i)
		nf := newV.Field(i)
		if reflect.DeepEqual(of.Interface(), nf.Interface()) {
			continue
		}
		if isSensitiveField(key) {
			*changes = append(*changes, path+": 已变更（凭据值不显示）")
			continue
		}
		if of.Kind() == reflect.Struct {
			diffConfigValue(path, of, nf, changes)
			continue
		}
		if of.Kind() == reflect.Map || of.Kind() == reflect.Slice {
			*changes = append(*changes, path+": 已变更")
			continue
		}
		if of.Kind() == reflect.Pointer {
			if of.IsNil() != nf.IsNil() {
				*changes = append(*changes, path+": "+fmt.Sprint(of.Interface())+" → "+fmt.Sprint(nf.Interface()))
			} else if !of.IsNil() {
				*changes = append(*changes, path+": "+fmt.Sprint(of.Elem().Interface())+" → "+fmt.Sprint(nf.Elem().Interface()))
			}
			continue
		}
		*changes = append(*changes, path+": "+fmt.Sprint(of.Interface())+" → "+fmt.Sprint(nf.Interface()))
	}
}

func isSensitiveField(key string) bool {
	k := strings.ToLower(key)
	return k == "app_key" ||
		k == "access_key" ||
		k == "resource_id" ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "password")
}

func (p *Plugin) updateAndroidMic() {
	st := voice.AndroidMicStatus()
	port := p.androidPort()
	var text string
	switch {
	case st.Running:
		text = fmt.Sprintf("状态: 运行中（端口 %d）", st.Port)
	case portOnline(port):
		// Another process (e.g. a leftover TUI/daemon) already listens on this
		// port — the client connects to that, but this process did not start.
		text = fmt.Sprintf("状态: 端口 %d 已有服务端（可能来自其它进程）", port)
	default:
		text = "状态: 未运行"
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if text == p.lastAndroidState {
		return
	}
	p.lastAndroidState = text
	if p.androidStatusItem != nil {
		p.androidStatusItem.SetTitle(text)
	}
}

// androidPort returns the AndroidMic port configured for this run.
func (p *Plugin) androidPort() int {
	if port := p.env.Config().Voice.AndroidMicPort; port > 0 {
		return port
	}
	return 54345
}

// portOnline reports whether something is actually listening on port. Used to
// distinguish "we started the server" from "another process owns the port" so
// the tray status is not misleading when a client has connected.
func portOnline(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// notifyMessage pops a desktop message box (kdialog) or, failing that, a
// notification (notify-send) so the tray does not silently do nothing.
func notifyMessage(title, msg string) {
	switch runtime.GOOS {
	case "windows":
		s := strings.ReplaceAll(msg, "'", "''")
		_ = exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("[System.Windows.Forms.MessageBox]::Show('%s','%s')", s, title)).Start()
	case "darwin":
		s := strings.ReplaceAll(msg, `"`, `\"`)
		_ = exec.Command("osascript", "-e",
			fmt.Sprintf("display dialog \"%s\" with title \"%s\"", s, title)).Start()
	default:
		if k, err := exec.LookPath("kdialog"); err == nil {
			_ = exec.Command(k, "--msgbox", msg, "--title", title).Start()
		} else if n, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command(n, "--icon=info", title, msg).Start()
		}
	}
}

// ---- log ----

func (p *Plugin) openLog() {
	if os.Getenv("INVOCATION_ID") != "" {
		// 由 systemd 托管时日志在 journald，打开文件是旧的/空的——改用 journalctl
		p.openJournal()
		return
	}
	path := logPath()
	p.logger.Info("tray: 打开日志", "path", path)
	if err := openPath(path); err != nil {
		p.logger.Error("tray: 打开日志失败", "error", err)
	}
}

func (p *Plugin) openJournal() {
	cmd := terminalCommand("journalctl", "--user", "-u", "just-talk-plus", "-e")
	if cmd == nil {
		go notifyMessage("查看日志", "请手动运行：journalctl --user -u just-talk-plus -e")
		return
	}
	if err := cmd.Start(); err != nil {
		p.logger.Error("tray: 打开 journalctl 终端失败", "error", err)
		go notifyMessage("查看日志", "启动终端失败。请手动运行 journalctl --user -u just-talk-plus -e")
	}
}

func logPath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserCacheDir(); err == nil {
			return filepath.Join(dir, "just-talk-plus", "just-talk-plus.log")
		}
	}
	return "/tmp/just-talk-plus.log"
}

func openPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ---- config reload ----

// reloadConfig reads the latest config from disk so we never mutate a config
// pointer that plugins are concurrently reading.
func (p *Plugin) reloadConfig() *config.Config {
	cfg, err := config.Load(p.cfgPath)
	if err != nil {
		p.logger.Error("tray: 读取配置失败", "error", err)
		return nil
	}
	return cfg
}

func (p *Plugin) apply(cfg *config.Config) error {
	if err := config.SaveTo(p.cfgPath, cfg); err != nil {
		p.logger.Error("tray: 保存配置失败", "error", err)
		p.setConfigStatus("配置状态: 保存失败")
		go notifyMessage("配置保存失败", "配置文件没有写入成功：\n"+err.Error())
		return err
	}
	if err := p.env.Engine().ReloadConfig(cfg); err != nil {
		p.logger.Error("tray: 应用配置失败", "error", err)
		return err
	}
	p.syncChecks()
	p.updateAndroidMic()
	return nil
}

// ---- optional global hotkeys ----

// registerHotkeys registers the optional global hotkeys defined in [tray].
// It only runs when the tray is enabled. Failures (parse errors, or a
// collision with the recording hotkey) are logged and skipped so they never
// disrupt the daemon.
func (p *Plugin) registerHotkeys() {
	t := p.env.Config().Tray
	if !t.Enabled {
		return
	}
	if t.CycleEngineHotkey != "" {
		combo, err := config.ParseHotkey(t.CycleEngineHotkey)
		if err != nil {
			p.logger.Warn("tray: 循环切引擎热键解析失败", "hotkey", t.CycleEngineHotkey, "error", err)
		} else if err := p.env.RegisterHotkeyWithOptions(combo, hotkey.RegisterOptions{}, p.onCycleEngineHotkey); err != nil {
			p.logger.Warn("tray: 循环切引擎热键注册失败（可能与录音热键冲突）", "hotkey", combo, "error", err)
		} else {
			p.logger.Info("tray: 循环切引擎热键已注册", "hotkey", combo)
		}
	}
	if t.TogglePushToTalkHotkey != "" {
		combo, err := config.ParseHotkey(t.TogglePushToTalkHotkey)
		if err != nil {
			p.logger.Warn("tray: push-to-talk 切换热键解析失败", "hotkey", t.TogglePushToTalkHotkey, "error", err)
		} else if err := p.env.RegisterHotkeyWithOptions(combo, hotkey.RegisterOptions{}, p.onTogglePTTHotkey); err != nil {
			p.logger.Warn("tray: push-to-talk 切换热键注册失败（可能与录音热键冲突）", "hotkey", combo, "error", err)
		} else {
			p.logger.Info("tray: push-to-talk 切换热键已注册", "hotkey", combo)
		}
	}
}

func (p *Plugin) onCycleEngineHotkey(evt hotkey.Event) {
	if evt.Type != hotkey.KeyDown {
		return
	}
	p.setEngine(nextEngine(p.env.Config().Voice.Engine))
}

func (p *Plugin) onTogglePTTHotkey(evt hotkey.Event) {
	if evt.Type != hotkey.KeyDown {
		return
	}
	p.setMode(p.toggleMode())
}

// nextEngine returns the ASR engine that follows current in the user-facing
// engine list, wrapping around to the first. An unknown/empty current falls
// back to the first engine.
func nextEngine(current string) string {
	types := asrcfg.Types()
	if len(types) == 0 {
		return ""
	}
	for i, name := range types {
		if name == current && i+1 < len(types) {
			return types[i+1]
		}
	}
	return types[0]
}

func stateLabel(state string) string {
	switch state {
	case "idle":
		return "待机"
	case "recording":
		return "录音中"
	case "stopping":
		return "停止中"
	case "error":
		return "错误"
	default:
		return state
	}
}

// statusIcon returns a colored dot PNG for the given recording state. The tray
// icon itself carries the status (color change), which is cheaper and more
// glanceable than an editable menu row, and writing it does not touch the D-Bus
// menu (so it cannot cause flicker).
func statusIcon(state string) []byte {
	var c color.RGBA
	switch state {
	case "recording":
		c = color.RGBA{230, 60, 60, 255}
	case "stopping":
		c = color.RGBA{230, 170, 50, 255}
	case "error":
		c = color.RGBA{160, 30, 30, 255}
	default: // idle and anything unknown
		c = color.RGBA{120, 140, 160, 255}
	}
	return dotIcon(c)
}

// dotIcon renders a filled circle into a small PNG so the tray icon is a simple
// colored dot per state (cross-platform, no external asset needed).
func dotIcon(c color.RGBA) []byte {
	const size = 16
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size-1) / 2.0
	radius := float64(size) / 2.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center
			dy := float64(y) - center
			if dx*dx+dy*dy <= radius*radius {
				img.SetRGBA(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func (p *Plugin) restart() {
	exe, err := os.Executable()
	if err != nil {
		p.logger.Error("tray: 定位重启程序失败", "error", err)
		go notifyMessage("重启失败", "无法定位当前程序")
		return
	}
	args := append([]string(nil), os.Args[1:]...)
	p.logger.Info("tray: 重启程序")
	go func() {
		p.env.Engine().Stop()
		p.env.Engine().Wait()
		if err := restartCurrentProcess(exe, args); err != nil {
			p.logger.Error("tray: 重启程序失败", "error", err)
			return
		}
		// Unix replaces this process inside restartCurrentProcess. On Windows
		// the replacement is a child, so the old process must exit explicitly.
		os.Exit(0)
	}()
}

func (p *Plugin) openConfigTerminal() {
	exe, err := os.Executable()
	if err != nil {
		p.logger.Error("tray: 定位可执行文件失败", "error", err)
		return
	}
	cmd := p.configTerminalCmd(exe)
	if cmd == nil {
		go notifyMessage("打开配置模式", "未找到可用的终端模拟器。\n请手动在终端运行：just-talk-plus --tui-config")
		return
	}
	if err := cmd.Start(); err != nil {
		p.logger.Error("tray: 启动配置终端失败", "error", err)
		go notifyMessage("打开配置模式", "启动终端失败。\n请手动在终端运行：just-talk-plus --tui-config")
	}
}

// configTerminalCmd 尝试用常见的终端模拟器唤起一个运行
// `just-talk-plus --tui-config` 的终端窗口。找不到终端时返回 nil。
func (p *Plugin) configTerminalCmd(exe string) *exec.Cmd {
	return terminalCommand(exe, "--tui-config")
}

// terminalCommand 尝试用常见的终端模拟器唤起一个运行 args... 的终端窗口。
// 找不到终端时返回 nil。
func terminalCommand(args ...string) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", append([]string{"/c", "start", ""}, args...)...)
	case "darwin":
		return exec.Command("open", append([]string{"-a", "Terminal"}, args...)...)
	default:
		for _, term := range []struct{ bin, flag string }{
			{"konsole", "-e"},
			{"x-terminal-emulator", "-e"},
			{"gnome-terminal", "--"},
			{"kitty", "-e"},
			{"alacritty", "-e"},
			{"xterm", "-e"},
		} {
			if path, err := exec.LookPath(term.bin); err == nil {
				return exec.Command(path, append([]string{term.flag}, args...)...)
			}
		}
		return nil
	}
}

const aboutJtgpURL = "https://github.com/conglinyizhi/just-talk-go-plus"
const aboutJtgpLicense = `just-talk-go-plus
许可协议：GPL-3.0（GNU General Public License v3.0）
版权：© 2026 conglinyizhi

说明：桌面语音输入工具。全局热键录音，流式 ASR 识别，文本上屏或写入剪贴板；支持 Linux、macOS、Windows。

修改与包装声明：本软件基于开源项目 just-talk-go（whoamihappyhacking，GPL-3.0）修改并包装而来，按 GPL-3.0 第 5 节保留原作者版权与变更声明，详见 NOTICE；第三方许可详见 THIRD_PARTY_NOTICES。`

const aboutJtgURL = "https://github.com/whoamihappyhacking/just-talk-go"
const aboutJtgLicense = `just-talk-go
许可协议：GPL-3.0（GNU General Public License v3.0）
版权：whoamihappyhacking

说明：Just Talk 的 Go 重写版，桌面语音输入工具（全局热键录音 → 流式 ASR 识别 → 上屏/复制到剪贴板）。本项目的直接上游。`

const aboutAmURL = "https://github.com/teamclouday/AndroidMic"
const aboutAmLicense = `AndroidMic
许可协议：GPL-3.0
作者/版权：wiiznokes / teamclouday

说明：把 Android 手机当电脑麦克风。Just Talk Plus 内嵌的 TCP 服务端（internal/androidmic）按其协议实现（Go 重写），音频统一转换为 16 kHz / 16-bit / 单声道 PCM。`

const historyMenuSlots = 8

// buildAboutMenu adds an "关于和许可" submenu so a tray-only (--gui) user can
// reach the open-source / license attributions of the project and its direct
// dependencies. Each project carries a license dialog and a link to its source.
func (p *Plugin) buildAboutMenu() {
	about := systray.AddMenuItem("关于", "开源与许可声明")
	p.addAboutProject(about, "just-talk-go-plus（当前）", "just-talk-go-plus 开源与许可", aboutJtgpLicense, aboutJtgpURL)
	p.addAboutProject(about, "just-talk-go", "just-talk-go 开源与许可", aboutJtgLicense, aboutJtgURL)
	p.addAboutProject(about, "androidmic", "AndroidMic 开源与许可", aboutAmLicense, aboutAmURL)
}

func (p *Plugin) addAboutProject(parent *systray.MenuItem, name, title, license, url string) {
	item := parent.AddSubMenuItem(name, "")
	click(item.AddSubMenuItem("开源许可", ""), func() { go notifyMessage(title, license) })
	click(item.AddSubMenuItem("查看源码", ""), func() { go p.openURL(url) })
}

func (p *Plugin) openURL(url string) {
	if err := openPath(url); err != nil {
		p.logger.Error("tray: 打开链接失败", "url", url, "error", err)
	}
}

func (p *Plugin) buildHistoryMenu() {
	p.historyMenu = systray.AddMenuItem("历史记录", "最近识别记录")
	p.historySlots = make([]*systray.MenuItem, 0, historyMenuSlots)
	for i := 0; i < historyMenuSlots; i++ {
		index := i
		item := p.historyMenu.AddSubMenuItem("暂无记录", "")
		click(item.AddSubMenuItem("使用当前引擎重试", "重新识别这条录音"), func() {
			if entry, ok := p.historyEntry(index); ok {
				p.retryHistory(entry)
			}
		})
		click(item.AddSubMenuItem("播放录音", ""), func() {
			if entry, ok := p.historyEntry(index); ok {
				p.replayRecording(entry.RecordingPath)
			}
		})
		click(item.AddSubMenuItem("复制识别文本", ""), func() {
			if entry, ok := p.historyEntry(index); ok {
				p.copyText(entry.Text)
			}
		})
		click(item.AddSubMenuItem("复制报错 ID", ""), func() {
			if entry, ok := p.historyEntry(index); ok {
				p.copyText(entry.UpstreamLogID)
			}
		})
		click(item.AddSubMenuItem("在文件管理器中显示", ""), func() {
			if entry, ok := p.historyEntry(index); ok {
				p.revealRecording(entry.RecordingPath)
			}
		})
		item.Hide()
		p.historySlots = append(p.historySlots, item)
	}
	p.syncHistoryMenu()
}

func (p *Plugin) historyEntry(index int) (voice.HistoryEntry, bool) {
	history := p.recentHistory()
	if index < 0 || index >= len(history) {
		return voice.HistoryEntry{}, false
	}
	return history[index], true
}

func (p *Plugin) syncHistoryMenu() {
	if p.historyMenu == nil {
		return
	}
	history := p.recentHistory()
	snapshot := historySnapshot(history)
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if snapshot == p.lastHistorySnapshot {
		return
	}
	p.lastHistorySnapshot = snapshot
	for i, item := range p.historySlots {
		if i < len(history) {
			entry := history[i]
			item.SetTitle(truncate(entry.Text, 24))
			item.Show()
		} else {
			item.Hide()
		}
	}
}

func (p *Plugin) recentHistory() []voice.HistoryEntry {
	h := voice.History()
	if len(h) > 8 {
		h = h[:8]
	}
	return h
}

func formatStatsSpeed(chars int, duration time.Duration) string {
	if duration <= 0 {
		return "0"
	}
	return fmt.Sprintf("%.0f", float64(chars)/duration.Minutes())
}

func (p *Plugin) syncStatsMenu() {
	if p.statsItems[0] == nil {
		return
	}
	stats := voice.TUIStats()
	values := [4]string{
		fmt.Sprintf("总使用次数：%d", stats.Sessions),
		fmt.Sprintf("输入字数：%d", stats.Chars),
		fmt.Sprintf("平均速度：%s 字/分钟", formatStatsSpeed(int(stats.Chars), stats.AudioDuration)),
		fmt.Sprintf("最近速度：%s 字/分钟", formatStatsSpeed(stats.LastTextChars, stats.LastAudioDuration)),
	}
	snapshot := strings.Join(values[:], "\x00")
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if snapshot == p.lastStatsSnapshot {
		return
	}
	p.lastStatsSnapshot = snapshot
	for i, item := range p.statsItems {
		item.SetTitle(values[i])
	}
}

func historySnapshot(history []voice.HistoryEntry) string {
	return fmt.Sprintf("%v", history)
}

func (p *Plugin) retryHistory(entry voice.HistoryEntry) {
	if entry.RecordingPath == "" {
		go notifyMessage("语音重试", "这条历史没有可用的录音文件。\n\n请确认录音时开启了「保留录音文件」。")
		return
	}
	go notifyMessage("语音重试", "正在读取录音并使用当前引擎识别…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		result, err := voice.RetryHistory(ctx, entry, p.env.Config().Voice)
		if err != nil {
			p.logger.Error("tray: 历史录音重试失败", "error", err)
			go notifyMessage("语音重试失败", err.Error()+"\n\n原历史记录未修改。")
			return
		}
		p.logger.Info("tray: 历史录音重试成功", "engine", result.Engine, "text_len", len(result.Text))
		go notifyMessage("语音重试成功", fmt.Sprintf("识别结果：\n%s\n\n已新增到历史记录。不会自动上屏或写入剪贴板；如需使用，请从历史记录中复制。", result.Text))
	}()
}

func (p *Plugin) replayRecording(path string) {
	if path == "" {
		return
	}
	if player, err := exec.LookPath("ffplay"); err == nil {
		if e := exec.Command(player, "-nodisp", "-autoexit", path).Start(); e != nil {
			p.logger.Error("tray: 播放录音失败", "error", e)
		}
		return
	}
	p.revealRecording(path)
}

func (p *Plugin) revealRecording(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,"+path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		p.logger.Error("tray: 打开录音位置失败", "error", err)
	}
}

func (p *Plugin) copyText(s string) {
	if s == "" {
		return
	}
	cb, err := clipboard.New()
	if err != nil {
		p.logger.Error("tray: 剪贴板初始化失败", "error", err)
		return
	}
	if err := cb.Set(s); err != nil {
		p.logger.Error("tray: 复制到剪贴板失败", "error", err)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
