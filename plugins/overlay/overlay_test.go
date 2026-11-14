// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only

package overlay

import (
	"strings"
	"testing"
	"time"

	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

// testMetrics 固定的布局参数，配合下面的假测量函数做纯几何测试。
func testMetrics() capsuleMetrics {
	return capsuleMetrics{FontSize: 14, Padding: 10, Gap: 8, BallD: 40, BallInset: 4}
}

// fixedMeasure 假定等宽字体每字符 8 像素。
func fixedMeasure(s string) int { return len([]rune(s)) * 8 }

func TestCountdownLabelWidthIsStable(t *testing.T) {
	// 倒计时必须在两位/三位数之间保持相同字符数，否则胶囊宽度会跳。
	cases := []struct {
		remaining time.Duration
		want      string
	}{
		{1200 * time.Millisecond, "STOP   1.2s"},
		{9900 * time.Millisecond, "STOP   9.9s"},
		{99 * time.Second, "STOP  99.0s"},
		{120 * time.Second, "STOP 120.0s"},
	}
	for _, tc := range cases {
		got := countdownLabel(voice.TUIVoiceStatus{State: "stopping_delayed", StopAt: time.Now().Add(tc.remaining), StopDelayMs: 6000})
		if len(got) != len(tc.want) {
			t.Fatalf("remaining %s → %q 长度 %d, 与 %q 的 %d 不一致", tc.remaining, got, len(got), tc.want, len(tc.want))
		}
	}

	// 已结束的倒计时归零且宽度不变
	done := countdownLabel(voice.TUIVoiceStatus{State: "stopping_delayed", StopAt: time.Now().Add(-time.Millisecond), StopDelayMs: 6000})
	if done != "STOP   0.0s" {
		t.Fatalf("完成的倒计时 = %q", done)
	}
}

func TestContentForStatusMapsSemantics(t *testing.T) {
	// 左栏 = 本地采集侧，右栏 = 识别侧；两栏都该有内容，球居中时视觉才平衡。
	waiting := contentForStatus(voice.TUIVoiceStatus{State: "connecting", RecorderReady: true}, false)
	if !waiting.Visible || strings.TrimSpace(waiting.Left) != "MIC ON" || strings.TrimSpace(waiting.Right) != "ASR .." {
		t.Fatalf("等待声音 = %+v", waiting)
	}
	if waiting.Color != colorListening {
		t.Fatalf("等待声音应为黄色监听态, got %+v", waiting.Color)
	}

	dialing := contentForStatus(voice.TUIVoiceStatus{State: "connecting", RecorderReady: true, VoiceDetected: true}, false)
	if strings.TrimSpace(dialing.Right) != "ASR ->" || dialing.Color != colorCapturing {
		t.Fatalf("拨号中 = %+v", dialing)
	}

	connected := contentForStatus(voice.TUIVoiceStatus{State: "recording", RecorderReady: true, ASRReady: true, VoiceDetected: true}, false)
	if strings.TrimSpace(connected.Right) != "ASR ON" || strings.TrimSpace(connected.Left) != "MIC ON" {
		t.Fatalf("已连接 = %+v", connected)
	}

	// 倒计时在左栏，右栏预告还在等识别收尾——两栏都有内容
	stopping := contentForStatus(voice.TUIVoiceStatus{State: "stopping_delayed", StopAt: time.Now().Add(time.Second), StopDelayMs: 3000}, false)
	if !strings.HasPrefix(strings.TrimSpace(stopping.Left), "STOP ") {
		t.Fatalf("倒计时应占左栏: left=%q", stopping.Left)
	}
	if strings.TrimSpace(stopping.Right) != "ASR WAIT" {
		t.Fatalf("倒计时期间右栏应是 ASR WAIT, got %q", stopping.Right)
	}

	// 麦克风没起来时左栏应显示 MIC ..
	noMic := contentForStatus(voice.TUIVoiceStatus{State: "recording", RecorderReady: false}, false)
	if strings.TrimSpace(noMic.Left) != "MIC .." {
		t.Fatalf("麦克风未就绪 = %+v", noMic)
	}

	// 空闲默认隐藏，idle_visible 打开才显示
	if idle := contentForStatus(voice.TUIVoiceStatus{State: "idle"}, false); idle.Visible {
		t.Fatal("空闲且 idle_visible=false 时不应显示")
	}
	if idle := contentForStatus(voice.TUIVoiceStatus{State: "idle"}, true); !idle.Visible {
		t.Fatal("idle_visible=true 时空闲应显示")
	}
}

func TestPaddingKeepsColumnWidthFixed(t *testing.T) {
	// 补齐后所有文案的字符数一致，等宽字体下像素宽因此恒定。
	texts := []string{
		padRight("MIC ON  ASR ON"),
		padRight("WAIT ASR"),
		padRight("ERR"),
		padLeft(countdownLabel(voice.TUIVoiceStatus{State: "stopping_delayed", StopAt: time.Now().Add(120 * time.Second), StopDelayMs: 6000})),
		padLeft(countdownLabel(voice.TUIVoiceStatus{State: "stopping_delayed", StopAt: time.Now().Add(9 * time.Second), StopDelayMs: 6000})),
	}
	for _, s := range texts {
		if got := len([]rune(s)); got != columnCols {
			t.Fatalf("%q 补齐后 %d 字符, 期望 %d", s, got, columnCols)
		}
	}
}

// TestLayoutKeepsBallCentered 球必须严格居中。这是「左右等宽」的直接推论，
// 也是这次改版最容易被后续改动破坏的性质。
func TestLayoutKeepsBallCentered(t *testing.T) {
	contents := []capsuleContent{
		{Right: padRight("MIC ON  ASR ON")},
		{Left: padLeft("STOP 120.0s")},
		{Left: padLeft("STOP   9.9s"), Right: padRight("ERR")},
		{},
	}
	for _, c := range contents {
		l := computeLayout(c, testMetrics(), fixedMeasure)
		if l.BallX != l.Width/2 {
			t.Fatalf("球心 x=%d, 胶囊宽=%d（应为中心 %d）", l.BallX, l.Width, l.Width/2)
		}
	}
}

// TestLayoutWidthDoesNotDependOnText 文案长短不能改变窗口尺寸，否则每帧都会
// 触发 resize 与坐标跳动。
func TestLayoutWidthDoesNotDependOnText(t *testing.T) {
	base := computeLayout(capsuleContent{Right: padRight("ERR")}, testMetrics(), fixedMeasure)
	for _, c := range []capsuleContent{
		{Right: padRight("MIC ON  ASR ON")},
		{Left: padLeft("STOP 120.0s")},
		{},
	} {
		l := computeLayout(c, testMetrics(), fixedMeasure)
		if l.Width != base.Width || l.Height != base.Height {
			t.Fatalf("尺寸随文案变化: %dx%d vs %dx%d", l.Width, l.Height, base.Width, base.Height)
		}
	}
}

// TestColumnsDoNotOverlapBall 两栏文字不能压到球上。
func TestColumnsDoNotOverlapBall(t *testing.T) {
	m := testMetrics()
	l := computeLayout(capsuleContent{
		Left:  padLeft("STOP 120.0s"),
		Right: padRight("MIC ON  ASR ON"),
	}, m, fixedMeasure)

	ballLeft := l.BallX - l.BallD/2
	ballRight := l.BallX + l.BallD/2

	if leftEnd := l.LeftX + fixedMeasure(l.LeftText); leftEnd > ballLeft {
		t.Fatalf("左栏右边界 %d 越过了球的左边缘 %d", leftEnd, ballLeft)
	}
	if l.RightX < ballRight {
		t.Fatalf("右栏起点 %d 落在了球的右边缘 %d 之内", l.RightX, ballRight)
	}
	if l.RightX+fixedMeasure(l.RightText) > l.Width {
		t.Fatalf("右栏越出胶囊右边界: %d > %d", l.RightX+fixedMeasure(l.RightText), l.Width)
	}
}

// TestLayoutScalesWithMetrics 缩放后仍保持居中与不重叠。
func TestLayoutScalesWithMetrics(t *testing.T) {
	for _, m := range []capsuleMetrics{
		{FontSize: 14, Padding: 10, Gap: 8, BallD: 36, BallInset: 4},
		{FontSize: 28, Padding: 20, Gap: 16, BallD: 72, BallInset: 8},
	} {
		measure := func(s string) int { return len([]rune(s)) * int(m.FontSize/2) }
		l := computeLayout(capsuleContent{Right: padRight("MIC ON  ASR ON")}, m, measure)
		if l.BallX != l.Width/2 {
			t.Fatalf("缩放后球心偏移: %d vs %d", l.BallX, l.Width/2)
		}
		if l.Height != m.BallD+m.BallInset*2 {
			t.Fatalf("高度 = %d, 期望 %d", l.Height, m.BallD+m.BallInset*2)
		}
	}
}
