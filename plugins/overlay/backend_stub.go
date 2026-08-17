//go:build (!linux && !darwin && !windows) || (linux && no_x11)

package overlay

import (
	"fmt"

	"github.com/conglinyizhi/just-talk-plus/config"
)

func newBackend(cfg config.OverlayConfig) (backend, error) {
	return nil, fmt.Errorf("overlay backend is not implemented for this platform")
}
