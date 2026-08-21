//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package jsonl

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryPlatformLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return false, err
}

func unlockPlatformLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
