package main

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

const sandboxUnavailableExit = 3

func runSandboxChild(t *testing.T, scenario string, env ...string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSandboxChild$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), append(env, "DRIVESYNC_SANDBOX_SCENARIO="+scenario)...)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == sandboxUnavailableExit {
		if os.Getenv("DRIVESYNC_REQUIRE_SANDBOX") != "" {
			t.Fatalf("sandbox unavailable but required:\n%s", out)
		}
		t.Skipf("sandbox unavailable in this test binary (run with CGO_ENABLED=0):\n%s", lastLines(out))
	}
	if err != nil {
		t.Fatalf("sandboxed child failed: %v\n%s", err, out)
	}
}

func lastLines(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, "\n")
}

func enterChildSandbox(t *testing.T, policy sandboxPolicy) {
	if err := applySandbox(policy); err != nil {
		t.Log(err)
		os.Exit(sandboxUnavailableExit)
	}
}

func TestSandboxChild(t *testing.T) {
	switch os.Getenv("DRIVESYNC_SANDBOX_SCENARIO") {
	case "escape":
		sandboxEscapeScenario(t)
	case "sync":
		sandboxSyncScenario(t)
	default:
		t.Skip("only runs as a child of the sandbox tests")
	}
}

func TestSandboxBlocksEscapes(t *testing.T) {
	allowed, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_ed25519"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	runSandboxChild(t, "escape", "SANDBOX_ALLOWED="+allowed, "SANDBOX_OUTSIDE="+outside)
}

func TestSandboxAllowsFullSync(t *testing.T) {
	home := t.TempDir()
	runSandboxChild(t, "sync", "SANDBOX_HOME="+home)
}

func listenPort(t *testing.T) (net.Listener, uint16) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return listener, uint16(listener.Addr().(*net.TCPAddr).Port)
}

func expectDenied(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: allowed, want denied", what)
		return
	}
	if !errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) && !errors.Is(err, syscall.EAFNOSUPPORT) && !errors.Is(err, syscall.ENOSYS) && !errors.Is(err, syscall.EROFS) {
		t.Errorf("%s: failed with %v, want a permission error", what, err)
	}
}

func expectAllowed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
	}
}

