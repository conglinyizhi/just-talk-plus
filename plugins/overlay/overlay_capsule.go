package overlay

// 胶囊的内容与几何：这一层是纯计算，不碰任何平台 API。
//
// 拆出来的理由：四个平台的绘制原本各算各的几何，改一次效果要写三遍，而且没有
// 一行能测。现在文案映射与布局计算集中在这里（可用表驱动测试钉住），平台后端
// 只负责「按给定坐标把字画上去」。
//
// 布局约束：球恒定居中。整体宽 = pad·2 + 列宽·2 + 间隙·2 + 球径，
// 球心 = pad + 列宽 + 间隙 + 球径/2，整体中心 = 总宽/2，两者相等当且仅当左右
// 列宽相等。所以「两侧等宽」不是审美选择，它是球居中的数学前提。

import (
	"fmt"
	"strings"
	"time"

	"github.com/conglinyizhi/just-talk-plus/plugins/voice"
)

// capsuleContent 是一次绘制的语义内容：左栏辅助信息、右栏主状态、状态色。
type capsuleContent struct {
	Left    string
	Right   string
	Color   statusColor
	Visible bool
}

// 文案按固定列数排版：等宽字体下像素宽只取决于字符数，所以把列宽钉死，
// 文案长短变化不会让窗口宽度或坐标跳动。
const (
	// leftCols 容纳 "STOP 120.0s"（11 字符），也容纳 "MIC ON"。
	leftCols = 11
	// rightCols 容纳 "ASR WAIT"（8 字符）。
	rightCols = 8
	// columnCols 左右两栏统列数，取两者较大值——这是球能居中的前提。
	columnCols = leftCols
)

// contentForStatus 把语音状态映射成胶囊内容。
//
// 左栏给辅助信息（当前只有停止倒计时在用），右栏给录制/识别主状态。
func contentForStatus(status voice.TUIVoiceStatus, idleVisible bool) capsuleContent {
	// 左栏放本地采集侧（麦克风是否在采、停止倒计时），右栏放识别侧
	// （ASR 是否连上、是否在等最终结果）。两栏都有内容，球居中时视觉才平衡。
	switch status.State {
	case "connecting":
		left := padLeft(micLabel(status))
		if status.VoiceDetected {
			// 已检测到人声、正在拨 ASR：红灯，提示已经开始真正发送音频
			return capsuleContent{Left: left, Right: padRight("ASR ->"), Color: colorCapturing, Visible: true}
		}
		return capsuleContent{Left: left, Right: padRight("ASR .."), Color: colorListening, Visible: true}
	case "recording":
		return capsuleContent{Left: padLeft(micLabel(status)), Right: padRight(asrLabel(status)), Color: colorCapturing, Visible: true}
	case "stopping_delayed":
		// 等静音结束：左边报剩余时间，右边预告接下来在等识别收尾
		return capsuleContent{Left: padLeft(countdownLabel(status)), Right: padRight("ASR WAIT"), Color: colorStopping, Visible: true}
	case "waiting_asr":
		return capsuleContent{Left: padLeft(micLabel(status)), Right: padRight("WAIT ASR"), Color: colorCapturing, Visible: true}
	case "stopping":
		return capsuleContent{Left: padLeft(micLabel(status)), Right: padRight("ASR WAIT"), Color: colorStopping, Visible: true}
	case "error":
		return capsuleContent{Left: padLeft(micLabel(status)), Right: padRight("ERR"), Color: colorCapturing, Visible: true}
	default:
		return capsuleContent{Right: padRight("IDL"), Color: colorIdle, Visible: idleVisible}
	}
}

var (
	colorCapturing = statusColor{R: 255 << 8, G: 65 << 8, B: 65 << 8}  // 红：正在采集并发送
	colorListening = statusColor{R: 245 << 8, G: 190 << 8, B: 70 << 8} // 黄：等声音
	colorStopping  = statusColor{R: 255 << 8, G: 140 << 8, B: 60 << 8} // 橙：停止/收尾
	colorIdle      = statusColor{R: 145 << 8, G: 145 << 8, B: 145 << 8}
)

