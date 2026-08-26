// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// 内嵌 AndroidMic TCP 服务端的 voice 插件侧管理：录音输入指向
// device = "androidmic" 时，从该服务端接收 Android 设备音频。
// 服务端生命周期独立于语音识别流程，内部错误不会带崩主应用。

package voice

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/conglinyizhi/just-talk-plus/internal/androidmic"
)

// androidMicServer 是 voice 插件持有的服务端单例（惰性创建，TUI 与录音共用）。
var androidMicServer = androidmic.New(nil)

// AndroidMicDeviceName 返回 voice 配置中指向内嵌服务端的 device 取值。
func AndroidMicDeviceName() string { return androidmic.DeviceName }

// AndroidMicStart 启动内嵌服务端（幂等）。port <= 0 时使用 AndroidMic 默认端口。
func AndroidMicStart(port int) error {
	if port <= 0 {
		port = androidmic.DefaultPort
	}
	return androidMicServer.Start(port)
}

// AndroidMicStop 停止内嵌服务端（幂等）。
func AndroidMicStop() error { return androidMicServer.Stop() }

// AndroidMicStatus returns an embedded server status snapshot.
func AndroidMicStatus() androidmic.Status { return androidMicServer.Status() }

// AndroidMicDiagnosticReport returns a bounded, text-only diagnostic snapshot.
// It intentionally excludes audio, recognized text, and credentials.
func AndroidMicDiagnosticReport() string {
	st := AndroidMicStatus()
	var b strings.Builder
	b.WriteString("Just Talk Plus AndroidMic diagnostics\n")
	b.WriteString("generated_at: " + time.Now().Format(time.RFC3339) + "\n")
	if st.Running {
		fmt.Fprintf(&b, "server: running (%s)\n", st.Addr)
	} else {
		b.WriteString("server: stopped\n")
	}
	fmt.Fprintf(&b, "recording_consumer: %t\n", st.Active)
	if len(st.Clients) == 0 {
		b.WriteString("connected_clients: none\n")
	} else {
		b.WriteString("connected_clients: " + strings.Join(st.Clients, ", ") + "\n")
	}
	if len(st.RecentSessions) == 0 {
		b.WriteString("\nrecent_sessions: none\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\nrecent_sessions: %d (max 10)\n", len(st.RecentSessions))
	for i, session := range st.RecentSessions {
		fmt.Fprintf(&b, "\n%d. client: %s\n", i+1, session.Client)
		fmt.Fprintf(&b, "   connected_at: %s\n", formatAndroidMicTime(session.ConnectedAt))
		fmt.Fprintf(&b, "   disconnected_at: %s\n", formatAndroidMicTime(session.DisconnectedAt))
		fmt.Fprintf(&b, "   handshake_ok: %t\n", session.HandshakeOK)
		fmt.Fprintf(&b, "   packets_received: %d, pcm_bytes: %d, delivered_frames: %d\n", session.PacketsReceived, session.PCMBytes, session.DeliveredFrames)
		fmt.Fprintf(&b, "   decode_errors: %d, convert_errors: %d\n", session.DecodeErrors, session.ConvertErrors)
		fmt.Fprintf(&b, "   dropped_no_consumer: %d, dropped_queue_full: %d\n", session.DroppedNoConsumer, session.DroppedQueueFull)
		fmt.Fprintf(&b, "   last_packet_at: %s\n", formatAndroidMicTime(session.LastPacketAt))
	}
	return b.String()
}

func formatAndroidMicTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

// openAndroidMicSource 为一次录音会话挂接音频源；服务端未运行时返回错误，
// 录音会据此提示用户先启动服务端。
func openAndroidMicSource(logger *slog.Logger) (io.ReadCloser, func() error, error) {
	src, err := androidMicServer.OpenSource()
	if err != nil {
		return nil, nil, err
	}
	return src, func() error { return src.Close() }, nil
}

// isAndroidMicDevice 判断 device 是否指向内嵌 AndroidMic 服务端。
func isAndroidMicDevice(device string) bool { return device == androidmic.DeviceName }

// maybeAutoStartAndroidMic 在配置允许自动启用（androidmic_auto_start = true）
// 时，插件启动即启动内嵌服务端，保证切到 androidmic 设备即可用；
// 默认关闭，避免新用户不知情时被默默占用端口；失败只记日志，不阻塞启动。
func (p *VoicePlugin) maybeAutoStartAndroidMic() {
	vc := p.cfg.Load().Voice
	if !vc.AndroidMicAutoStart {
		return
	}
	if err := AndroidMicStart(vc.AndroidMicPort); err != nil {
		p.logger.Error("androidmic: 服务端自动启动失败", "error", err)
	}
}
