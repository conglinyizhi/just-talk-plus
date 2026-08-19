// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// 内嵌 AndroidMic TCP 服务端的 voice 插件侧管理：录音输入指向
// device = "androidmic" 时，从该服务端接收 Android 设备音频。
// 服务端生命周期独立于语音识别流程，内部错误不会带崩主应用。

package voice

import (
	"io"
	"log/slog"

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

// AndroidMicStatus 返回内嵌服务端状态快照。
func AndroidMicStatus() androidmic.Status { return androidMicServer.Status() }

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

// maybeAutoStartAndroidMic 在配置指向 Android 设备时自动启动服务端，
// 保证 TUI / daemon 模式都能直接用；失败只记日志，不阻塞启动。
func (p *VoicePlugin) maybeAutoStartAndroidMic() {
	vc := p.cfg.Load().Voice
	if !isAndroidMicDevice(vc.Device) {
		return
	}
	if err := AndroidMicStart(vc.AndroidMicPort); err != nil {
		p.logger.Error("androidmic: 服务端自动启动失败", "error", err)
	}
}
