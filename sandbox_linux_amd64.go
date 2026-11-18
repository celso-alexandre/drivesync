//go:build linux && amd64

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
	sysSeccomp               = 317
	sysClone                 = 56
	sysClone3                = 435
	sysSocket                = 41

	oPath = 0x200000

	landlockCreateRulesetVersion = 1
	landlockRulePathBeneath      = 1
	landlockRuleNetPort          = 2
	minimumLandlockABI           = 4

	fsExecute    = 1 << 0
	fsWriteFile  = 1 << 1
	fsReadFile   = 1 << 2
	fsReadDir    = 1 << 3
	fsRemoveDir  = 1 << 4
	fsRemoveFile = 1 << 5
	fsMakeChar   = 1 << 6
	fsMakeDir    = 1 << 7
	fsMakeReg    = 1 << 8
	fsMakeSock   = 1 << 9
	fsMakeFifo   = 1 << 10
	fsMakeBlock  = 1 << 11
	fsMakeSym    = 1 << 12
	fsRefer      = 1 << 13
	fsTruncate   = 1 << 14
	fsIoctlDev   = 1 << 15

	netBindTCP    = 1 << 0
	netConnectTCP = 1 << 1

	scopeAbstractUnixSocket = 1 << 0
	scopeSignal             = 1 << 1

	fileOnlyRights = fsExecute | fsWriteFile | fsReadFile | fsTruncate | fsIoctlDev
	readRights     = fsReadFile | fsReadDir
	writeRights    = readRights | fsWriteFile | fsRemoveDir | fsRemoveFile | fsMakeDir | fsMakeReg | fsRefer | fsTruncate

	prSetNoNewPrivs = 38

	seccompSetModeFilter = 1
	seccompFlagTsync     = 1

	bpfLoadWord = 0x20
	bpfJumpEq   = 0x15
	bpfJumpGe   = 0x35
	bpfJumpSet  = 0x45
	bpfReturn   = 0x06

	auditArchX8664     = 0xc000003e
	x32SyscallBit      = 0x40000000
	seccompRetKill     = 0x80000000
	seccompRetErrno    = 0x00050000
	seccompRetAllow    = 0x7fff0000
	seccompDataNr      = 0
	seccompDataArch    = 4
	seccompDataArgZero = 16

	namespaceCloneFlags = 0x00020000 | 0x02000000 | 0x04000000 | 0x08000000 | 0x10000000 | 0x20000000 | 0x40000000
)

var deniedSyscalls = map[uint32]syscall.Errno{
	59: syscall.EPERM, 322: syscall.EPERM,
	49: syscall.EPERM, 50: syscall.EPERM,
	101: syscall.EPERM, 310: syscall.EPERM, 311: syscall.EPERM,
	165: syscall.EPERM, 166: syscall.EPERM, 155: syscall.EPERM, 161: syscall.EPERM,
	428: syscall.EPERM, 429: syscall.EPERM, 430: syscall.EPERM, 431: syscall.EPERM, 432: syscall.EPERM, 433: syscall.EPERM, 442: syscall.EPERM,
	272: syscall.EPERM, 308: syscall.EPERM, sysClone3: syscall.ENOSYS,
	321: syscall.EPERM, 298: syscall.EPERM, 323: syscall.EPERM,
	248: syscall.EPERM, 249: syscall.EPERM, 250: syscall.EPERM,
	175: syscall.EPERM, 176: syscall.EPERM, 313: syscall.EPERM, 246: syscall.EPERM, 320: syscall.EPERM,
	304: syscall.EPERM, 319: syscall.EPERM, 447: syscall.EPERM,
	425: syscall.EPERM, 426: syscall.EPERM, 427: syscall.EPERM,
	135: syscall.EPERM, 163: syscall.EPERM, 167: syscall.EPERM, 168: syscall.EPERM, 169: syscall.EPERM,
}

type sockFilter struct {
	code uint16
	jt   uint8
	jf   uint8
	k    uint32
}

type sockFprog struct {
	length uint16
	filter *sockFilter
}

type rulesetAttr struct {
	handledFS  uint64
	handledNet uint64
	scoped     uint64
}

type netPortAttr struct {
	allowed uint64
	port    uint64
}

