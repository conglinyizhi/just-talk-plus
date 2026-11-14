//go:build !linux || no_x11

package overlay

// overlay 只在 Linux（X11 / Wayland）实现。删掉 Windows 与 macOS 那两份是刻意的：
// 它们来自上游 just-talk-go，维护者没有对应平台可以验证，留着一个改不动的影子
// 实现比没有更危险。需要时从 git 历史或上游取回，按当时的接口适配。

import (
	"fmt"

	"github.com/conglinyizhi/just-talk-plus/config"
)

func newBackend(cfg config.OverlayConfig) (backend, error) {
	return nil, fmt.Errorf("overlay is only implemented on Linux (X11 / Wayland)")
}
