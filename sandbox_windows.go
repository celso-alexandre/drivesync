//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

var probeProgram = []string{`C:\Windows\System32\cmd.exe`, "/c", "exit", "0"}

var (
	userenv                               = syscall.NewLazyDLL("userenv.dll")
	procCreateAppContainerProfile         = userenv.NewProc("CreateAppContainerProfile")
	procDeriveAppContainerSid             = userenv.NewProc("DeriveAppContainerSidFromAppContainerName")
	kernel32                              = syscall.NewLazyDLL("kernel32.dll")
	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateJobObject                   = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject           = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject          = kernel32.NewProc("AssignProcessToJobObject")
	procResumeThread                      = kernel32.NewProc("ResumeThread")
	procSetHandleInformation              = kernel32.NewProc("SetHandleInformation")
	procGetNamedSecurityInfo              = advapi32.NewProc("GetNamedSecurityInfoW")
	procSetNamedSecurityInfo              = advapi32.NewProc("SetNamedSecurityInfoW")
	procSetEntriesInAcl                   = advapi32.NewProc("SetEntriesInAclW")
)

const (
	appContainerName = "drivesync.worker"

	hresultAlreadyExists = 0x800700B7

	seFileObject            = 1
	daclSecurityInformation = 4
	grantAccess             = 1
	inheritToChildren       = 3
	noInheritance           = 0
	trusteeIsSid            = 0
	trusteeIsUnknown        = 0

	fileAllAccess  = 0x1F01FF
	fileReadAccess = 0x1200A9

	internetClientSid = "S-1-15-3-1"
	seGroupEnabled    = 4

	procThreadAttributeSecurityCapabilities = 0x00020009
	extendedStartupInfoPresent              = 0x00080000
	createSuspended                         = 0x00000004
	createUnicodeEnvironment                = 0x00000400
	startfUseStdHandles                     = 0x00000100
	handleFlagInherit                       = 0x00000001

	jobObjectExtendedLimitInformation = 9
	jobLimitActiveProcess             = 0x00000008
	jobLimitDieOnUnhandledException   = 0x00000400
	jobLimitKillOnJobClose            = 0x00002000
)

type trustee struct {
	MultipleTrustee          uintptr
	MultipleTrusteeOperation uint32
	TrusteeForm              uint32
	TrusteeType              uint32
	Name                     uintptr
}

type explicitAccess struct {
	AccessPermissions uint32
	AccessMode        uint32
	Inheritance       uint32
	Trustee           trustee
}

type sidAndAttributes struct {
	Sid        *syscall.SID
	Attributes uint32
}

type securityCapabilities struct {
	AppContainerSid *syscall.SID
	Capabilities    *sidAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

type startupInfoEx struct {
	syscall.StartupInfo
	AttributeList *byte
}

type jobBasicLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtendedLimits struct {
	Basic                 jobBasicLimits
	IoCounters            [6]uint64
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

func appContainerSid() (*syscall.SID, error) {
	name, _ := syscall.UTF16PtrFromString(appContainerName)
	display, _ := syscall.UTF16PtrFromString("drivesync sync worker")
	var sid *syscall.SID
	hr, _, _ := procCreateAppContainerProfile.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(display)), uintptr(unsafe.Pointer(display)), 0, 0, uintptr(unsafe.Pointer(&sid)))
	if hr == 0 {
		return sid, nil
	}
	if uint32(hr) != hresultAlreadyExists {
		return nil, fmt.Errorf("creating the AppContainer profile: HRESULT %#x", uint32(hr))
	}
	hr, _, _ = procDeriveAppContainerSid.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&sid)))
	if hr != 0 {
		return nil, fmt.Errorf("finding the AppContainer: HRESULT %#x", uint32(hr))
	}
	return sid, nil
}

