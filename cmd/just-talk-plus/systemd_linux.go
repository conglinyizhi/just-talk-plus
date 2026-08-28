// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed systemd_user.service
var systemdUnitTemplate string

// installSystemdUser writes a systemd --user unit for just-talk-plus that runs
// the background daemon (--gui) with autostart and journal logging, then
// enables it. It does not start the service immediately (that would race the
// global hotkeys with any running daemon); the user starts it explicitly.
func installSystemdUser() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl 不可用（请确认系统使用 systemd）：%w", err)
	}
	out, err := exec.Command("systemctl", "--user", "is-system-running").CombinedOutput()
	state := strings.TrimSpace(string(out))
	// is-system-running 对 degraded / starting / maintenance 返回非零，但
	// user manager 仍在运行；只有明确 "off"（manager 未启动）才视为不可用。
	if err != nil && (state == "off" || state == "") {
		return fmt.Errorf("systemd --user 不可用（%s）：%w", state, err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败：%w", err)
	}

	unitDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("创建 unit 目录失败：%w", err)
	}
	unitPath := filepath.Join(unitDir, "just-talk-plus.service")
	unit := systemdUnit(exe)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("写入 unit 失败：%w", err)
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload 失败：%w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "just-talk-plus").Run(); err != nil {
		return fmt.Errorf("systemctl --user enable 失败：%w", err)
	}

	fmt.Printf("已安装 systemd 用户服务：%s\n", unitPath)
	fmt.Println("开机自启已启用（systemctl --user enable just-talk-plus）。")
	fmt.Println("若需系统未登录也开机自启：loginctl enable-linger $USER")
	fmt.Println("现在启动：systemctl --user start just-talk-plus")
	fmt.Println("查看日志：journalctl --user -u just-talk-plus -f")
	fmt.Println("停止：systemctl --user stop just-talk-plus")
	return nil
}

func systemdUnit(exe string) string {
	unit := strings.ReplaceAll(systemdUnitTemplate, "__EXE__", systemdEscapePath(exe))
	return strings.ReplaceAll(unit, "__BACKEND_ARGS__", systemdBackendArgs())
}

func systemdBackendArgs() string {
	backend := os.Getenv("JUST_TALK_PLUS_BACKEND")
	if backend == "" {
		switch {
		case os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland":
			backend = "wayland"
		case os.Getenv("DISPLAY") != "":
			backend = "x11"
		}
	}
	if backend != "wayland" && backend != "x11" {
		return ""
	}
	return " --backend " + backend
}

// systemdEscapePath escapes spaces (and tabs) in an ExecStart path; systemd
// treats a bare space as an argument separator, so it must be written as \x20.
func systemdEscapePath(p string) string {
	return strings.NewReplacer(" ", `\x20`, "\t", `\x09`).Replace(p)
}
