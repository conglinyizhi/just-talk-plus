// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

func useTestHistoryStore(t *testing.T) string {
	t.Helper()
	originalPathFunc := historyPathFunc
	path := filepath.Join(t.TempDir(), "history.json")
	historyPathFunc = func() string { return path }
	historyMu.Lock()
	history = nil
	historyMu.Unlock()
	t.Cleanup(func() {
		historyMu.Lock()
		history = nil
		historyMu.Unlock()
		historyPathFunc = originalPathFunc
	})
	return path
}

func TestHistoryRing(t *testing.T) {
	path := useTestHistoryStore(t)

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
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("history snapshot was not saved: %v", err)
	}
}

func TestHistoryEmpty(t *testing.T) {
	useTestHistoryStore(t)
	if len(History()) != 0 {
		t.Fatal("history should be empty")
	}
}

func TestHistoryStoresUpstreamLogID(t *testing.T) {
	useTestHistoryStore(t)

	recordHistory("识别结果", "doubao-log-1")
	recordHistory("识别结果", "doubao-log-2")
	items := History()
	if len(items) != 2 {
		t.Fatalf("len=%d, want 2", len(items))
	}
	if items[0].UpstreamLogID != "doubao-log-2" {
		t.Fatalf("latest log ID = %q", items[0].UpstreamLogID)
	}
}

func TestHistoryLoadsRuntimeSnapshot(t *testing.T) {
	useTestHistoryStore(t)
	recordHistory("恢复的记录", "doubao-log-restore")
	historyMu.Lock()
	history = nil
	historyMu.Unlock()

	loadHistory()
	items := History()
	if len(items) != 1 || items[0].Text != "恢复的记录" {
		t.Fatalf("loaded history = %+v", items)
	}
	if items[0].UpstreamLogID != "doubao-log-restore" {
		t.Fatalf("loaded log ID = %q", items[0].UpstreamLogID)
	}
}

func TestSaveRecordingAudioCreatesWAV(t *testing.T) {
	useTestHistoryStore(t)
	path := saveRecordingAudio([]byte{0, 0, 1, 0}, speech.PCMRate, 7)
	if path == "" {
		t.Fatal("recording path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) {
		t.Fatalf("recording is not WAV: %q", data[:min(len(data), 4)])
	}
}
