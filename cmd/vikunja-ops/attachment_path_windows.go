//go:build windows

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// fileAttributeReparsePoint is FILE_ATTRIBUTE_REPARSE_POINT.
const fileAttributeReparsePoint = 0x400

// isReparsePoint reports whether path carries the reparse point attribute
// (symlink, junction, or any other reparse point) according to
// GetFileAttributesEx. Errors are treated as unsafe.
func isReparsePoint(path string) bool {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	var data syscall.Win32FileAttributeData
	if err := syscall.GetFileAttributesEx(pathPtr, syscall.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&data))); err != nil {
		return true
	}
	return data.FileAttributes&fileAttributeReparsePoint != 0
}

// pathComponentIsLink reports whether a path component is any kind of link:
// a symlink per Lstat or any reparse point (which includes directory
// junctions that Lstat does not report as symlinks). Errors are unsafe.
func pathComponentIsLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return true
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return true
	}
	return isReparsePoint(path)
}

// unsafeWindowsPath rejects Windows-specific unsafe path forms feasible to
// detect with the standard library: both slash forms of UNC and device
// namespaces, alternate data stream colons beyond a legitimate volume
// prefix, final names ending in a dot or space, reserved device names
// normalized for trailing dots/spaces, and control characters in the final
// component.
func unsafeWindowsPath(raw string) bool {
	// Reject both slash forms of UNC paths and device namespaces
	// (\\server\share, //server/share, \\?\, \\.\, //?/, //./).
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		return true
	}
	// Any colon beyond a legitimate volume prefix (e.g. "C:") denotes an
	// alternate data stream or another unsafe form.
	volume := filepath.VolumeName(raw)
	rest := raw[len(volume):]
	// C:foo is relative to the process current directory on drive C, not an
	// absolute destination. Reject it rather than validating a different path.
	if len(volume) == 2 && volume[1] == ':' && rest != "" && rest[0] != '\\' && rest[0] != '/' {
		return true
	}
	if strings.ContainsRune(rest, ':') {
		return true
	}
	for _, component := range strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' }) {
		if componentUnsafeOnWindows(component) {
			return true
		}
	}
	return false
}

func componentUnsafeOnWindows(component string) bool {
	for _, r := range component {
		if r < 0x20 {
			return true
		}
	}
	// Windows strips trailing dots and spaces from every path component, so
	// accepting them would make the validated path differ from the opened one.
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return true
	}
	name := component
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	name = strings.TrimRight(name, ". ")
	switch strings.ToUpper(name) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}
