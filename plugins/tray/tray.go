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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/engine"
	"github.com/conglinyizhi/just-talk-plus/hotkey"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
	"github.com/conglinyizhi/just-talk-plus/internal/clipboard"
	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
	"github.com/energye/systray"
)

// Plugin is the system tray control plugin.
type Plugin struct {
	logger  *slog.Logger
	cfgPath string
	env     engine.PluginEnv

	quitOnce sync.Once
	stateMu  sync.Mutex

	// Cached state so we only write to the SNI properties / menu items when
	// something actually changed (writing a MenuItem triggers a D-Bus menu
	// redraw — doing it every tick is what made the menu flicker).
	lastIconBytes    []byte
	lastTooltip      string
	lastAndroidState string

	engineMenu  *systray.MenuItem
	engineItems map[string]*systray.MenuItem
	deviceMenu  *systray.MenuItem
	deviceItems map[string]*systray.MenuItem
	pttItem     *systray.MenuItem

	androidMenu       *systray.MenuItem
	androidStatusItem *systray.MenuItem
	androidAutoItem   *systray.MenuItem
	historyMenu       *systray.MenuItem

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

func (p *Plugin) onReady() {
	systray.SetIcon(statusIcon("idle"))
	systray.SetTooltip("Just Talk Plus · 待机")
	cfg := p.env.Config()

	// ASR engine picker. Checkable submenu items so the current engine shows a
	// check mark.
	p.engineMenu = systray.AddMenuItem("引擎", "切换语音识别引擎")
	for _, name := range asrcfg.Types() {
		name := name
		item := p.engineMenu.AddSubMenuItemCheckbox(name, "", name == cfg.Voice.Engine)
		item.Click(func() { p.setEngine(name) })
		p.engineItems[name] = item
	}

	// Microphone device picker.
	p.deviceMenu = systray.AddMenuItem("麦克风", "选择输入设备")
	autoItem := p.deviceMenu.AddSubMenuItemCheckbox("自动（默认）", "", cfg.Voice.Device == "")
	autoItem.Click(func() { p.setDevice("") })
	p.deviceItems[""] = autoItem
	// Virtual microphone (AndroidMic) is always selectable, whether or not the
	// server is running — it points the recorder at the embedded server.
	amItem := p.deviceMenu.AddSubMenuItemCheckbox("虚拟麦克风（AndroidMic）", "device=androidmic", cfg.Voice.Device == voice.AndroidMicDeviceName())
	amItem.Click(func() { p.setDevice(voice.AndroidMicDeviceName()) })
	p.deviceItems[voice.AndroidMicDeviceName()] = amItem
	if devs, err := voice.ListDevices(); err == nil {
		for _, d := range devs {
			d := d
			item := p.deviceMenu.AddSubMenuItemCheckbox(d, "", d == cfg.Voice.Device)
			item.Click(func() { p.setDevice(d) })
			p.deviceItems[d] = item
		}
	}

	// Recording behaviour: push-to-talk (hold) vs. toggle.
	p.pttItem = systray.AddMenuItemCheckbox("按住说话", "按住热键说话，松开停止", p.currentMode() == "hold")
	p.pttItem.Click(func() { p.setMode(p.toggleMode()) })

	systray.AddSeparator()

	// Android virtual microphone server control.
	p.androidMenu = systray.AddMenuItem("Android 虚拟麦克风", "把手机当麦克风（内嵌服务端）")
	p.androidStatusItem = p.androidMenu.AddSubMenuItem("状态: 未运行", "")
	p.androidStatusItem.Disable()
	p.androidAutoItem = p.androidMenu.AddSubMenuItemCheckbox("自动启用服务端", "启动时自动开启（androidmic_auto_start）", p.autoStart())
	p.androidAutoItem.Click(func() { p.setAutoStart(!p.autoStart()) })
	p.androidMenu.AddSubMenuItem("启动服务端", "").Click(func() { p.startAndroidMic() })
	p.androidMenu.AddSubMenuItem("停止服务端", "").Click(func() { p.stopAndroidMic() })
	p.androidMenu.AddSubMenuItem("设为当前输入设备", "device=androidmic").Click(func() {
		p.setDevice(voice.AndroidMicDeviceName())
	})
	p.androidMenu.AddSubMenuItem("复制诊断信息", "复制最近 10 次 AndroidMic 连接的收包与丢帧统计").Click(func() {
		p.copyAndroidMicDiagnostics()
	})

	p.buildHistoryMenu()

	systray.AddSeparator()

	// Open the run log.
	logItem := systray.AddMenuItem("打开日志", "查看运行日志文件")
	logItem.Click(func() { p.openLog() })

	// Config entry points. The daemon never runs the recording TUI, so the
	// menu surfaces the ways to edit config, and can spawn a terminal running
	// the config-only editor (--tui-config never grabs hotkeys, so it is safe
	// to run alongside the daemon).
	p.openFull = systray.AddMenuItem("完整设置", "修改配置的几种方式")
	p.openFull.Click(func() {
		p.logger.Info("完整设置菜单点击：向用户说明如何修改配置")
		go notifyMessage("修改配置", "你可以通过多种方式修改配置：\n\n· 配置模式：在终端运行 just-talk-plus --tui-config\n· 直接编辑配置文件：~/.config/just-talk-plus/config.toml\n· 托盘菜单：切换引擎 / 麦克风 / push-to-talk 等")
	})

	p.openConfigItem = systray.AddMenuItem("在终端打开配置模式", "唤醒一个终端运行 just-talk-plus --tui-config")
	p.openConfigItem.Click(func() { p.openConfigTerminal() })

	p.restartItem = systray.AddMenuItem("重启", "重新启动当前版本，不重启 systemd 用户服务")
	p.restartItem.Click(func() { p.restart() })

	p.quitItem = systray.AddMenuItem("退出", "退出 just-talk-plus")
	p.quitItem.Click(func() { go p.env.Engine().Stop() })

	// Initial consistency pass, then react to voice state changes immediately.
	// The low-frequency tick only probes Android service state.
	p.syncChecks()
	p.updateAndroidMic()
	statusChanges, unsubscribe := voice.SubscribeStatus()
	p.syncStatus(voice.TUIStatus())
	go p.tickLoop(statusChanges, unsubscribe)
}

func (p *Plugin) tickLoop(statusChanges <-chan struct{}, unsubscribe func()) {
	defer unsubscribe()
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-statusChanges:
			p.syncStatus(voice.TUIStatus())
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

func (p *Plugin) setTooltip(state string) {
	cfg := p.env.Config()
	tip := fmt.Sprintf("Just Talk Plus · %s · %s · %s",
		stateLabel(state), cfg.Voice.Engine, modeLabel(cfg.Voice.Mode))
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
	p.apply(cfg)
	p.logger.Info("tray: 切换引擎", "engine", name)
}

func (p *Plugin) setDevice(name string) {
	cfg := p.reloadConfig()
	if cfg == nil || cfg.Voice.Device == name {
		return
	}
	cfg.Voice.Device = name
	p.apply(cfg)
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
	p.apply(cfg)
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

func (p *Plugin) startAndroidMic() {
	cfg := p.reloadConfig()
	port := androidmicPort(cfg)
	if err := voice.AndroidMicStart(port); err != nil {
		p.logger.Error("tray: 启动 AndroidMic 服务端失败", "error", err)
	}
	p.updateAndroidMic()
}

func (p *Plugin) stopAndroidMic() {
	if err := voice.AndroidMicStop(); err != nil {
		p.logger.Error("tray: 停止 AndroidMic 服务端失败", "error", err)
	}
	p.updateAndroidMic()
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
	p.apply(cfg)
	p.logger.Info("tray: AndroidMic 自动启用", "on", on)
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

func (p *Plugin) apply(cfg *config.Config) {
	if err := config.SaveTo(p.cfgPath, cfg); err != nil {
		p.logger.Error("tray: 保存配置失败", "error", err)
		return
	}
	if err := p.env.Engine().ReloadConfig(cfg); err != nil {
		p.logger.Error("tray: 应用配置失败", "error", err)
	}
	p.syncChecks()
	p.updateAndroidMic()
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

func (p *Plugin) buildHistoryMenu() {
	p.historyMenu = systray.AddMenuItem("历史记录", "最近识别记录")
	for _, e := range p.recentHistory() {
		e := e
		item := p.historyMenu.AddSubMenuItem(truncate(e.Text, 24), e.Time.Format("15:04:05"))
		item.AddSubMenuItem("播放录音", "").Click(func() { p.replayRecording(e.RecordingPath) })
		item.AddSubMenuItem("复制识别文本", "").Click(func() { p.copyText(e.Text) })
		if e.UpstreamLogID != "" {
			item.AddSubMenuItem("复制报错 ID", e.UpstreamLogID).Click(func() { p.copyText(e.UpstreamLogID) })
		}
		if e.RecordingPath != "" {
			item.AddSubMenuItem("在文件管理器中显示", "").Click(func() { p.revealRecording(e.RecordingPath) })
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
