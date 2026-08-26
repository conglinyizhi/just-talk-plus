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

func TestCaptureASRLabelDistinguishesVoiceDetection(t *testing.T) {
	waiting := voice.TUIVoiceStatus{State: "connecting", RecorderReady: true}
	label, color, visible := displayForStatus(waiting, false)
	if !visible || label != "MIC ON  ASR .." || color != (statusColor{R: 245 << 8, G: 190 << 8, B: 70 << 8}) {
		t.Fatalf("waiting status = label %q color %+v visible %t", label, color, visible)
	}

	dialing := voice.TUIVoiceStatus{State: "connecting", RecorderReady: true, VoiceDetected: true}
	label, color, visible = displayForStatus(dialing, false)
	if !visible || label != "MIC ON  ASR ->" || color != (statusColor{R: 255 << 8, G: 65 << 8, B: 65 << 8}) {
		t.Fatalf("dialing status = label %q color %+v visible %t", label, color, visible)
	}

	awaitingASR := voice.TUIVoiceStatus{State: "waiting_asr"}
	label, color, visible = displayForStatus(awaitingASR, false)
	if !visible || label != "WAIT ASR" || color != (statusColor{R: 255 << 8, G: 65 << 8, B: 65 << 8}) {
		t.Fatalf("ASR connection wait = label %q color %+v visible %t", label, color, visible)
	}

	connected := voice.TUIVoiceStatus{State: "recording", RecorderReady: true, ASRReady: true, VoiceDetected: true}
	label, _, _ = displayForStatus(connected, false)
	if label != "MIC ON  ASR ON" {
		t.Fatalf("connected label = %q", label)
	}
}

func TestCapsuleContentLayoutCentersStatusGroup(t *testing.T) {
	dotX, textX := capsuleContentLayout(160, 14, 10, 80)
	if dotX != 28 || textX != 52 {
		t.Fatalf("layout = dot %d, text %d", dotX, textX)
	}
	if groupCenter := (dotX + textX + 80) / 2; groupCenter != 80 {
		t.Fatalf("group center = %d, want 80", groupCenter)
	}
}
