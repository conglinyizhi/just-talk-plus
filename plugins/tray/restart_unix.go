// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package tray

import (
	"os"
	"syscall"
)

// restartCurrentProcess replaces the current process so a systemd user unit
// stays active while the newly built binary is loaded.
func restartCurrentProcess(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}
