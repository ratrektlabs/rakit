//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package jsonl

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }
