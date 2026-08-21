//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package jsonl

import "os"

// Unsupported targets still get process-level serialization. The supported
// Unix and Windows builds use advisory OS locks in their platform files.
func tryPlatformLock(*os.File) (bool, error) { return true, nil }

func unlockPlatformLock(*os.File) error { return nil }
