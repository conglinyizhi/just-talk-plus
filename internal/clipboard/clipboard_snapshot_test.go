// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package clipboard

import "testing"

func TestSnapshotMetadataDoesNotExposeData(t *testing.T) {
	snapshot := Snapshot{
		Restorable: true,
		Formats: []FormatData{
			{Format: FormatText, Data: []byte("secret text")},
			{Format: FormatPNG, Data: []byte{1, 2, 3}},
		},
	}
	if got := snapshot.ByteLen(); got != len("secret text")+3 {
		t.Fatalf("ByteLen = %d", got)
	}
	names := snapshot.FormatNames()
	if len(names) != 2 || names[0] != string(FormatText) || names[1] != string(FormatPNG) {
		t.Fatalf("FormatNames = %v", names)
	}
}

func TestRestoreIfUnchangedSkipsUnrestorableSnapshot(t *testing.T) {
	clipboard := &Clipboard{}
	result, err := clipboard.RestoreIfUnchanged(Snapshot{Reason: "unsupported_format"}, "temporary")
	if err != nil {
		t.Fatal(err)
	}
	if result.Restored || result.Reason != "unsupported_format" {
		t.Fatalf("result = %#v", result)
	}
}