func grantPath(path string, sid *syscall.SID, mask, inheritance uint32) error {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var oldACL, descriptor uintptr
	if ret, _, _ := procGetNamedSecurityInfo.Call(uintptr(unsafe.Pointer(name)), seFileObject, daclSecurityInformation, 0, 0, uintptr(unsafe.Pointer(&oldACL)), 0, uintptr(unsafe.Pointer(&descriptor))); ret != 0 {
		return fmt.Errorf("reading permissions of %s: %w", path, syscall.Errno(ret))
	}
	defer syscall.LocalFree(syscall.Handle(descriptor))
	access := explicitAccess{
		AccessPermissions: mask,
		AccessMode:        grantAccess,
		Inheritance:       inheritance,
		Trustee:           trustee{TrusteeForm: trusteeIsSid, TrusteeType: trusteeIsUnknown, Name: uintptr(unsafe.Pointer(sid))},
	}
	var newACL uintptr
	if ret, _, _ := procSetEntriesInAcl.Call(1, uintptr(unsafe.Pointer(&access)), oldACL, uintptr(unsafe.Pointer(&newACL))); ret != 0 {
		return fmt.Errorf("building permissions for %s: %w", path, syscall.Errno(ret))
	}
	defer syscall.LocalFree(syscall.Handle(newACL))
	if ret, _, _ := procSetNamedSecurityInfo.Call(uintptr(unsafe.Pointer(name)), seFileObject, daclSecurityInformation, 0, 0, newACL, 0); ret != 0 {
		return fmt.Errorf("granting the sandbox access to %s: %w", path, syscall.Errno(ret))
	}
	return nil
}

func inheritableHandle(handle syscall.Handle) (syscall.Handle, error) {
	if handle == 0 || handle == syscall.InvalidHandle {
		return 0, nil
	}
	process, _ := syscall.GetCurrentProcess()
	var duplicate syscall.Handle
	if err := syscall.DuplicateHandle(process, handle, process, &duplicate, 0, true, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, err
	}
	return duplicate, nil
}

func environmentBlock(extra string) *uint16 {
	env := append(os.Environ(), extra)
	sort.Strings(env)
	var block []uint16
	for _, entry := range env {
		if strings.HasPrefix(entry, sandboxChildEnv+"=") && entry != extra {
			continue
		}
		block = append(block, syscall.StringToUTF16(entry)...)
	}
	block = append(block, 0)
	return &block[0]
}

