package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func selftestSandbox(ctx context.Context, args []string) error {
	if len(args) == 3 && args[0] == "--child" {
		return selftestChecks(ctx, args[1], args[2])
	}
	allowed, err := os.MkdirTemp("", "drivesync-selftest-allowed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(allowed)
	outside, err := os.MkdirTemp("", "drivesync-selftest-outside-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outside)
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		return err
	}
	policy := sandboxPolicy{ReadWrite: []string{allowed}, Read: systemReadPaths(), ConnectPorts: internetPorts}
	childArgs := []string{"selftest-sandbox", "--child", allowed, outside}
	if usesLauncher() {
		return launchSandboxed(ctx, childArgs, policy, &credentials{ClientID: "-", ClientSecret: "-", RefreshToken: "-"})
	}
	if err := enterSandbox(policy); err != nil {
		return err
	}
	return selftestChecks(ctx, allowed, outside)
}

func selftestChecks(ctx context.Context, allowed, outside string) error {
	if usesLauncher() {
		if err := verifyConfined(); err != nil {
			return err
		}
	}
	failures := 0
	check := func(name string, wantAllowed bool, err error) {
		status := "ok  "
		if (err == nil) != wantAllowed {
			status = "FAIL"
			failures++
		}
		verdict := "denied"
		if err == nil {
			verdict = "allowed"
		}
		fmt.Printf("%s %-40s %s\n", status, name, verdict)
	}

	file := filepath.Join(allowed, "file.txt")
	check("write in the sync folder", true, os.WriteFile(file, []byte("x"), 0o600))
	_, err := os.ReadFile(file)
	check("read in the sync folder", true, err)
	check("make a folder in the sync folder", true, os.MkdirAll(filepath.Join(allowed, "a", "b"), 0o700))
	check("move across folders", true, os.Rename(file, filepath.Join(allowed, "a", "b", "moved.txt")))
	check("set file times", true, os.Chtimes(filepath.Join(allowed, "a", "b", "moved.txt"), time.Now(), time.Now()))
	check("delete in the sync folder", true, os.RemoveAll(filepath.Join(allowed, "a")))

	_, err = os.ReadFile(filepath.Join(outside, "secret"))
	check("read a file outside", false, err)
	check("write a file outside", false, os.WriteFile(filepath.Join(outside, "planted"), []byte("x"), 0o600))
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		_, err = os.ReadDir(home)
		check("list the home folder", false, err)
	}
	check("start a program", false, exec.Command(probeProgram[0], probeProgram[1:]...).Run())

	client := &http.Client{Timeout: 20 * time.Second}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/discovery/v1/apis?name=drive&preferred=true", nil)
	response, err := client.Do(request)
	if err == nil {
		response.Body.Close()
	}
	check("HTTPS to www.googleapis.com", true, err)
	if runtime.GOOS != "windows" {
		dialer := net.Dialer{Timeout: 10 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", "www.google.com:80")
		if err == nil {
			conn.Close()
		}
		check("plain TCP to port 80", false, err)
	}

	if failures > 0 {
		return fmt.Errorf("%d sandbox checks failed", failures)
	}
	fmt.Println("sandbox self-test passed")
	return nil
}
