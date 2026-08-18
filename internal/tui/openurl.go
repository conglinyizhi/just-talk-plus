// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package tui

// 用系统默认浏览器打开链接：Linux xdg-open / macOS open / Windows rundll32。
// 仅用于 TUI 内「相关链接」子页面的回车打开，失败只记日志不阻塞界面。

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func playRecording(path string) (openedFolder bool, err error) {
	player, lookupErr := exec.LookPath("ffplay")
	if lookupErr == nil {
		return false, exec.Command(player, "-nodisp", "-autoexit", path).Start()
	}
	return true, revealRecording(path)
}

func revealRecording(path string) error {
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
		return fmt.Errorf("open recording location: %w", err)
	}
	return nil
}
