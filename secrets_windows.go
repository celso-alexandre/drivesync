//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = syscall.Errno(1168)
	credentialTarget        = keyringService + "/" + keyringAccount
)

type winCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func loadStoredCredentials() (*credentials, error) {
	target, err := syscall.UTF16PtrFromString(credentialTarget)
	if err != nil {
		return nil, err
	}
	var cred *winCredential
	ret, _, callErr := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if ret == 0 {
		if errors.Is(callErr, errorNotFound) {
			return nil, errNotSignedIn
		}
		return nil, fmt.Errorf("reading Credential Manager: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	return parseCredentials(string(blob))
}

type pendingSecret struct{}

func prepareCredentialStore() (*pendingSecret, error) {
	return &pendingSecret{}, nil
}

func (p *pendingSecret) commit(creds credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(credentialTarget)
	if err != nil {
		return err
	}
	user, err := syscall.UTF16PtrFromString(keyringAccount)
	if err != nil {
		return err
	}
	cred := winCredential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(data)),
		CredentialBlob:     &data[0],
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	if ret, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0); ret == 0 {
		return fmt.Errorf("saving to Credential Manager: %w", callErr)
	}
	return nil
}

func (p *pendingSecret) abort() {}

func clearCredentials() error {
	target, err := syscall.UTF16PtrFromString(credentialTarget)
	if err != nil {
		return err
	}
	if ret, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0); ret == 0 && !errors.Is(callErr, errorNotFound) {
		return fmt.Errorf("clearing Credential Manager: %w", callErr)
	}
	return nil
}
