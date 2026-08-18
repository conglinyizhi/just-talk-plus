// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package overlay

import (
	"strings"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

func TestStopDelayLabelProgress(t *testing.T) {
	label, _, visible := displayForStatus(voice.TUIVoiceStatus{
		State: "stopping_delayed", StopAt: time.Now().Add(3 * time.Second), StopDelayMs: 6000,
	}, false)
	if !visible || !strings.HasPrefix(label, "STOP ") || !strings.HasSuffix(label, "s") {
		t.Fatalf("unexpected initial stop-delay label: %q", label)
	}

	label, _, _ = displayForStatus(voice.TUIVoiceStatus{
		State: "stopping_delayed", StopAt: time.Now().Add(-time.Millisecond), StopDelayMs: 6000,
	}, false)
	if label != "STOP 00.0s" {
		t.Fatalf("completed stop-delay label = %q", label)
	}
}
