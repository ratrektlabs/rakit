//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package jsonl

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }
