// Package clipboard provides cross-platform clipboard operations.
package clipboard

import (
	"context"
	"time"
)

const maxSnapshotBytes = 16 << 20

// Format identifies a clipboard representation that can be safely restored.
type Format string

const (
	FormatText Format = "text/plain"
	FormatHTML Format = "text/html"
	FormatPNG  Format = "image/png"
)

// FormatData is one clipboard representation. Data is never logged.
type FormatData struct {
	Format Format
	Data   []byte
}

// Snapshot holds the supported clipboard representations captured before
// autotype temporarily takes ownership of the clipboard. A non-restorable
// snapshot is an intentional safe outcome for unsupported clipboard content.
type Snapshot struct {
	Formats    []FormatData
	Restorable bool
	Reason     string
}

// ByteLen reports the total snapshot payload size without exposing its data.
func (s Snapshot) ByteLen() int {
	var n int
	for _, format := range s.Formats {
		n += len(format.Data)
	}
	return n
}

// FormatNames reports the snapshot MIME names without exposing its data.
func (s Snapshot) FormatNames() []string {
	names := make([]string, 0, len(s.Formats))
	for _, format := range s.Formats {
		names = append(names, string(format.Format))
	}
	return names
}

// RestoreResult records whether a restorable snapshot was restored or safely
// skipped. A skip never changes the current clipboard.
type RestoreResult struct {
	Restored bool
	Reason   string
}

// Clipboard provides text operations plus safe clipboard snapshot support.
type Clipboard struct {
	getCmd         []string
	setCmd         []string
	setPrimaryCmd  []string // X11 primary selection, nil if unsupported
	getFunc        func() (string, error)
	setFunc        func(string) error
	setPrimaryFunc func(string) error
	snapshotFunc   func() (Snapshot, error)
	restoreFunc    func(Snapshot, string) (RestoreResult, error)
}

// New creates a platform-specific Clipboard.
func New() (*Clipboard, error) {
	return newPlatformClipboard()
}

// Get reads the current clipboard text content.
func (c *Clipboard) Get() (string, error) {
	if c.getFunc != nil {
		return c.getFunc()
	}
	return runCmd(c.getCmd)
}

// Set writes text to the clipboard.
func (c *Clipboard) Set(text string) error {
	if c.setFunc != nil {
		return c.setFunc(text)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := runCmdWithInput(ctx, c.setCmd, text)
	return err
}

// SetPrimary writes text to the X11 primary selection (if supported).
func (c *Clipboard) SetPrimary(text string) error {
	if c.setPrimaryFunc != nil {
		return c.setPrimaryFunc(text)
	}
	if c.setPrimaryCmd == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := runCmdWithInput(ctx, c.setPrimaryCmd, text)
	return err
}

// Snapshot captures a restorable supported clipboard value. Unsupported,
// multi-format, or oversized content returns a non-restorable snapshot and no
// error so callers can safely skip restoration without damaging user data.
func (c *Clipboard) Snapshot() (Snapshot, error) {
	if c.snapshotFunc == nil {
		return Snapshot{Reason: "snapshot_not_supported"}, nil
	}
	return c.snapshotFunc()
}

// RestoreIfUnchanged restores snapshot only when the clipboard still contains
// temporaryText written for this autotype operation. If another application or
// the user has changed the clipboard, restoration is skipped.
func (c *Clipboard) RestoreIfUnchanged(snapshot Snapshot, temporaryText string) (RestoreResult, error) {
	if !snapshot.Restorable {
		return RestoreResult{Reason: snapshot.Reason}, nil
	}
	if c.restoreFunc == nil {
		return RestoreResult{Reason: "restore_not_supported"}, nil
	}
	return c.restoreFunc(snapshot, temporaryText)
}

type clipCmd struct {
	get        []string
	set        []string
	setPrimary []string
}

// NewFromCmd creates a Clipboard from explicit commands (for testing).
func newFromCmd(c clipCmd) *Clipboard {
	return &Clipboard{getCmd: c.get, setCmd: c.set, setPrimaryCmd: c.setPrimary}
}
