//go:build darwin && cgo

package clipboard

// #cgo LDFLAGS: -framework AppKit -framework Foundation
// #include <stdlib.h>
// #include "clipboard_darwin.h"
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

func newPlatformClipboard() (*Clipboard, error) {
	return &Clipboard{
		getFunc:      darwinGet,
		setFunc:      darwinSet,
		snapshotFunc: darwinSnapshot,
		restoreFunc:  darwinRestoreIfUnchanged,
	}, nil
}

func darwinSet(text string) error {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	if C.jt_clipboard_set(cText) != 0 {
		return fmt.Errorf("NSPasteboard set failed")
	}
	return nil
}

func darwinGet() (string, error) {
	cText := C.jt_clipboard_get()
	if cText == nil {
		return "", fmt.Errorf("NSPasteboard get failed")
	}
	defer C.free(unsafe.Pointer(cText))
	return C.GoString(cText), nil
}

func darwinSnapshot() (Snapshot, error) {
	if C.jt_clipboard_item_count() != 1 {
		return Snapshot{Reason: "multiple_items_or_empty"}, nil
	}
	cTypes := C.jt_clipboard_types()
	if cTypes == nil {
		return Snapshot{Reason: "type_list_failed"}, nil
	}
	defer C.free(unsafe.Pointer(cTypes))

	formatTypes, reason := darwinSupportedTypes(strings.Fields(C.GoString(cTypes)))
	if reason != "" {
		return Snapshot{Reason: reason}, nil
	}
	formats := make([]FormatData, 0, len(formatTypes))
	for format, pasteboardType := range formatTypes {
		data, err := darwinReadData(pasteboardType)
		if err != nil {
			return Snapshot{Reason: "snapshot_read_failed"}, err
		}
		formats = append(formats, FormatData{Format: format, Data: data})
	}
	var total int
	for _, format := range formats {
		total += len(format.Data)
	}
	if total > maxSnapshotBytes {
		return Snapshot{Reason: "snapshot_too_large"}, nil
	}
	return Snapshot{Formats: formats, Restorable: true}, nil
}

func darwinRestoreIfUnchanged(snapshot Snapshot, temporaryText string) (RestoreResult, error) {
	current, err := darwinGet()
	if err != nil || current != temporaryText {
		return RestoreResult{Reason: "clipboard_changed"}, nil
	}
	if C.jt_clipboard_clear() != 0 {
		return RestoreResult{Reason: "restore_write_failed"}, fmt.Errorf("NSPasteboard clear failed")
	}
	for _, format := range snapshot.Formats {
		pasteboardType, ok := darwinTypeForFormat(format.Format)
		if !ok {
			return RestoreResult{Reason: "unsupported_format"}, nil
		}
		if err := darwinSetData(pasteboardType, format.Data); err != nil {
			return RestoreResult{Reason: "restore_write_failed"}, err
		}
	}
	return RestoreResult{Restored: true}, nil
}

func darwinSupportedTypes(types []string) (map[Format]string, string) {
	formats := make(map[Format]string)
	for _, pasteboardType := range types {
		format, ok := darwinFormatForType(pasteboardType)
		if !ok {
			return nil, "unsupported_format"
		}
		if existing, exists := formats[format]; exists && existing != pasteboardType {
			return nil, "multiple_formats"
		}
		formats[format] = pasteboardType
	}
	if len(formats) == 0 {
		return nil, "empty_or_unknown_clipboard"
	}
	return formats, ""
}

func darwinFormatForType(pasteboardType string) (Format, bool) {
	switch strings.ToLower(pasteboardType) {
	case "public.utf8-plain-text", "public.text", "com.apple.traditional-mac-plain-text":
		return FormatText, true
	case "public.html":
		return FormatHTML, true
	case "public.png":
		return FormatPNG, true
	default:
		return "", false
	}
}

func darwinTypeForFormat(format Format) (string, bool) {
	switch format {
	case FormatText:
		return "public.utf8-plain-text", true
	case FormatHTML:
		return "public.html", true
	case FormatPNG:
		return "public.png", true
	default:
		return "", false
	}
}

func darwinReadData(pasteboardType string) ([]byte, error) {
	cType := C.CString(pasteboardType)
	defer C.free(unsafe.Pointer(cType))
	var data unsafe.Pointer
	var length C.size_t
	if C.jt_clipboard_read_data(cType, &data, &length) != 0 {
		return nil, fmt.Errorf("NSPasteboard read %s failed", pasteboardType)
	}
	defer C.free(data)
	if uint64(length) > maxSnapshotBytes {
		return nil, fmt.Errorf("clipboard data exceeds snapshot limit")
	}
	return append([]byte(nil), unsafe.Slice((*byte)(data), int(length))...), nil
}

func darwinSetData(pasteboardType string, data []byte) error {
	cType := C.CString(pasteboardType)
	defer C.free(unsafe.Pointer(cType))
	var ptr unsafe.Pointer
	if len(data) > 0 {
		ptr = unsafe.Pointer(&data[0])
	}
	if C.jt_clipboard_set_data(cType, ptr, C.size_t(len(data))) != 0 {
		return fmt.Errorf("NSPasteboard write %s failed", pasteboardType)
	}
	return nil
}
