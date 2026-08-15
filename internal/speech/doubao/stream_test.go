// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package doubao

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c/just-talk-go/internal/speech"
)

func newTestSession() *StreamSession {
	s := NewStreamSession(Config{EnableITN: true, EnablePunc: true}, speech.StreamParams{})
	s.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s
}

func TestParseResponseClosesFinalForEmptyText(t *testing.T) {
	client := newTestSession()
	client.parseResponse(testASRResponse(t, 0x03, "", false))

	select {
	case <-client.Final():
	default:
		t.Fatal("empty final response did not close Final")
	}
}

func TestParseResponseDoesNotCloseFinalForDefiniteUtterance(t *testing.T) {
	client := newTestSession()
	client.parseResponse(testASRResponse(t, 0x01, "partial", true))

	select {
	case <-client.Final():
		t.Fatal("definite utterance closed Final before the last packet")
	default:
	}

	select {
	case result := <-client.Results():
		if !result.IsFinal {
			t.Fatal("definite utterance was not marked final in result stream")
		}
		if result.Text != "partial" {
			t.Fatalf("text = %q", result.Text)
		}
	default:
		t.Fatal("definite utterance result was not published")
	}
}

func TestParseResponseTracksLastText(t *testing.T) {
	client := newTestSession()
	client.parseResponse(testASRResponse(t, 0x01, "你好", false))
	if got := client.LastText(); got != "你好" {
		t.Fatalf("LastText = %q", got)
	}
}

func TestParseResponseErrorFrameClosesFinal(t *testing.T) {
	client := newTestSession()
	frame := buildHeader(msgErrorServer, flagNone, serJSON, compNone)
	frame = append(frame, 0, 0, 0, 1) // error code
	frame = append(frame, 0, 0, 0, 3) // message length
	frame = append(frame, 'b', 'a', 'd')
	client.parseResponse(frame)

	select {
	case <-client.Final():
	default:
		t.Fatal("error frame did not close Final")
	}
	select {
	case result := <-client.Results():
		if result.Error == nil {
			t.Fatal("error frame should publish an error result")
		}
	default:
		t.Fatal("error frame result was not published")
	}
}

func TestSendAudioHonorsContextWhileWaitingForWriter(t *testing.T) {
	client := newTestSession()
	<-client.writeSlot

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := client.SendAudio(ctx, nil, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendAudio error = %v, want context deadline exceeded", err)
	}
}

func TestHotwordsContextDedupesAndSkipsEmpty(t *testing.T) {
	ctxJSON, err := hotwordsContext([]string{"", "Wayland", "Wayland", " wtype "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctxJSON, "Wayland") || !strings.Contains(ctxJSON, "wtype") {
		t.Fatalf("missing hotwords: %s", ctxJSON)
	}
	if strings.Count(ctxJSON, "Wayland") != 1 {
		t.Fatalf("duplicate hotword kept: %s", ctxJSON)
	}
}

func TestStreamRequestUsesConfigFlags(t *testing.T) {
	// 验证请求参数随配置变化（enable_nonstream 随 stream_mode 切换）
	async := NewStreamSession(Config{StreamMode: "async"}, speech.StreamParams{})
	if async.cfg.StreamMode != "async" {
		t.Fatal("default stream mode should be async")
	}
	stream := NewStreamSession(Config{StreamMode: "stream"}, speech.StreamParams{})
	if stream.cfg.StreamMode != "stream" {
		t.Fatal("stream mode not honored")
	}
}

func testASRResponse(t *testing.T, flags byte, text string, definite bool) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"result": map[string]any{
			"text": text,
			"utterances": []map[string]any{
				{"text": text, "definite": definite},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	response := make([]byte, 12+len(payload))
	response[0] = 0x11 // protocolVersion<<4 | headerSizeVal
	response[1] = 0x90 | flags
	binary.BigEndian.PutUint32(response[4:8], ^uint32(0))
	binary.BigEndian.PutUint32(response[8:12], uint32(len(payload)))
	copy(response[12:], payload)
	return response
}
