// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

//go:build !linux

package main

import "fmt"

// installSystemdUser is only meaningful on Linux (systemd --user). On other
// platforms it reports that the feature is unavailable.
func installSystemdUser() error {
	return fmt.Errorf("--install-systemd 仅支持 Linux 上的 systemd --user")
}