func launchSandboxed(ctx context.Context, args []string, policy sandboxPolicy, creds *credentials) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	sid, err := appContainerSid()
	if err != nil {
		return err
	}
	if err := grantPath(exe, sid, fileReadAccess, noInheritance); err != nil {
		return err
	}
	for _, path := range policy.ReadWrite {
		if err := grantPath(path, sid, fileAllAccess, inheritToChildren); err != nil {
			return err
		}
	}
	for _, path := range policy.Read {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := grantPath(path, sid, fileReadAccess, inheritToChildren); err != nil {
			return err
		}
	}

	var capabilities []sidAndAttributes
	if len(policy.ConnectPorts) > 0 {
		internet, err := syscall.StringToSid(internetClientSid)
		if err != nil {
			return err
		}
		capabilities = append(capabilities, sidAndAttributes{Sid: internet, Attributes: seGroupEnabled})
	}
	caps := securityCapabilities{AppContainerSid: sid, CapabilityCount: uint32(len(capabilities))}
	if len(capabilities) > 0 {
		caps.Capabilities = &capabilities[0]
	}

	var size uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	attributes := make([]byte, size)
	if ret, _, callErr := procInitializeProcThreadAttributeList.Call(uintptr(unsafe.Pointer(&attributes[0])), 1, 0, uintptr(unsafe.Pointer(&size))); ret == 0 {
		return fmt.Errorf("preparing the sandbox: %w", callErr)
	}
	defer procDeleteProcThreadAttributeList.Call(uintptr(unsafe.Pointer(&attributes[0])))
	if ret, _, callErr := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(&attributes[0])), 0, procThreadAttributeSecurityCapabilities, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps), 0, 0); ret == 0 {
		return fmt.Errorf("preparing the sandbox: %w", callErr)
	}

	var stdinRead, stdinWrite syscall.Handle
	inherit := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), InheritHandle: 1}
	if err := syscall.CreatePipe(&stdinRead, &stdinWrite, &inherit, 0); err != nil {
		return err
	}
	defer syscall.CloseHandle(stdinRead)
	procSetHandleInformation.Call(uintptr(stdinWrite), handleFlagInherit, 0)
	stdout, err := inheritableHandle(syscall.Handle(os.Stdout.Fd()))
	if err != nil {
		return err
	}
	stderr, err := inheritableHandle(syscall.Handle(os.Stderr.Fd()))
	if err != nil {
		return err
	}
	defer func() {
		if stdout != 0 {
			syscall.CloseHandle(stdout)
		}
		if stderr != 0 {
			syscall.CloseHandle(stderr)
		}
	}()

	job, _, callErr := procCreateJobObject.Call(0, 0)
	if job == 0 {
		return fmt.Errorf("creating the job object: %w", callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	limits := jobExtendedLimits{Basic: jobBasicLimits{LimitFlags: jobLimitActiveProcess | jobLimitDieOnUnhandledException | jobLimitKillOnJobClose, ActiveProcessLimit: 1}}
	if ret, _, callErr := procSetInformationJobObject.Call(job, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); ret == 0 {
		return fmt.Errorf("limiting the job object: %w", callErr)
	}

	commandLine := syscall.EscapeArg(exe)
	for _, arg := range args {
		commandLine += " " + syscall.EscapeArg(arg)
	}
	commandLinePtr, err := syscall.UTF16PtrFromString(commandLine)
	if err != nil {
		return err
	}
	startup := startupInfoEx{AttributeList: &attributes[0]}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = startfUseStdHandles
	startup.StdInput = stdinRead
	startup.StdOutput = stdout
	startup.StdErr = stderr
	var info syscall.ProcessInformation
	flags := uint32(extendedStartupInfoPresent | createSuspended | createUnicodeEnvironment)
	if err := syscall.CreateProcess(nil, commandLinePtr, nil, nil, true, flags, environmentBlock(sandboxChildEnv+"=1"), nil, &startup.StartupInfo, &info); err != nil {
		syscall.CloseHandle(stdinWrite)
		return fmt.Errorf("starting the sandboxed worker: %w", err)
	}
	runtime.KeepAlive(attributes)
	runtime.KeepAlive(capabilities)
	defer syscall.CloseHandle(info.Process)
	defer syscall.CloseHandle(info.Thread)
	if ret, _, callErr := procAssignProcessToJobObject.Call(job, uintptr(info.Process)); ret == 0 {
		syscall.TerminateProcess(info.Process, 1)
		syscall.CloseHandle(stdinWrite)
		return fmt.Errorf("confining the sandboxed worker: %w", callErr)
	}
	if ret, _, callErr := procResumeThread.Call(uintptr(info.Thread)); ret == ^uintptr(0) {
		syscall.TerminateProcess(info.Process, 1)
		syscall.CloseHandle(stdinWrite)
		return fmt.Errorf("resuming the sandboxed worker: %w", callErr)
	}

	stdinFile := os.NewFile(uintptr(stdinWrite), "worker-stdin")
	if err := writeCredentials(stdinFile, creds); err != nil {
		syscall.TerminateProcess(info.Process, 1)
		return err
	}
	stop := context.AfterFunc(ctx, func() { syscall.TerminateProcess(info.Process, 0) })
	defer stop()
	if _, err := syscall.WaitForSingleObject(info.Process, syscall.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := syscall.GetExitCodeProcess(info.Process, &code); err != nil {
		return err
	}
	if code != 0 && ctx.Err() == nil {
		return exitCodeError(code)
	}
	return nil
}
