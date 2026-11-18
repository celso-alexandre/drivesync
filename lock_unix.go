//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"syscall"
)

func lockFile(path string) (io.Closer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another drivesync sync is already running")
	}
	return file, nil
}