func applySandbox(policy sandboxPolicy) error {
	abi, _, errno := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if errno != 0 {
		return fmt.Errorf("the kernel has no Landlock support (%v)", errno)
	}
	if abi < minimumLandlockABI {
		return fmt.Errorf("Landlock ABI %d is too old, need %d (Linux 6.7 or newer)", abi, minimumLandlockABI)
	}

	handledFS := uint64(fsExecute | fsWriteFile | fsReadFile | fsReadDir | fsRemoveDir | fsRemoveFile | fsMakeChar | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo | fsMakeBlock | fsMakeSym | fsRefer | fsTruncate)
	if abi >= 5 {
		handledFS |= fsIoctlDev
	}
	attr := rulesetAttr{handledFS: handledFS, handledNet: netBindTCP | netConnectTCP}
	size := uintptr(16)
	if abi >= 6 {
		attr.scoped = scopeAbstractUnixSocket
		if !policy.AllowSignals {
			attr.scoped |= scopeSignal
		}
		size = 24
	}
	ruleset, _, errno := syscall.Syscall(sysLandlockCreateRuleset, uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return fmt.Errorf("creating Landlock ruleset: %v", errno)
	}
	defer syscall.Close(int(ruleset))

	for _, path := range policy.ReadWrite {
		if err := addPathRule(ruleset, path, writeRights&handledFS, true); err != nil {
			return err
		}
	}
	for _, path := range policy.Read {
		if err := addPathRule(ruleset, path, readRights&handledFS, false); err != nil {
			return err
		}
	}
	for _, port := range policy.ConnectPorts {
		rule := netPortAttr{allowed: netConnectTCP, port: uint64(port)}
		if _, _, errno := syscall.Syscall6(sysLandlockAddRule, ruleset, landlockRuleNetPort, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
			return fmt.Errorf("allowing TCP port %d: %v", port, errno)
		}
	}

	if _, _, errno := syscall.AllThreadsSyscall(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0); errno != 0 {
		if errno == syscall.ENOTSUP {
			return errors.New("this binary was built with cgo; rebuild it with CGO_ENABLED=0")
		}
		return fmt.Errorf("setting no_new_privs: %v", errno)
	}
	if err := installSeccomp(); err != nil {
		return err
	}
	if _, _, errno := syscall.AllThreadsSyscall(sysLandlockRestrictSelf, ruleset, 0, 0); errno != 0 {
		return fmt.Errorf("enforcing Landlock: %v", errno)
	}
	return nil
}

func addPathRule(ruleset uintptr, path string, rights uint64, required bool) error {
	fd, err := syscall.Open(path, oPath|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) && !required {
			return nil
		}
		return fmt.Errorf("sandbox path %s: %w", path, err)
	}
	defer syscall.Close(fd)
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("sandbox path %s: %w", path, err)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		rights &= fileOnlyRights
	}
	var rule [12]byte
	binary.LittleEndian.PutUint64(rule[0:8], rights)
	binary.LittleEndian.PutUint32(rule[8:12], uint32(fd))
	if _, _, errno := syscall.Syscall6(sysLandlockAddRule, ruleset, landlockRulePathBeneath, uintptr(unsafe.Pointer(&rule[0])), 0, 0, 0); errno != 0 {
		return fmt.Errorf("sandbox path %s: %v", path, errno)
	}
	return nil
}

func statement(code uint16, k uint32) sockFilter {
	return sockFilter{code: code, k: k}
}

func jump(code uint16, k uint32, jt, jf uint8) sockFilter {
	return sockFilter{code: code, jt: jt, jf: jf, k: k}
}

func seccompProgram() []sockFilter {
	program := []sockFilter{
		statement(bpfLoadWord, seccompDataArch),
		jump(bpfJumpEq, auditArchX8664, 1, 0),
		statement(bpfReturn, seccompRetKill),
		statement(bpfLoadWord, seccompDataNr),
		jump(bpfJumpGe, x32SyscallBit, 0, 1),
		statement(bpfReturn, seccompRetErrno|uint32(syscall.EPERM)),
	}
	for nr := uint32(0); nr < 512; nr++ {
		errno, denied := deniedSyscalls[nr]
		if !denied {
			continue
		}
		program = append(program,
			jump(bpfJumpEq, nr, 0, 1),
			statement(bpfReturn, seccompRetErrno|uint32(errno)),
		)
	}
	program = append(program,
		jump(bpfJumpEq, sysClone, 0, 4),
		statement(bpfLoadWord, seccompDataArgZero),
		jump(bpfJumpSet, namespaceCloneFlags, 0, 1),
		statement(bpfReturn, seccompRetErrno|uint32(syscall.EPERM)),
		statement(bpfReturn, seccompRetAllow),
		jump(bpfJumpEq, sysSocket, 0, 4),
		statement(bpfLoadWord, seccompDataArgZero),
		jump(bpfJumpEq, syscall.AF_INET, 2, 0),
		jump(bpfJumpEq, syscall.AF_INET6, 1, 0),
		statement(bpfReturn, seccompRetErrno|uint32(syscall.EAFNOSUPPORT)),
		statement(bpfReturn, seccompRetAllow),
	)
	return program
}

func installSeccomp() error {
	program := seccompProgram()
	prog := sockFprog{length: uint16(len(program)), filter: &program[0]}
	if _, _, errno := syscall.Syscall(sysSeccomp, seccompSetModeFilter, seccompFlagTsync, uintptr(unsafe.Pointer(&prog))); errno != 0 {
		return fmt.Errorf("installing seccomp filter: %v", errno)
	}
	return nil
}
