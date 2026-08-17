// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package diag

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestMemoryBufferKeepsBoundedSnapshot(t *testing.T) {
	b := NewMemoryBuffer()
	payload := bytes.Repeat([]byte("x"), maxBytes+100)
	if _, err := b.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := b.Snapshot()
	if len(got) != maxBytes {
		t.Fatalf("snapshot bytes=%d, want %d", len(got), maxBytes)
	}
	if !bytes.Equal(got[:10], bytes.Repeat([]byte("x"), 10)) {
		t.Fatalf("snapshot does not retain the newest bytes")
	}
}

func TestMemoryBufferDisabledDropsAndCanEnable(t *testing.T) {
	b := NewDisabledMemoryBuffer()
	if b.Enabled() {
		t.Fatal("disabled buffer reports enabled")
	}
	_, _ = b.Write([]byte("discarded"))
	if got := b.Snapshot(); len(got) != 0 {
		t.Fatalf("disabled snapshot=%q, want empty", got)
	}
	b.SetEnabled(true)
	_, _ = b.Write([]byte("kept"))
	if got := string(b.Snapshot()); got != "kept" {
		t.Fatalf("enabled snapshot=%q, want kept", got)
	}
	b.SetEnabled(false)
	if got := b.Snapshot(); len(got) != 0 {
		t.Fatalf("disabled snapshot=%q, want empty", got)
	}
}

func TestMemoryBufferConcurrentWrites(t *testing.T) {
	b := NewMemoryBuffer()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.Write([]byte("entry\n"))
		}()
	}
	wg.Wait()
	if got := strings.Count(string(b.Snapshot()), "entry"); got != 8 {
		t.Fatalf("entries=%d, want 8", got)
	}
}
