// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package ask

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"unsafe"

	"github.com/maruel/genai"
	"golang.org/x/sys/windows"
)

var (
	userenv  = windows.NewLazyDLL("userenv.dll")
	kernel32 = windows.NewLazyDLL("kernel32.dll")
	advapi32 = windows.NewLazyDLL("advapi32.dll")

	procCreateAppContainerProfile = userenv.NewProc("CreateAppContainerProfile")
	procDeleteAppContainerProfile = userenv.NewProc("DeleteAppContainerProfile")
	// procDeriveAppContainerSidFromAppContainerName = userenv.NewProc("DeriveAppContainerSidFromAppContainerName")
	procCreateProcessAsUser   = advapi32.NewProc("CreateProcessAsUserW")
	procCreateRestrictedToken = advapi32.NewProc("CreateRestrictedToken")
	procCreatePipe            = kernel32.NewProc("CreatePipe")
	procSetHandleInformation  = kernel32.NewProc("SetHandleInformation")
	procCloseHandle           = kernel32.NewProc("CloseHandle")
	procReadFile              = kernel32.NewProc("ReadFile")
	// procFreeSid                                   = advapi32.NewProc("FreeSid")
)

const (
	LOGON32_LOGON_INTERACTIVE = 2
	LOGON32_PROVIDER_DEFAULT  = 0
	CREATE_NEW_CONSOLE        = 0x00000010
	// CREATE_NO_WINDOW          = 0x08000000
	// STARTF_USESTDHANDLES      = 0x00000100
	// CREATE_SUSPENDED          = 0x00000004
	STARTF_USESHOWWINDOW = 0x00000001
	SW_HIDE              = 0
	HANDLE_FLAG_INHERIT  = 0x00000001

	// Security attributes for read-only access
	DISABLE_MAX_PRIVILEGE = 0x1
	SANDBOX_INERT         = 0x2
	LUA_TOKEN             = 0x4
	WRITE_RESTRICTED      = 0x8

	INVALID_HANDLE_VALUE = ^uintptr(0)
)

type SECURITY_CAPABILITIES struct {
	AppContainerSid *windows.SID
	Capabilities    *SID_AND_ATTRIBUTES
	CapabilityCount uint32
	Reserved        uint32
}

type SID_AND_ATTRIBUTES struct {
	Sid        *windows.SID
	Attributes uint32
}

type SECURITY_ATTRIBUTES struct {
	NLength              uint32
	LpSecurityDescriptor unsafe.Pointer
	BInheritHandle       int32
}

type STARTUPINFO struct {
	Cb              uint32
	LpReserved      *uint16
	LpDesktop       *uint16
	LpTitle         *uint16
	DwX             uint32
	DwY             uint32
	DwXSize         uint32
	DwYSize         uint32
	DwXCountChars   uint32
	DwYCountChars   uint32
	DwFillAttribute uint32
	DwFlags         uint32
	WShowWindow     uint16
	CbReserved2     uint16
	LpReserved2     *byte
	HStdInput       windows.Handle
	HStdOutput      windows.Handle
	HStdError       windows.Handle
}

type PROCESS_INFORMATION struct {
	HProcess    windows.Handle
	HThread     windows.Handle
	DwProcessId uint32
	DwThreadId  uint32
}

func getSandbox(ctx context.Context) (*genai.OptionsTools, error) {
	return &genai.OptionsTools{
		Tools: []genai.ToolDef{
			{
				Name:        "cmd.exe",
				Description: "Runs the requested command CreateProcess on the Windows computer and returns the output",
				Callback: func(ctx context.Context, args *bashArguments) (string, error) {
					out, err := runWithRestrictedAppContainer(args.CommandLine)
					slog.DebugContext(ctx, "bash", "command", args.CommandLine, "output", string(out), "err", err)
					return string(out), err
				},
			},
		},
	}, nil
}

func newStr16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return p
}

func createContainer(profileNamePtr *uint16) error {
	// Create App Container Profile with NO capabilities (most restrictive)
	displayNamePtr := newStr16("Read-Only App Container")
	descriptionPtr := newStr16("Highly restricted read-only App Container")
	var appContainerSid *windows.SID
	ret, _, err := procCreateAppContainerProfile.Call(
		uintptr(unsafe.Pointer(profileNamePtr)),
		uintptr(unsafe.Pointer(displayNamePtr)),
		uintptr(unsafe.Pointer(descriptionPtr)),
		0, // pCapabilities - NULL for no capabilities
		0, // dwCapabilityCount - 0 for maximum restriction
		uintptr(unsafe.Pointer(&appContainerSid)),
	)
	if ret != 0 {
		// If profile already exists, try to delete and recreate
		if ret == 0x800700B7 { // HRESULT_FROM_WIN32(ERROR_ALREADY_EXISTS)
			procDeleteAppContainerProfile.Call(uintptr(unsafe.Pointer(profileNamePtr)))
			// Try creating again
			ret, _, err = procCreateAppContainerProfile.Call(
				uintptr(unsafe.Pointer(profileNamePtr)),
				uintptr(unsafe.Pointer(displayNamePtr)),
				uintptr(unsafe.Pointer(descriptionPtr)),
				0, // pCapabilities
				0, // dwCapabilityCount
				uintptr(unsafe.Pointer(&appContainerSid)),
			)
		}
		if ret != 0 {
			return fmt.Errorf("CreateAppContainerProfile failed with code: 0x%x, error: %v", ret, err)
		}
	}
	return nil
}

