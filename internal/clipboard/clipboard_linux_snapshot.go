//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func linuxSnapshot() (Snapshot, error) {
	if isWayland() {
		return waylandSnapshot()
	}
	return x11Snapshot()
}

func linuxRestoreIfUnchanged(snapshot Snapshot, temporaryText string) (RestoreResult, error) {
	if isWayland() {
		return waylandRestoreIfUnchanged(snapshot, temporaryText)
	}
	return x11RestoreIfUnchanged(snapshot, temporaryText)
}

func isWayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"
}

func waylandSnapshot() (Snapshot, error) {
	types, err := commandLines("wl-paste", "--list-types")
	if err != nil {
		return Snapshot{Reason: "type_list_failed"}, err
	}
	format, mime, reason := oneSupportedFormat(types)
	if reason != "" {
		return Snapshot{Reason: reason}, nil
	}
	data, err := commandBytes("wl-paste", "--type", mime)
	if err != nil {
		return Snapshot{Reason: "snapshot_read_failed"}, err
	}
	return snapshotFromData(format, data)
}

func waylandRestoreIfUnchanged(snapshot Snapshot, temporaryText string) (RestoreResult, error) {
	if !waylandStillTemporary(temporaryText) {
		return RestoreResult{Reason: "clipboard_changed"}, nil
	}
	format := snapshot.Formats[0]
	if err := commandInput(format.Data, "wl-copy", "--type", string(format.Format)); err != nil {
		return RestoreResult{Reason: "restore_write_failed"}, err
	}
	return RestoreResult{Restored: true}, nil
}

func waylandStillTemporary(temporaryText string) bool {
	types, err := commandLines("wl-paste", "--list-types")
	if err != nil {
		return false
	}
	_, mime, reason := oneSupportedFormat(types)
	if reason != "" {
		return false
	}
	data, err := commandBytes("wl-paste", "--type", mime)
	return err == nil && temporaryTextMatches(types, data, temporaryText)
}

func x11Snapshot() (Snapshot, error) {
	types, err := commandLines("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	if err != nil {
		return Snapshot{Reason: "type_list_failed"}, err
	}
	format, mime, reason := oneSupportedFormat(types)
	if reason != "" {
		return Snapshot{Reason: reason}, nil
	}
	data, err := commandBytes("xclip", "-selection", "clipboard", "-t", mime, "-o")
	if err != nil {
		return Snapshot{Reason: "snapshot_read_failed"}, err
	}
	return snapshotFromData(format, data)
}

func x11RestoreIfUnchanged(snapshot Snapshot, temporaryText string) (RestoreResult, error) {
	types, err := commandLines("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	if err != nil {
		return RestoreResult{Reason: "clipboard_changed"}, nil
	}
	_, mime, reason := oneSupportedFormat(types)
	if reason != "" {
		return RestoreResult{Reason: "clipboard_changed"}, nil
	}
	data, err := commandBytes("xclip", "-selection", "clipboard", "-t", mime, "-o")
	if err != nil || !temporaryTextMatches(types, data, temporaryText) {
		return RestoreResult{Reason: "clipboard_changed"}, nil
	}
	format := snapshot.Formats[0]
	if err := commandInput(format.Data, "xclip", "-selection", "clipboard", "-t", string(format.Format), "-i"); err != nil {
		return RestoreResult{Reason: "restore_write_failed"}, err
	}
	return RestoreResult{Restored: true}, nil
}

func snapshotFromData(format Format, data []byte) (Snapshot, error) {
	if len(data) > maxSnapshotBytes {
		return Snapshot{Reason: "snapshot_too_large"}, nil
	}
	return Snapshot{Formats: []FormatData{{Format: format, Data: data}}, Restorable: true}, nil
}

func oneSupportedFormat(types []string) (Format, string, string) {
	var format Format
	var selected string
	for _, mime := range types {
		mime = strings.TrimSpace(mime)
		if mime == "" || isMetadataMime(mime) {
			continue
		}
		candidate, ok := canonicalFormat(mime)
		if !ok {
			return "", "", "unsupported_format"
		}
		if format != "" && candidate != format {
			return "", "", "multiple_formats"
		}
		if format == "" || preferredMime(candidate, mime, selected) {
			format, selected = candidate, mime
		}
	}
	if format == "" {
		return "", "", "empty_or_unknown_clipboard"
	}
	return format, selected, ""
}

func canonicalFormat(mime string) (Format, bool) {
	if isTextMime(mime) {
		return FormatText, true
	}
	switch strings.ToLower(mime) {
	case "text/html":
		return FormatHTML, true
	case "image/png":
		return FormatPNG, true
	default:
		return "", false
	}
}

func temporaryTextMatches(types []string, data []byte, temporaryText string) bool {
	format, _, reason := oneSupportedFormat(types)
	return reason == "" && format == FormatText && bytes.Equal(data, []byte(temporaryText))
}

func isTextMime(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "text/plain", "text/plain;charset=utf-8", "text/plain;charset=utf8", "text", "string", "utf8_string":
		return true
	default:
		return false
	}
}

func preferredMime(format Format, candidate, current string) bool {
	if format != FormatText {
		return false
	}
	return strings.EqualFold(candidate, "text/plain;charset=utf-8") || current == ""
}

func isMetadataMime(mime string) bool {
	switch strings.ToUpper(mime) {
	case "TARGETS", "TIMESTAMP", "MULTIPLE", "SAVE_TARGETS", "INCR":
		return true
	default:
		return false
	}
}

func commandLines(name string, args ...string) ([]string, error) {
	data, err := commandBytes(name, args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(data)), nil
}

func commandBytes(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	data, err := exec.CommandContext(ctx, name, args...).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return data, nil
}

func commandInput(data []byte, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
