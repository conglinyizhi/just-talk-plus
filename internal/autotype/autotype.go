package autotype

import (
	"log/slog"
	"time"
)

// pasteSettleDelay 模拟粘贴成功发送后等待目标应用读取剪贴板的时间。
// 过早返回会让调用方立刻恢复/回收剪贴板，应用实际粘到的是被还原的旧内容。
// 三平台共用同一常量，保证 X11/Wayland/Windows/macOS 的粘贴后保护窗口一致。
const pasteSettleDelay = 800 * time.Millisecond

// Paste inserts text into the currently focused input field.
func Paste(text string, logger *slog.Logger) error {
	return PasteAndFinalize(text, logger, nil)
}

// PasteAndFinalize invokes finalize before platform-specific temporary clipboard
// ownership is released. pasted reports whether the synthetic paste input was
// sent; on success finalize runs after the settle window. finalize must not log
// clipboard contents.
func PasteAndFinalize(text string, logger *slog.Logger, finalize func(pasted bool) error) error {
	return pastePlatform(text, logger, finalize)
}
