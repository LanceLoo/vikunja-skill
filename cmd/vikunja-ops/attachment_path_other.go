//go:build !windows

package main

import (
	"io/fs"
	"os"
)

// unsafeWindowsPath is a no-op on non-Windows platforms.
func unsafeWindowsPath(string) bool { return false }

// pathComponentIsLink reports whether a path component is a symlink.
// Errors are treated as unsafe.
func pathComponentIsLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return true
	}
	return info.Mode()&fs.ModeSymlink != 0
}
