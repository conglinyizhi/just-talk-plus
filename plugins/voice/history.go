// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"sync"
	"time"
)

// historyCap 内存中保留的最近识别结果条数。
const historyCap = 10

// HistoryEntry 一条识别历史。
type HistoryEntry struct {
	Text string
	Time time.Time
}

var (
	historyMu sync.Mutex
	history   []HistoryEntry // 最新在前
)

// recordHistory 记录一条成功上屏/复制的识别文本。
func recordHistory(text string) {
	if text == "" {
		return
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	// 与最新一条去重（避免同一次识别的重复记录）
	if len(history) > 0 && history[0].Text == text {
		return
	}
	history = append([]HistoryEntry{{Text: text, Time: time.Now()}}, history...)
	if len(history) > historyCap {
		history = history[:historyCap]
	}
}

// History 返回最近的识别结果（最新在前）。
func History() []HistoryEntry {
	historyMu.Lock()
	defer historyMu.Unlock()
	out := make([]HistoryEntry, len(history))
	copy(out, history)
	return out
}

// CopyHistoryEntry 把第 i 条历史复制到剪贴板。
func CopyHistoryEntry(i int) error {
	historyMu.Lock()
	if i < 0 || i >= len(history) {
		historyMu.Unlock()
		return nil
	}
	text := history[i].Text
	historyMu.Unlock()
	return writeClipboard(text)
}
