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
	// columnCols 两栏统一的列数。右栏最长是 "A.WAIT"，左栏是前缀加倒计时，
	// 两侧都正好填满，球居中时视觉才对称。
	columnCols = 6
)

// contentForStatus 把语音状态映射成胶囊内容。
//
// 左栏给辅助信息（当前只有停止倒计时在用），右栏给录制/识别主状态。
func contentForStatus(status voice.TUIVoiceStatus, idleVisible bool, limitMs int) capsuleContent {
	// 左栏只表达「时间」，右栏只表达「识别侧状态」。
	//
	// 左栏以前会在 MIC ON / STOP 倒计时 / MIC .. 之间切换，按下停止的那一刻
	// 会连续横跳好几次，看起来像闪烁。现在它唯二的两种内容都是时间，而且
	// 停止后冻结在最后一帧，不再回退到别的文案。
	timer := timeLabel(status, limitMs)
	left := padLeft(timer)

	// 左栏空着会让球看起来偏向一边，所以没有时间可显示时用占位符顶住。
	// 收尾阶段用 UPLOAD（确实在传），出错时用中性的短横线。
	placeholder := func(p string) string {
		if timer == "" {
			return padLeft(p)
		}
		return left
	}

	switch status.State {
	case "connecting":
		if status.VoiceDetected {
			// 已检测到人声、正在拨 ASR：红灯，提示已经开始真正发送音频
			return capsuleContent{Left: left, Right: padRight("ASR ->"), Color: colorCapturing, Visible: true}
		}
		return capsuleContent{Left: left, Right: padRight("ASR .."), Color: colorListening, Visible: true}
	case "recording":
		return capsuleContent{Left: left, Right: padRight(asrLabel(status)), Color: colorCapturing, Visible: true}
	case "stopping_delayed":
		// 还在录（等静音结束）：左栏的时间继续走，不换成别的文案
		return capsuleContent{Left: left, Right: padRight("A.WAIT"), Color: colorStopping, Visible: true}
	case "waiting_asr":
		return capsuleContent{Left: placeholder(uploadPlaceholder), Right: padRight("A.WAIT"), Color: colorCapturing, Visible: true}
	case "stopping":
		return capsuleContent{Left: placeholder(uploadPlaceholder), Right: padRight("A.WAIT"), Color: colorStopping, Visible: true}
	case "error":
		// 球用灰色：红色一直代表「正在采集/发送」，出错时那件事已经停了。
		t := errorTextFor(status.ErrorKind)
		return capsuleContent{Left: padLeft(t.left), Right: padRight(t.right), Color: colorIdle, Visible: true}
	default:
		return capsuleContent{Left: padLeft(idlePlaceholder), Right: padRight("IDL"), Color: colorIdle, Visible: idleVisible}
	}
}

var (
	colorCapturing = statusColor{R: 255 << 8, G: 65 << 8, B: 65 << 8}  // 红：正在采集并发送
	colorListening = statusColor{R: 245 << 8, G: 190 << 8, B: 70 << 8} // 黄：等声音
	colorStopping  = statusColor{R: 255 << 8, G: 140 << 8, B: 60 << 8} // 橙：停止/收尾
	colorIdle      = statusColor{R: 145 << 8, G: 145 << 8, B: 145 << 8}
)

// timePrefix 左栏时间的前缀。两栏等宽，右栏文字比纯数字长，所以左侧靠这个
// 前缀把重量补回来，否则球看上去会偏向一边。
const timePrefix = "R "

// uploadPlaceholder 录音已结束、音频正在上传并等待云端识别时占据左栏。
// 六个字符正好填满一栏，替换时不会改变胶囊尺寸或球的位置。
const uploadPlaceholder = "UPLOAD"

// errorText 错误状态在两栏里的文案。
//
// 出错时左栏不再显示时间——「还能录多久」已经没有意义了。识别侧和网络侧
// 保留 CRASH: 的说法，设备与配置两个本地故障用更好读的 ERROR + 单词。
type errorText struct {
	left  string
	right string
}

var errorTexts = map[string]errorText{
	"asr": {left: "CRASH:", right: "ASRERR"}, // 识别侧：服务端报错、识别超时、结束识别失败
	"net": {left: "CRASH:", right: "NETERR"}, // 连接与传输：建连失败、音频发送中断、发送超时
	"dev": {left: "ERROR", right: "DEVICE"},  // 本地录音设备：启动失败、读取失败
	"cfg": {left: "ERROR", right: "CONFIG"},  // 引擎配置错误
}

// errorTextFor 未知类别统一回落，不把内部字符串直接漏到屏幕上。
func errorTextFor(kind string) errorText {
	if t, ok := errorTexts[kind]; ok {
		return t
	}
	return errorText{left: "CRASH:", right: "ERR"}
}

// idlePlaceholder 空闲态的左栏占位。不能复用 UPLOAD——什么都没在传的时候
// 显示「上传」是假信息。六个短横线只表示「这里没有正在计时的事」。
const idlePlaceholder = "------"

// softLimitMs 是没有硬上限的引擎使用的软上限：只作提示，不强制停录。
// 一分钟以内的语音识别成功率明显更高，所以默认也按 60 秒倒计数。
const softLimitMs = 60 * 1000

// timeLabel 左栏唯一的内容：距离上限还剩多少秒。
//
// 起点是「第一次检测到人声」（VoiceStartedAt），不是麦克风启动。按下热键后
// 一直没说话时显示满额静止，开口后才开始倒扣——否则等声音的时间会被算成
// 已经录掉的时长，看起来像白白流走。
//
// 宽度固定，百位出现或消失时不会推动后面的字符。
func timeLabel(status voice.TUIVoiceStatus, limitMs int) string {
	// 没有可显示的时间时返回空串，由调用方决定填什么占位符。不能在这里
	// 一律返回 UPLOAD：出错时左栏也走这条路径，而「上传中」和「出错了」
	// 摆在一起是两句话打架。
	if status.StartedAt.IsZero() || status.WaitingForASR {
		return ""
	}
	limit := limitMs
	if limit <= 0 {
		limit = softLimitMs
	}
	if status.VoiceStartedAt.IsZero() {
		// 还没开口：满额静止
		return timePrefix + fmt.Sprintf("%3ds", limit/1000)
	}
	elapsed := time.Since(status.VoiceStartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	remain := time.Duration(limit)*time.Millisecond - elapsed
	if remain < 0 {
		remain = 0
	}
	// 向上取整：开口第一刻显示满额，而不是少一秒
	secs := int((remain + time.Second - 1) / time.Second)
	return timePrefix + fmt.Sprintf("%3ds", secs)
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
	// 文字垂直居中：基线的位置要让「上伸-下伸」的中点落在胶囊中心，
	// 直接用 fontSize/2 会让文字整体偏下 1~2 像素。
	layout.TextBaseline = height/2 + int(m.FontSize*0.36)

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
