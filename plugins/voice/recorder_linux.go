//go:build linux

package voice

import (
	"fmt"
	"os/exec"
	"strings"
)

// arecordBase ALSA arecord 基础参数：16 kHz 16-bit 单声道 PCM 原始输出。
func arecordBase() []string {
	return []string{"-r", "16000", "-f", "S16_LE", "-c", "1", "-t", "raw"}
}

// parecordBase PipeWire/PulseAudio parecord 基础参数。
func parecordBase() []string {
	return []string{"--raw", "--rate=16000", "--format=s16le", "--channels=1"}
}

func pickCommand() (*exec.Cmd, string, error) {
	return pickCommandWithDevice("")
}

// pickCommandWithDevice 选择录音命令：
//   - 指定设备：PipeWire/PulseAudio 源名 → parecord；否则视为 ALSA 设备 → arecord -D
//   - 默认设备：优先系统音频服务插件（pipewire/pulse），再回退 ALSA 默认设备
func pickCommandWithDevice(device string) (*exec.Cmd, string, error) {
	if device != "" {
		if isPulseSource(device) {
			return firstFound([]candidate{
				{"parecord", append(parecordBase(), "-d", device, "-")},
				{"arecord", append(arecordBase(), "-D", "pipewire", "-")},
				{"arecord", append(arecordBase(), "-D", "pulse", "-")},
			})
		}
		return firstFound([]candidate{
			{"arecord", append(arecordBase(), "-D", device, "-")},
			{"parecord", append(parecordBase(), "-d", device, "-")},
		})
	}
	return firstFound([]candidate{
		{"arecord", append(arecordBase(), "-D", "pipewire", "-")},
		{"arecord", append(arecordBase(), "-D", "pulse", "-")},
		{"arecord", append(arecordBase(), "-")},
		{"parecord", append(parecordBase(), "-")},
	})
}

func errNoBackend(candidates []candidate) error {
	return fmt.Errorf("未找到可用的录音工具（arecord/parecord）；请安装 alsa-utils（ALSA）或 pipewire（PipeWire）")
}

// isPulseSource 判断设备名是否为 PipeWire/PulseAudio 源名（而非 ALSA hw/plughw）。
func isPulseSource(device string) bool {
	return strings.HasPrefix(device, "alsa_input.") || strings.HasPrefix(device, "input.")
}

// ListDevices 返回可用录音设备：优先系统音频服务（PipeWire/PulseAudio）源，
// 其次 ALSA 硬件卡；报错区分「没装工具」与「没有设备」两种情况。
func ListDevices() ([]string, error) {
	if devices, ok := listPulseSources(); ok {
		return devices, nil
	}
	if devices, ok := listALSAHardware(); ok {
		return devices, nil
	}
	if !hasRecordingTool() {
		return nil, fmt.Errorf("未找到录音工具（pactl/arecord）；请安装 pipewire-pulse 或 alsa-utils")
	}
	return nil, fmt.Errorf("未检测到录音设备：请检查麦克风连接、audio 权限与系统音频服务（PipeWire/PulseAudio）")
}

// listPulseSources 通过 pactl 列出输入源（排除输出设备的 monitor 回声源）。
func listPulseSources() ([]string, bool) {
	path, err := exec.LookPath("pactl")
	if err != nil {
		return nil, false
	}
	out, err := exec.Command(path, "list", "sources", "short").Output()
	if err != nil {
		return nil, false
	}
	var devices []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		if strings.Contains(name, ".monitor") {
			continue
		}
		devices = append(devices, name)
	}
	return devices, len(devices) > 0
}

// listALSAHardware 通过 arecord -l 列出硬件录音卡，返回 plughw:N 设备名。
func listALSAHardware() ([]string, bool) {
	path, err := exec.LookPath("arecord")
	if err != nil {
		return nil, false
	}
	out, err := exec.Command(path, "-l").Output()
	if err != nil {
		return nil, false
	}
	return parseALSAHardwareCards(string(out)), true
}

// parseALSAHardwareCards 解析 `arecord -l` 输出中的 "card N: ..." 行。
func parseALSAHardwareCards(output string) (devices []string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "card ") {
			continue
		}
		var card int
		if _, err := fmt.Sscanf(line, "card %d:", &card); err != nil {
			continue
		}
		devices = append(devices, fmt.Sprintf("plughw:%d", card))
	}
	return devices
}

// hasRecordingTool 是否存在任一可用的录音/探测工具。
func hasRecordingTool() bool {
	for _, name := range []string{"arecord", "parecord", "pactl"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}
