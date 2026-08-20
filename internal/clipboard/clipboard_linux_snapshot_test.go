// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

//go:build linux

package clipboard

import "testing"

func TestOneSupportedFormat(t *testing.T) {
	tests := []struct {
		name   string
		types  []string
		format Format
		reason string
	}{
		{
			name:   "text aliases are one semantic format",
			types:  []string{"text/plain", "text/plain;charset=utf-8", "TEXT", "STRING", "UTF8_STRING"},
			format: FormatText,
		},
		{
			name:   "single HTML",
			types:  []string{"TARGETS", "text/html", "TIMESTAMP"},
			format: FormatHTML,
		},
		{
			name:   "single PNG",
			types:  []string{"image/png"},
			format: FormatPNG,
		},
		{
			name:   "text plus HTML skips multi format",
			types:  []string{"text/plain", "text/html"},
			reason: "multiple_formats",
		},
		{
			name:   "PNG plus plain text skips multi format",
			types:  []string{"image/png", "text/plain"},
			reason: "multiple_formats",
		},
		{
			name:   "file list skips unsupported format",
			types:  []string{"text/uri-list"},
			reason: "unsupported_format",
		},
		{
			name:   "private format skips",
			types:  []string{"application/x-kde-private"},
			reason: "unsupported_format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, _, reason := oneSupportedFormat(tt.types)
			if reason != tt.reason {
				t.Fatalf("reason = %q, want %q", reason, tt.reason)
			}
			if format != tt.format {
				t.Fatalf("format = %q, want %q", format, tt.format)
			}
		})
	}
}

func TestTemporaryTextMatches(t *testing.T) {
	if !temporaryTextMatches([]string{"text/plain", "UTF8_STRING"}, []byte("temporary"), "temporary") {
		t.Fatal("expected matching text clipboard")
	}
	if temporaryTextMatches([]string{"image/png"}, []byte("temporary"), "temporary") {
		t.Fatal("image data must never be treated as temporary text")
	}
	if temporaryTextMatches([]string{"text/plain", "text/html"}, []byte("temporary"), "temporary") {
		t.Fatal("multi-format clipboard must not be restored")
	}
}

func TestSnapshotFromData(t *testing.T) {
	snapshot, err := snapshotFromData(FormatPNG, []byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Restorable || len(snapshot.Formats) != 1 || snapshot.Formats[0].Format != FormatPNG {
		t.Fatalf("snapshot = %#v, want restorable PNG", snapshot)
	}

	tooLarge := make([]byte, maxSnapshotBytes+1)
	snapshot, err = snapshotFromData(FormatPNG, tooLarge)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Restorable || snapshot.Reason != "snapshot_too_large" {
		t.Fatalf("snapshot = %#v, want oversized skip", snapshot)
	}
}
