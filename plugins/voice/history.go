// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

// historyCap 运行期滚动保存的最近识别结果条数。
const historyCap = 50

// HistoryEntry 一条识别历史。
type HistoryEntry struct {
	Text          string
	Time          time.Time
	UpstreamLogID string
	RecordingPath string
}

var (
	historyMu       sync.Mutex
	history         []HistoryEntry // 最新在前
	historyPathFunc = defaultHistoryPath
)

func defaultHistoryPath() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "just-talk-plus", "history.json")
	}
	return filepath.Join(os.TempDir(), "just-talk-plus-history.json")
}

func recordingDir() string {
	return filepath.Join(filepath.Dir(historyPathFunc()), "recordings")
}

// loadHistory 恢复本次系统运行期间保存的历史。运行时目录通常会在系统重启后清理。
func loadHistory() {
	data, err := os.ReadFile(historyPathFunc())
	if err != nil {
		return
	}
	var entries []HistoryEntry
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	entries = trimHistory(entries)
	historyMu.Lock()
	history = entries
	historyMu.Unlock()
}

// recordHistory 记录一条成功上屏/复制的识别文本。
func recordHistory(text string, upstreamLogID ...string) {
	if text == "" {
		return
	}
	logID := ""
	if len(upstreamLogID) > 0 {
		logID = upstreamLogID[0]
	}
	recordHistoryEntry(text, logID, "")
}

func recordHistoryWithRecording(text, upstreamLogID, recordingPath string) bool {
	return recordHistoryEntry(text, upstreamLogID, recordingPath)
}

func recordHistoryEntry(text, logID, recordingPath string) bool {
	if text == "" {
		return false
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	// 与最新一条去重（避免同一次识别的重复记录）
	if len(history) > 0 && history[0].Text == text && history[0].UpstreamLogID == logID {
		return false
	}
	previous := history
	history = trimHistory(append([]HistoryEntry{{Text: text, Time: time.Now(), UpstreamLogID: logID, RecordingPath: recordingPath}}, history...))
	removeDiscardedRecordings(previous, history)
	saveHistoryLocked()
	return true
}

func saveRecordingAudio(pcm []byte, sampleRate int, sessionID uint64) string {
	if len(pcm) == 0 {
		return ""
	}
	dir := recordingDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ""
	}
	name := "recording-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(sessionID, 10) + ".wav"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, speech.EncodeWAVWithSampleRate(pcm, sampleRate), 0600); err != nil {
		return ""
	}
	return path
}

func removeDiscardedRecordings(previous, current []HistoryEntry) {
	kept := make(map[string]struct{}, len(current))
	for _, entry := range current {
		if entry.RecordingPath != "" {
			kept[entry.RecordingPath] = struct{}{}
		}
	}
	for _, entry := range previous {
		if entry.RecordingPath == "" {
			continue
		}
		if _, ok := kept[entry.RecordingPath]; !ok {
			_ = os.Remove(entry.RecordingPath)
		}
	}
}

func trimHistory(entries []HistoryEntry) []HistoryEntry {
	filtered := make([]HistoryEntry, 0, min(len(entries), historyCap))
	for _, entry := range entries {
		if entry.Text == "" {
			continue
		}
		filtered = append(filtered, entry)
		if len(filtered) == historyCap {
			break
		}
	}
	return filtered
}

func saveHistoryLocked() {
	path := historyPathFunc()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	data, err := json.Marshal(history)
	if err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".history-*")
	if err != nil {
		return
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return
	}
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	if err := os.Rename(temporaryName, path); err == nil {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = os.Rename(temporaryName, path)
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
	return CopyHistoryText(text)
}

// CopyHistoryText 把指定的历史文本复制到剪贴板。
func CopyHistoryText(text string) error { return writeClipboard(text) }
