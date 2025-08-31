//go:build ignore

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCreateProcess        = kernel32.NewProc("CreateProcessW")
	procWaitForSingleObject  = kernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess   = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procCreatePipe           = kernel32.NewProc("CreatePipe")
	procReadFile             = kernel32.NewProc("ReadFile")
	procSetHandleInformation = kernel32.NewProc("SetHandleInformation")
)

const (
	STARTF_USESTDHANDLES = 0x00000100
	HANDLE_FLAG_INHERIT  = 0x00000001
	INFINITE             = 0xFFFFFFFF
)

type SecurityAttributes struct {
	Length             uint32
	SecurityDescriptor uintptr
	InheritHandle      uint32
}

type StartupInfo struct {
	Cb            uint32
	_             *uint16 // lpReserved
	Desktop       *uint16 // lpDesktop
	Title         *uint16 // lpTitle
	X             uint32
	Y             uint32
	XSize         uint32
	YSize         uint32
	XCountChars   uint32
	YCountChars   uint32
	FillAttribute uint32
	Flags         uint32
	ShowWindow    uint16
	_             uint16 // cbReserved2
	_             *byte  // lpReserved2
	StdInput      syscall.Handle
	StdOutput     syscall.Handle
	StdError      syscall.Handle
}

type ProcessInformation struct {
	Process   syscall.Handle
	Thread    syscall.Handle
	ProcessId uint32
	ThreadId  uint32
}

// runPowerShellScript executes a PowerShell script using pure Win32 API and returns stdout and stderr as bytes
func runPowerShellScript(scriptPath string) (stdout, stderr []byte, err error) {
	// Create pipes for stdout and stderr
	var stdoutRead, stdoutWrite syscall.Handle
	var stderrRead, stderrWrite syscall.Handle

	sa := SecurityAttributes{
		Length:        uint32(unsafe.Sizeof(SecurityAttributes{})),
		InheritHandle: 1,
	}

	// Create stdout pipe
	ret, _, err := procCreatePipe.Call(
		uintptr(unsafe.Pointer(&stdoutRead)),
		uintptr(unsafe.Pointer(&stdoutWrite)),
		uintptr(unsafe.Pointer(&sa)),
		0,
	)
	if ret == 0 {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	// Ensure read handles are not inherited
	procSetHandleInformation.Call(uintptr(stdoutRead), HANDLE_FLAG_INHERIT, 0)
	defer procCloseHandle.Call(uintptr(stdoutRead))
	defer procCloseHandle.Call(uintptr(stdoutWrite))

	// Create stderr pipe
	ret, _, err = procCreatePipe.Call(
		uintptr(unsafe.Pointer(&stderrRead)),
		uintptr(unsafe.Pointer(&stderrWrite)),
		uintptr(unsafe.Pointer(&sa)),
		0,
	)
	if ret == 0 {
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %v", err)
	}
	procSetHandleInformation.Call(uintptr(stderrRead), HANDLE_FLAG_INHERIT, 0)
	defer procCloseHandle.Call(uintptr(stderrRead))
	defer procCloseHandle.Call(uintptr(stderrWrite))

	// Create command line
	cmdLine := fmt.Sprintf(`powershell.exe -ExecutionPolicy Bypass -File "%s"`, scriptPath)
	cmdLinePtr, _ := syscall.UTF16PtrFromString(cmdLine)

	// Setup startup info
	si := StartupInfo{
		Cb:        uint32(unsafe.Sizeof(StartupInfo{})),
		Flags:     STARTF_USESTDHANDLES,
		StdInput:  0, // No stdin - this ensures stdin is closed/null
		StdOutput: stdoutWrite,
		StdError:  stderrWrite,
	}

	var pi ProcessInformation

	// Create the process
	ret, _, err = procCreateProcess.Call(
		0,                                   // lpApplicationName
		uintptr(unsafe.Pointer(cmdLinePtr)), // lpCommandLine
		0,                                   // lpProcessAttributes
		0,                                   // lpThreadAttributes
		1,                                   // bInheritHandles
		0,                                   // dwCreationFlags
		0,                                   // lpEnvironment
		0,                                   // lpCurrentDirectory
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ret == 0 {
		return nil, nil, fmt.Errorf("failed to create process: %v", err)
	}
	defer procCloseHandle.Call(uintptr(pi.Process))
	defer procCloseHandle.Call(uintptr(pi.Thread))

	// Close write ends of pipes in parent process
	procCloseHandle.Call(uintptr(stdoutWrite))
	procCloseHandle.Call(uintptr(stderrWrite))

	// Read from pipes
	stdoutData := make([]byte, 0)
	stderrData := make([]byte, 0)

	buffer := make([]byte, 4096)
	var bytesRead uint32

	// Read stdout
	for {
		ret, _, _ := procReadFile.Call(
			uintptr(stdoutRead),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&bytesRead)),
			0,
		)
		if ret == 0 || bytesRead == 0 {
			break
		}
		stdoutData = append(stdoutData, buffer[:bytesRead]...)
	}

	// Read stderr
	for {
		ret, _, _ := procReadFile.Call(
			uintptr(stderrRead),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&bytesRead)),
			0,
		)
		if ret == 0 || bytesRead == 0 {
			break
		}
		stderrData = append(stderrData, buffer[:bytesRead]...)
	}

	// Wait for process to complete
	procWaitForSingleObject.Call(uintptr(pi.Process), INFINITE)

	// Get exit code
	var exitCode uint32
	procGetExitCodeProcess.Call(uintptr(pi.Process), uintptr(unsafe.Pointer(&exitCode)))

	if exitCode != 0 {
		err = fmt.Errorf("process exited with code %d", exitCode)
	} else {
		err = nil
	}

	return stdoutData, stderrData, err
}

// Example usage
func main() {
	scriptPath := `C:\Users\maruel\AppData\Local\Temp\ask_script_3085323650.ps1`

	stdout, stderr, err := runPowerShellScript(scriptPath)

	if err != nil {
		fmt.Printf("Command failed with error: %v\n", err)
	}

	fmt.Printf("STDOUT (%d bytes):\n%s\n", len(stdout), string(stdout))
	fmt.Printf("STDERR (%d bytes):\n%s\n", len(stderr), string(stderr))
}