func runWithRestrictedAppContainer(cmdLine string) (string, error) {
	profileNamePtr := newStr16("ReadOnlyAppContainer")
	if err := createContainer(profileNamePtr); err != nil {
		return "", err
	}
	// Get current user token
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ALL_ACCESS, &token); err != nil {
		return "", fmt.Errorf("failed to open process token: %v", err)
	}
	defer token.Close()

	stdoutRead, stdoutWrite, err := createPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	defer procCloseHandle.Call(uintptr(stdoutRead))
	defer procCloseHandle.Call(uintptr(stdoutWrite))
	stderrRead, stderrWrite, err := createPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %v", err)
	}
	defer procCloseHandle.Call(uintptr(stderrRead))
	defer procCloseHandle.Call(uintptr(stderrWrite))

	// Make sure the read handles are not inherited
	procSetHandleInformation.Call(uintptr(stdoutRead), HANDLE_FLAG_INHERIT, 0)
	procSetHandleInformation.Call(uintptr(stderrRead), HANDLE_FLAG_INHERIT, 0)

	// Create a restricted token with minimal privileges
	var restrictedToken windows.Handle
	ret, _, err := procCreateRestrictedToken.Call(
		uintptr(token),
		DISABLE_MAX_PRIVILEGE|LUA_TOKEN|WRITE_RESTRICTED, // Maximum restrictions
		0, // DisableSidCount
		0, // SidsToDisable
		0, // DeletePrivilegeCount
		0, // PrivilegesToDelete
		0, // RestrictedSidCount
		0, // SidsToRestrict
		uintptr(unsafe.Pointer(&restrictedToken)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CreateRestrictedToken failed: %v", err)
	}
	defer procCloseHandle.Call(uintptr(restrictedToken))
	/*
		// Setup security capabilities with App Container SID
		secCaps := SECURITY_CAPABILITIES{
			AppContainerSid: appContainerSid,
			Capabilities:    nil, // No additional capabilities
			CapabilityCount: 0,   // Zero capabilities for maximum restriction
			Reserved:        0,
		}
	*/
	si := STARTUPINFO{
		Cb:          uint32(unsafe.Sizeof(STARTUPINFO{})),
		DwFlags:     STARTF_USESHOWWINDOW,
		WShowWindow: SW_HIDE,
		HStdInput:   windows.Handle(windows.InvalidHandle),
		HStdOutput:  stdoutWrite,
		HStdError:   stderrWrite,
	}
	stdoutChan := make(chan string, 100)
	stderrChan := make(chan string, 100)
	go readFromPipe(stdoutRead, stdoutChan, "[STDOUT] ")
	go readFromPipe(stderrRead, stderrChan, "[STDERR] ")

	var pi PROCESS_INFORMATION
	// Create process with App Container and restricted token
	// Using EXTENDED_STARTUPINFO would allow us to pass SECURITY_CAPABILITIES
	// but we'll use the restricted token approach for simplicity
	ret, _, err = procCreateProcessAsUser.Call(
		uintptr(restrictedToken), // Use restricted token
		0,                        // lpApplicationName
		uintptr(unsafe.Pointer(newStr16(cmdLine))),
		0,                  // lpProcessAttributes
		0,                  // lpThreadAttributes
		0,                  // bInheritHandles
		CREATE_NEW_CONSOLE, // Create flags
		0,                  // lpEnvironment
		0,                  // lpCurrentDirectory
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CreateProcessAsUser failed: %v", err)
	}

	// Close write handles in parent process to avoid blocking
	procCloseHandle.Call(uintptr(stdoutWrite))
	procCloseHandle.Call(uintptr(stderrWrite))

	mu := sync.Mutex{}
	buf := bytes.Buffer{}
	outputDone := make(chan bool, 2)
	go func() {
		for output := range stdoutChan {
			mu.Lock()
			buf.WriteString(output)
			mu.Unlock()
		}
		outputDone <- true
	}()
	go func() {
		for output := range stderrChan {
			mu.Lock()
			buf.WriteString(output)
			mu.Unlock()
		}
		outputDone <- true
	}()

	windows.WaitForSingleObject(pi.HProcess, windows.INFINITE)

	<-outputDone
	<-outputDone

	var exitCode uint32
	windows.GetExitCodeProcess(pi.HProcess, &exitCode)
	procCloseHandle.Call(uintptr(pi.HProcess))
	procCloseHandle.Call(uintptr(pi.HThread))
	procDeleteAppContainerProfile.Call(uintptr(unsafe.Pointer(profileNamePtr)))
	return buf.String(), nil
}

func createPipe() (readHandle, writeHandle windows.Handle, err error) {
	sa := SECURITY_ATTRIBUTES{
		NLength:        uint32(unsafe.Sizeof(SECURITY_ATTRIBUTES{})),
		BInheritHandle: 1, // Allow inheritance
	}
	var r, w uintptr
	ret, _, err := procCreatePipe.Call(
		uintptr(unsafe.Pointer(&r)),
		uintptr(unsafe.Pointer(&w)),
		uintptr(unsafe.Pointer(&sa)),
		0, // Default buffer size
	)
	if ret == 0 {
		return 0, 0, fmt.Errorf("CreatePipe failed: %v", err)
	}
	return windows.Handle(r), windows.Handle(w), nil
}

func readFromPipe(handle windows.Handle, output chan<- string, prefix string) {
	defer close(output)
	buffer := make([]byte, 4096)
	var bytesRead uint32
	for {
		ret, _, _ := procReadFile.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&bytesRead)),
			0, // lpOverlapped
		)
		if ret == 0 || bytesRead == 0 {
			break
		}
		data := string(buffer[:bytesRead])
		output <- fmt.Sprintf("%s%s", prefix, data)
	}
}
