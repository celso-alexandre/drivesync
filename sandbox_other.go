//go:build linux && !amd64

package main

import "errors"

func applySandbox(sandboxPolicy) error {
	return errors.New("the Linux sandbox is only implemented for x86-64")
}
