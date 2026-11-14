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
	return capsuleMetrics{FontSize: 13, Padding: 10, Gap: 9, BallD: 34, BallInset: 3}
}

// fixedMeasure 假定等宽字体每字符 8 像素。
func fixedMeasure(s string) int { return len([]rune(s)) * 8 }

// TestTimeLabelWidthIsStable 左栏时间必须定宽：百位出现或消失时不能推动后面的字符，
// 否则每帧都会看到数字左右跳。
func TestTimeLabelWidthIsStable(t *testing.T) {
	limit := 120 * 1000 // 120 秒上限
	cases := []struct {
		elapsed time.Duration
		want    string
	}{
		{0, "R 120s"},                 // 刚开录：满额
		{20 * time.Second, "R 100s"},  // 刚掉到三位
		{21 * time.Second, "R  99s"},  // 掉到两位，前补空格
		{111 * time.Second, "R   9s"}, // 一位
		{200 * time.Second, "R   0s"}, // 超时归零
	}
	for _, tc := range cases {
		got := timeLabel(voice.TUIVoiceStatus{StartedAt: time.Now(), VoiceStartedAt: time.Now().Add(-tc.elapsed)}, limit)
		if len(got) != 6 {
			t.Fatalf("elapsed=%s → %q 宽度 %d, 期望 6", tc.elapsed, got, len(got))
		}
		if got != tc.want {
			t.Fatalf("elapsed=%s → %q, 期望 %q", tc.elapsed, got, tc.want)
		}
	}

	// 还没开口：满额静止。等声音的时间不该算成已经录掉的时长。
	if got := timeLabel(voice.TUIVoiceStatus{StartedAt: time.Now().Add(-30 * time.Second)}, limit); got != "R 120s" {
		t.Fatalf("未开口时应显示满额静止, got %q", got)
	}

	// 没有上限的引擎用 60 秒软上限，同样定宽
	if got := timeLabel(voice.TUIVoiceStatus{StartedAt: time.Now(), VoiceStartedAt: time.Now().Add(-5 * time.Second)}, 0); got != "R  55s" {
		t.Fatalf("软上限倒计时 = %q", got)
	}
	// 录制结束、会话还在收尾：显示 UPLOAD，正好填满一栏
	if got := timeLabel(voice.TUIVoiceStatus{}, 120000); got != uploadPlaceholder {
		t.Fatalf("收尾阶段应显示 UPLOAD, got %q", got)
	}
	if len(uploadPlaceholder) != columnCols {
		t.Fatalf("UPLOAD 长度 %d 应等于列宽 %d", len(uploadPlaceholder), columnCols)
	}

	// 停止状态下左栏不能是空的——那是用户报告的失衡场景
	stopping := contentForStatus(voice.TUIVoiceStatus{State: "stopping", PendingFinishes: 1}, false, 120000)
	if strings.TrimSpace(stopping.Left) != uploadPlaceholder {
		t.Fatalf("停止收尾时左栏应显示 UPLOAD, got %q", stopping.Left)
	}
	if strings.TrimSpace(stopping.Right) != "A.WAIT" {
		t.Fatalf("停止收尾时右栏 = %q", stopping.Right)
	}
}

func TestContentForStatusMapsSemantics(t *testing.T) {
	started := time.Now().Add(-10 * time.Second) // 开口已 10 秒，上限 120 秒 → 左栏应显示 110s
	opened := started
	// 左栏 = 本地采集侧，右栏 = 识别侧；两栏都该有内容，球居中时视觉才平衡。
	waiting := contentForStatus(voice.TUIVoiceStatus{State: "connecting", RecorderReady: true, StartedAt: started, VoiceStartedAt: opened}, false, 120000)
	if !waiting.Visible || strings.TrimSpace(waiting.Right) != "ASR .." {
		t.Fatalf("等待声音 = %+v", waiting)
	}
	// 左栏只剩时间：上限 120 秒、已录 10 秒 → 剩 110s
	if got := strings.TrimSpace(waiting.Left); got != "R 110s" {
		t.Fatalf("左栏应是剩余时间 110s, got %q", got)
	}
	if waiting.Color != colorListening {
		t.Fatalf("等待声音应为黄色监听态, got %+v", waiting.Color)
	}

	dialing := contentForStatus(voice.TUIVoiceStatus{State: "connecting", RecorderReady: true, VoiceDetected: true, StartedAt: started, VoiceStartedAt: opened}, false, 120000)
	if strings.TrimSpace(dialing.Right) != "ASR ->" || dialing.Color != colorCapturing {
		t.Fatalf("拨号中 = %+v", dialing)
	}

	connected := contentForStatus(voice.TUIVoiceStatus{State: "recording", RecorderReady: true, ASRReady: true, VoiceDetected: true, StartedAt: started, VoiceStartedAt: opened}, false, 120000)
	if strings.TrimSpace(connected.Right) != "ASR ON" {
		t.Fatalf("已连接 = %+v", connected)
	}

	// 倒计时在左栏，右栏预告还在等识别收尾——两栏都有内容
	stopping := contentForStatus(voice.TUIVoiceStatus{State: "stopping_delayed", StartedAt: started, VoiceStartedAt: opened, StopAt: time.Now().Add(time.Second), StopDelayMs: 3000}, false, 120000)
	// 停止延迟期间左栏时间继续走，不换成别的文案（否则就会闪烁）
	if got := strings.TrimSpace(stopping.Left); got != "R 110s" {
		t.Fatalf("停止延迟期间左栏应继续显示时间, got %q", got)
	}
	if strings.TrimSpace(stopping.Right) != "A.WAIT" {
		t.Fatalf("倒计时期间右栏应是 A.WAIT, got %q", stopping.Right)
	}

	// 没声明上限的引擎：套用 60 秒软上限倒计时（只提示，不强制停录）
	noLimit := contentForStatus(voice.TUIVoiceStatus{State: "recording", RecorderReady: true, StartedAt: started, VoiceStartedAt: opened}, false, 0)
	if got := strings.TrimSpace(noLimit.Left); got != "R  50s" {
		t.Fatalf("软上限倒计时应为 50s, got %q", got)
	}

	// 空闲默认隐藏，idle_visible 打开才显示
	if idle := contentForStatus(voice.TUIVoiceStatus{State: "idle"}, false, 120000); idle.Visible {
		t.Fatal("空闲且 idle_visible=false 时不应显示")
	}
	idle := contentForStatus(voice.TUIVoiceStatus{State: "idle"}, true, 120000)
	if !idle.Visible {
		t.Fatal("idle_visible=true 时空闲应显示")
	}
	// 空闲时没有任何上传，不能借用 UPLOAD
	if strings.TrimSpace(idle.Left) == uploadPlaceholder {
		t.Fatal("空闲态不应显示 UPLOAD")
	}
	if strings.TrimSpace(idle.Left) != idlePlaceholder {
		t.Fatalf("空闲占位 = %q", idle.Left)
	}
}

func TestPaddingKeepsColumnWidthFixed(t *testing.T) {
	// 补齐后所有文案的字符数一致，等宽字体下像素宽因此恒定。
	texts := []string{
		padRight("ASR ON"),
		padRight("ASR WAIT"),
		padRight("ERR"),
		padLeft(timeLabel(voice.TUIVoiceStatus{StartedAt: time.Now()}, 120000)),
		padLeft(timeLabel(voice.TUIVoiceStatus{StartedAt: time.Now().Add(-99 * time.Second)}, 120000)),
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
