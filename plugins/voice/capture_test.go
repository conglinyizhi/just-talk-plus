// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type captureTestReader struct {
	reads [][]byte
	index int
}

func (r *captureTestReader) Read(p []byte) (int, error) {
	if r.index >= len(r.reads) {
		return 0, io.EOF
	}
	data := r.reads[r.index]
	r.index++
	return copy(p, data), nil
}
func (r *captureTestReader) Close() error { return nil }

func TestCaptureAudioQueuesFinalChunkBeforeDone(t *testing.T) {
	rec := &Recorder{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), stdout: &captureTestReader{
		reads: [][]byte{{1, 2, 3, 4}, {5, 6}},
	}, started: true}
	queue := newAudioQueue()
	done := make(chan struct{})
	p := NewVoicePlugin()
	p.logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	go p.captureAudio(context.Background(), rec, queue, done)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture did not finish")
	}

	var got []byte
	for {
		chunk, err := queue.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, chunk.pcm...)
	}
	want := []byte{1, 2, 3, 4, 5, 6}
	if string(got) != string(want) {
		t.Fatalf("queued PCM = %v, want %v", got, want)
	}
}
