// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import "testing"

func TestHistoryRing(t *testing.T) {
	// 清空全局历史
	historyMu.Lock()
	history = nil
	historyMu.Unlock()

	total := historyCap + 3
	for i := 0; i < total; i++ {
		recordHistory(string(rune('A'+i)) + "-text")
	}
	items := History()
	if len(items) > historyCap {
		t.Fatalf("len=%d, want <= %d", len(items), historyCap)
	}
	// 最新在前
	last := string(rune('A'+total-1)) + "-text"
	if items[0].Text != last {
		t.Fatalf("first=%q, want %q", items[0].Text, last)
	}
	// 重复记录被去重
	recordHistory(items[0].Text)
	if len(History()) != len(items) {
		t.Fatal("duplicate should be dropped")
	}
	// 空文本不记录
	recordHistory("")
	if len(History()) != len(items) {
		t.Fatal("empty text should not be recorded")
	}
}

func TestHistoryEmpty(t *testing.T) {
	historyMu.Lock()
	history = nil
	historyMu.Unlock()
	if len(History()) != 0 {
		t.Fatal("history should be empty")
	}
}
