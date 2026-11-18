//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"syscall"
)

func lockFile(path string) (io.Closer, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, syscall.Errno(32)) {
			return nil, errors.New("another drivesync sync is already running")
		}
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
