//go:build !(linux && amd64)

package main

import "errors"

func applySandbox(sandboxPolicy) error {
	return errors.New("the sandbox is only implemented for Linux on x86-64")
}
