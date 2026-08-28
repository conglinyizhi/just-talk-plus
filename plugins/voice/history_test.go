// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
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

func TestSubscribeHistoryReceivesChanges(t *testing.T) {
	useTestHistoryStore(t)
	changes, unsubscribe := SubscribeHistory()
	defer unsubscribe()
	recordHistory("诊断测试")
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("history change was not delivered")
	}
	if items := History(); len(items) != 1 || items[0].Text != "诊断测试" {
		t.Fatalf("history = %+v", items)
	}
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

func TestRetryHistoryMissingRecordingDoesNotChangeHistory(t *testing.T) {
	useTestHistoryStore(t)
	entry := HistoryEntry{Text: "原记录"}
	before := History()
	_, err := RetryHistory(context.Background(), entry, config.Default().Voice)
	if err == nil {
		t.Fatal("retry without recording should fail")
	}
	if got := History(); len(got) != len(before) {
		t.Fatalf("history changed after failed retry: before=%d after=%d", len(before), len(got))
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

func TestRecordNoResultHistory(t *testing.T) {
	useTestHistoryStore(t)

	// 无录音文件时不记录
	if recordNoResultHistory("", "") {
		t.Fatal("empty recording path should not be recorded")
	}
	if len(History()) != 0 {
		t.Fatal("history should stay empty")
	}

	// 连续多条无结果不去重（每次对应一次独立录音），且带占位文本
	first := filepath.Join(t.TempDir(), "rec-1.wav")
	second := filepath.Join(t.TempDir(), "rec-2.wav")
	if !recordNoResultHistory("log-a", first) {
		t.Fatal("first no-result entry should be recorded")
	}
	if !recordNoResultHistory("log-b", second) {
		t.Fatal("second no-result entry should be recorded")
	}
	items := History()
	if len(items) != 2 {
		t.Fatalf("len=%d, want 2", len(items))
	}
	if items[0].Text != noResultText || items[1].Text != noResultText {
		t.Fatalf("texts = %q / %q, want %q", items[0].Text, items[1].Text, noResultText)
	}
	// 最新在前，且各自保留录音路径与上游 log ID
	if items[0].RecordingPath != second || items[1].RecordingPath != first {
		t.Fatalf("recording paths reversed: %q / %q", items[0].RecordingPath, items[1].RecordingPath)
	}
	if items[0].UpstreamLogID != "log-b" || items[1].UpstreamLogID != "log-a" {
		t.Fatalf("log IDs = %q / %q", items[0].UpstreamLogID, items[1].UpstreamLogID)
	}

	// 与普通识别历史共存
	recordHistory("正常识别", "log-c")
	items = History()
	if len(items) != 3 {
		t.Fatalf("len=%d, want 3", len(items))
	}
	if items[0].Text != "正常识别" {
		t.Fatalf("latest text = %q, want 正常识别", items[0].Text)
	}
}