// micLabel 左栏：麦克风采集是否已经起来。
func micLabel(status voice.TUIVoiceStatus) string {
	if status.RecorderReady {
		return "MIC ON"
	}
	return "MIC .."
}

// asrLabel 右栏：识别链路状态。
func asrLabel(status voice.TUIVoiceStatus) string {
	if status.ASRReady {
		return "ASR ON"
	}
	if status.VoiceDetected {
		return "ASR ->"
	}
	return "ASR .."
}

// countdownLabel 停止倒计时。数字宽度固定，百位出现/消失时不整体位移。
func countdownLabel(status voice.TUIVoiceStatus) string {
	remaining := 0.0
	if status.StopDelayMs > 0 && !status.StopAt.IsZero() {
		remaining = time.Until(status.StopAt).Seconds()
	}
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 999.9 {
		remaining = 999.9
	}
	return fmt.Sprintf("STOP %5.1fs", remaining)
}

// padRight 把右栏文案补齐到固定列数（左对齐，尾部补空格）。
func padRight(s string) string { return padTo(s, columnCols) }

// padLeft 把左栏文案补齐到固定列数（右对齐，前部补空格）。
func padLeft(s string) string {
	runes := []rune(s)
	if len(runes) >= columnCols {
		return string(runes[:columnCols])
	}
	return strings.Repeat(" ", columnCols-len(runes)) + s
}

func padTo(s string, n int) string {
	runes := []rune(s)
	if len(runes) >= n {
		return string(runes[:n])
	}
	return s + strings.Repeat(" ", n-len(runes))
}

// capsuleMetrics 是布局需要的平台相关量：字号、内边距、间隙、球径。
type capsuleMetrics struct {
	FontSize  float64
	Padding   int // 左右外边距
	Gap       int // 栏与球之间的空隙
	BallD     int // 中间球的直径，同时决定胶囊高度
	BallInset int // 球相对胶囊上下边缘的内缩
}

// capsuleLayout 是一次绘制所需的全部坐标；后端照着画，不再自己算。
type capsuleLayout struct {
	Width, Height int
	Radius        int // 圆角半径（全圆角时取高度一半）
	BallD         int
	BallX, BallY  int // 球心
	LeftText      string
	LeftX         int
	RightText     string
	RightX        int
	TextBaseline  int
	FontSize      float64
}

// computeLayout 计算胶囊几何。measure 由平台提供（按 FontSize 测量像素宽），
// 这样等宽字体的实际字宽由真实字体决定，布局本身可以脱离图形环境测试。
func computeLayout(content capsuleContent, m capsuleMetrics, measure func(string) int) capsuleLayout {
	colW := measure(strings.Repeat("M", columnCols))
	if colW <= 0 {
		colW = columnCols * 8 // 测量失败时的保守回退
	}
	width := m.Padding*2 + colW*2 + m.Gap*2 + m.BallD
	height := m.BallD + m.BallInset*2

	layout := capsuleLayout{
		Width:    width,
		Height:   height,
		Radius:   height / 2,
		BallD:    m.BallD,
		BallX:    width / 2, // 左右列宽相等，故恒为中心
		BallY:    height / 2,
		FontSize: m.FontSize,
	}
	// 文字垂直居中：基线大约在中心再加半个字高。
	layout.TextBaseline = height/2 + int(m.FontSize)/2

	leftStart := m.Padding
	rightStart := m.Padding + colW + m.Gap + m.BallD + m.Gap

	// 左栏右对齐（贴近球），右栏左对齐（贴近球），视觉重量集中在中间。
	if content.Left != "" {
		layout.LeftText = content.Left
		layout.LeftX = leftStart + colW - measure(content.Left)
	}
	if content.Right != "" {
		layout.RightText = content.Right
		layout.RightX = rightStart
	}
	return layout
}
