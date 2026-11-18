//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

var probeProgram = []string{"/usr/bin/true"}

func sandboxString(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`
}

func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func darwinProfile(policy sandboxPolicy, exe string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n(import \"system.sb\")\n")
	fmt.Fprintf(&b, "(allow process-exec (literal %s))\n", sandboxString(exe))
	fmt.Fprintf(&b, "(allow file-read* (literal %s))\n", sandboxString(exe))
	b.WriteString("(allow file-read-metadata)\n(allow sysctl-read)\n(allow system-socket)\n")
	for _, path := range policy.ReadWrite {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sandboxString(realPath(path)))
	}
	for _, path := range policy.Read {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", sandboxString(realPath(path)))
	}
	for _, port := range policy.ConnectPorts {
		fmt.Fprintf(&b, "(allow network-outbound (remote tcp \"*:%d\"))\n", port)
		if port == 53 {
			b.WriteString("(allow network-outbound (remote udp \"*:53\"))\n")
		}
	}
	if len(policy.ConnectPorts) > 0 {
		b.WriteString("(allow network-outbound (literal \"/private/var/run/mDNSResponder\"))\n")
		b.WriteString("(allow mach-lookup (global-name \"com.apple.dnssd.service\") (global-name \"com.apple.trustd\") (global-name \"com.apple.trustd.agent\") (global-name \"com.apple.SecurityServer\") (global-name \"com.apple.system.opendirectoryd.libinfo\") (global-name \"com.apple.ocspd\"))\n")
		b.WriteString("(allow file-read* (subpath \"/private/var/db/mds\") (subpath \"/Library/Keychains\") (subpath \"/System/Library/Keychains\") (subpath \"/private/etc\"))\n")
	}
	return b.String()
}

func launchSandboxed(ctx context.Context, args []string, policy sandboxPolicy, creds *credentials) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = realPath(exe)
	cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", darwinProfile(policy, exe), exe}, args...)...)
	cmd.Env = append(os.Environ(), sandboxChildEnv+"=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the sandboxed worker: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Signal(syscall.SIGTERM) })
	defer stop()
	if err := writeCredentials(stdin, creds); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitCodeError(exitErr.ExitCode())
	}
	return err
}
