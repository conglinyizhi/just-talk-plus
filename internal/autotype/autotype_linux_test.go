// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package autotype

import (
	"strings"
	"testing"
)

// TestSensitiveArgsStructure 验证 Wayland 上屏写入参数构造：
// 支持 --sensitive 时以它为前缀，且始终带 text/plain 类型。
func TestSensitiveArgsStructure(t *testing.T) {
	// 模拟支持 --sensitive 的 wl-copy（≥2.2）
	sensitiveOK = true
	args := buildWaylandCopyArgs(true)
	if len(args) < 3 || args[0] != "--sensitive" {
		t.Fatalf("sensitive args = %v, want leading --sensitive", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--type") || !strings.Contains(joined, "text/plain") {
		t.Fatalf("sensitive args missing text/plain: %v", args)
	}

	// 老版本 wl-copy：无 --sensitive
	args = buildWaylandCopyArgs(false)
	for _, a := range args {
		if a == "--sensitive" {
			t.Fatalf("old wl-copy should not get --sensitive: %v", args)
		}
	}
	if !strings.Contains(strings.Join(args, " "), "text/plain") {
		t.Fatalf("old wl-copy args missing text/plain: %v", args)
	}
}
