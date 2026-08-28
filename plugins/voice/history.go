// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
	"github.com/conglinyizhi/just-talk-plus/internal/speech"
)

// historyCap 运行期滚动保存的最近识别结果条数。
const historyCap = 50

// RetryResult 是历史录音重新识别的结果。
type RetryResult struct {
	Text   string
	Engine string
}

// RetryHistory 使用当前配置的语音引擎重新识别一条历史录音。
// 成功后只新增历史，不执行上屏、剪贴板或统计输出；原历史条目保持不变。
func RetryHistory(ctx context.Context, entry HistoryEntry, vc config.VoiceConfig) (RetryResult, error) {
	if !retryMu.TryLock() {
		return RetryResult{}, fmt.Errorf("已有重试任务正在进行")
	}
	defer retryMu.Unlock()
	if strings.TrimSpace(entry.RecordingPath) == "" {
		return RetryResult{}, fmt.Errorf("这条历史没有可用的录音文件")
	}
	wav, err := os.ReadFile(entry.RecordingPath)
	if err != nil {
		return RetryResult{}, fmt.Errorf("读取录音失败: %w", err)
	}
	if len(wav) == 0 {
		return RetryResult{}, fmt.Errorf("录音文件为空")
	}
	eng, err := asrcfg.Build(vc)
	if err != nil {
		return RetryResult{}, err
	}
	if ok, detail := eng.Available(ctx); !ok {
		return RetryResult{}, fmt.Errorf("当前引擎不可用: %s", detail)
	}
	// 录音文件默认是标准 WAV；流式 DashScope 接口要求裸 PCM。
	audio := wav
	contentType := "audio/wav"
	if eng.Name() == "dashscope" {
		pcm, _, err := readWAV(entry.RecordingPath)
		if err != nil {
			return RetryResult{}, fmt.Errorf("解析录音失败: %w", err)
		}
		audio = pcm
		contentType = "audio/pcm"
	}
	result, err := eng.Transcribe(ctx, speech.TranscribeRequest{
		Audio:       audio,
		ContentType: contentType,
		Lang:        vc.Language,
	})
	if err != nil {
		return RetryResult{}, fmt.Errorf("%s 识别失败: %w", eng.Name(), err)
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return RetryResult{}, fmt.Errorf("%s 未识别到文本", eng.Name())
	}
	text = cleanRetryText(text, vc)
	if !recordRetryHistory(text, entry.RecordingPath) {
		return RetryResult{}, fmt.Errorf("识别成功，但历史记录写入失败")
	}
	return RetryResult{Text: text, Engine: eng.Name()}, nil
}

func cleanRetryText(text string, vc config.VoiceConfig) string {
	text = cleanTrailingPunct(text, vc)
	if vc.RemoveCJKSpace {
		text = cleanCJKSpace(text)
	}
	if vc.AutoSpace {
		text = autoSpaceCJK(text)
	}
	return text
}

func recordRetryHistory(text, recordingPath string) bool {
	if text == "" || recordingPath == "" {
		return false
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	previous := history
	history = trimHistory(append([]HistoryEntry{{Text: text, Time: time.Now(), RecordingPath: recordingPath}}, history...))
	removeDiscardedRecordings(previous, history)
	saveHistoryLocked()
	signalHistoryChangedLocked()
	return true
}

type HistoryEntry struct {
	Text          string
	Time          time.Time
	UpstreamLogID string
	RecordingPath string
}

var (
	historyMu        sync.Mutex
	history          []HistoryEntry // 最新在前
	historyPathFunc  = defaultHistoryPath
	historyListeners = make(map[chan struct{}]struct{})
	retryMu          sync.Mutex
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

// SubscribeHistory returns a coalescing notification channel for history
// changes. Consumers read the current snapshot through History().
func SubscribeHistory() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	historyMu.Lock()
	historyListeners[ch] = struct{}{}
	historyMu.Unlock()
	return ch, func() {
		historyMu.Lock()
		if _, ok := historyListeners[ch]; ok {
			delete(historyListeners, ch)
			close(ch)
		}
		historyMu.Unlock()
	}
}

func signalHistoryChangedLocked() {
	for ch := range historyListeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
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

// noResultText 是有录音但 ASR 未返回文本的历史条目占位文本。
const noResultText = "[无结果]"

func recordHistoryWithRecording(text, upstreamLogID, recordingPath string) bool {
	return recordHistoryEntry(text, upstreamLogID, recordingPath)
}

// recordNoResultHistory 记录一条「有录音但 ASR 未返回文本」的历史，便于回放该段录音。
// 与 recordHistoryEntry 不同：连续多条此类记录不去重（每条对应一次独立录音），
// 且没有可回放的录音文件时（recordingPath 为空）不记录。
func recordNoResultHistory(upstreamLogID, recordingPath string) bool {
	if recordingPath == "" {
		return false
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	previous := history
	history = trimHistory(append([]HistoryEntry{{Text: noResultText, Time: time.Now(), UpstreamLogID: upstreamLogID, RecordingPath: recordingPath}}, history...))
	removeDiscardedRecordings(previous, history)
	saveHistoryLocked()
	signalHistoryChangedLocked()
	return true
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
	signalHistoryChangedLocked()
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

// SetHistoryForTest 仅用于测试：直接替换历史快照，便于构造列表导航场景。
func SetHistoryForTest(entries []HistoryEntry) {
	historyMu.Lock()
	history = append([]HistoryEntry(nil), entries...)
	historyMu.Unlock()
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
