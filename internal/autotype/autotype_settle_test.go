// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package autotype

import (
	"testing"
	"time"
)

// TestPasteSettleDelayValue 在全部平台（无 build tag）验证粘贴后保护窗口统一为 800ms：
// 常量定义在公共文件，Linux/Windows/macOS 引用同一值，避免各平台实现漂移。
func TestPasteSettleDelayValue(t *testing.T) {
	if pasteSettleDelay != 800*time.Millisecond {
		t.Fatalf("pasteSettleDelay = %v, want exactly 800ms across all platforms", pasteSettleDelay)
	}
}