func sandboxEscapeScenario(t *testing.T) {
	allowed, outside := os.Getenv("SANDBOX_ALLOWED"), os.Getenv("SANDBOX_OUTSIDE")
	allowedListener, allowedPort := listenPort(t)
	defer allowedListener.Close()
	deniedListener, deniedPort := listenPort(t)
	defer deniedListener.Close()
	enterChildSandbox(t, sandboxPolicy{ReadWrite: []string{allowed}, Read: systemReadPaths(), ConnectPorts: []uint16{allowedPort}})

	inside := filepath.Join(allowed, "file.txt")
	expectAllowed(t, "write inside", os.WriteFile(inside, []byte("x"), 0o644))
	_, err := os.ReadFile(inside)
	expectAllowed(t, "read inside", err)
	expectAllowed(t, "mkdir inside", os.MkdirAll(filepath.Join(allowed, "a", "b"), 0o755))
	expectAllowed(t, "rename across folders inside", os.Rename(inside, filepath.Join(allowed, "a", "b", "moved.txt")))
	expectAllowed(t, "chtimes inside", os.Chtimes(filepath.Join(allowed, "a", "b", "moved.txt"), testTime, testTime))
	expectAllowed(t, "remove inside", os.RemoveAll(filepath.Join(allowed, "a")))
	_, err = os.ReadFile("/etc/hostname")
	if !errors.Is(err, fs.ErrNotExist) {
		expectAllowed(t, "read /etc", err)
	}

	_, err = os.ReadFile(filepath.Join(outside, "id_ed25519"))
	expectDenied(t, "read a secret outside", err)
	expectDenied(t, "write outside", os.WriteFile(filepath.Join(outside, "planted"), []byte("x"), 0o644))
	expectDenied(t, "write /tmp", os.WriteFile(filepath.Join(os.TempDir(), "drivesync-planted"), []byte("x"), 0o644))
	_, err = os.ReadDir(filepath.Dir(outside))
	expectDenied(t, "list the parent folder", err)
	_, err = os.ReadFile("/proc/self/environ")
	expectDenied(t, "read /proc", err)
	if home, err := os.UserHomeDir(); err == nil {
		if _, statErr := os.Stat(home); statErr == nil {
			_, err = os.ReadDir(home)
			expectDenied(t, "list the home folder", err)
		}
	}
	expectDenied(t, "symlink inside", os.Symlink("/etc/passwd", filepath.Join(allowed, "link")))
	expectDenied(t, "write /usr", os.WriteFile("/usr/drivesync-planted", []byte("x"), 0o644))

	expectDenied(t, "exec a program", exec.Command("/usr/bin/true").Run())
	expectDenied(t, "exec a shell", exec.Command("/bin/sh", "-c", "true").Run())

	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(int(allowedPort)))
	expectAllowed(t, "connect to the allowed port", err)
	if conn != nil {
		conn.Close()
	}
	_, err = net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(int(deniedPort)))
	expectDenied(t, "connect to another TCP port", err)
	_, err = net.Listen("tcp", "127.0.0.1:0")
	expectDenied(t, "listen on a TCP port", err)
	_, err = net.Dial("unix", "/run/user/"+strconv.Itoa(os.Getuid())+"/bus")
	expectDenied(t, "connect to the D-Bus session bus", err)
	_, err = syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, 0)
	expectDenied(t, "open a netlink socket", err)
	_, err = syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, 0)
	expectDenied(t, "open a packet socket", err)

	expectDenied(t, "signal the parent", syscall.Kill(os.Getppid(), syscall.Signal(0)))
	expectDenied(t, "ptrace the parent", syscall.PtraceAttach(os.Getppid()))
	expectDenied(t, "unshare a user namespace", syscall.Unshare(syscall.CLONE_NEWUSER))
	expectDenied(t, "unshare a mount namespace", syscall.Unshare(syscall.CLONE_NEWNS))
	name := []byte("x\x00")
	_, _, errno := syscall.Syscall(319, uintptr(unsafe.Pointer(&name[0])), 0, 0)
	expectDenied(t, "memfd_create", errnoErr(errno))
	_, _, errno = syscall.Syscall(sysClone3, 0, 0, 0)
	expectDenied(t, "clone3", errnoErr(errno))
	_, _, errno = syscall.Syscall(425, 1, 0, 0)
	expectDenied(t, "io_uring_setup", errnoErr(errno))
	_, _, errno = syscall.Syscall(321, 0, 0, 0)
	expectDenied(t, "bpf", errnoErr(errno))

	_ = applySandbox(sandboxPolicy{ReadWrite: []string{"/"}})
	_, err = os.ReadFile(filepath.Join(outside, "id_ed25519"))
	expectDenied(t, "read a secret after trying to loosen", err)
}

func errnoErr(errno syscall.Errno) error {
	if errno == 0 {
		return nil
	}
	return errno
}

func sandboxSyncScenario(t *testing.T) {
	home := os.Getenv("SANDBOX_HOME")
	h := newHarnessIn(t, home)
	port, err := strconv.Atoi(h.drive.server.URL[strings.LastIndex(h.drive.server.URL, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	cfgPath, _ := configPath()
	if err := os.MkdirAll(h.root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "outside.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	enterChildSandbox(t, sandboxPolicy{
		ReadWrite:    []string{h.root, filepath.Join(home, ".state", "drivesync")},
		Read:         append(systemReadPaths(), filepath.Dir(cfgPath)),
		ConnectPorts: []uint16{uint16(port)},
	})
	twoWayScenario(t, h)
	_, err = os.ReadFile(filepath.Join(home, "outside.txt"))
	expectDenied(t, "read next to the sync folder", err)
}
