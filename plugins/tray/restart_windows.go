// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build windows

package tray

import "os/exec"

// restartCurrentProcess starts the replacement process. Windows has no
// syscall.Exec equivalent, so the caller stops the current engine afterwards.
func restartCurrentProcess(exe string, args []string) error {
	return exec.Command(exe, args...).Start()
}
