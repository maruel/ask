// Copyright 2025 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package shelltool

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"unsafe"

	"github.com/maruel/genai"
	"golang.org/x/sys/windows"
)

var (
	advapi32                                      = windows.NewLazyDLL("advapi32.dll")
	procCreateRestrictedToken                     = advapi32.NewProc("CreateRestrictedToken")
	userenv                                       = windows.NewLazyDLL("userenv.dll")
	procCreateAppContainerProfile                 = userenv.NewProc("CreateAppContainerProfile")
	procDeleteAppContainerProfile                 = userenv.NewProc("DeleteAppContainerProfile")
	procDeriveAppContainerSidFromAppContainerName = userenv.NewProc("DeriveAppContainerSidFromAppContainerName")
)

const (
	PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES = 0x00020005
	DISABLE_MAX_PRIVILEGE                       = 0x1
	LUA_TOKEN                                   = 0x4
	WRITE_RESTRICTED                            = 0x8

	// File System Access
	WELL_KNOWN_SID_CAPABILITY_DOCUMENTS_LIBRARY = "S-1-15-3-1" // Documents folder
	WELL_KNOWN_SID_CAPABILITY_PICTURES_LIBRARY  = "S-1-15-3-2" // Pictures folder
	WELL_KNOWN_SID_CAPABILITY_VIDEOS_LIBRARY    = "S-1-15-3-3" // Videos folder
	WELL_KNOWN_SID_CAPABILITY_MUSIC_LIBRARY     = "S-1-15-3-4" // Music folder
	WELL_KNOWN_SID_CAPABILITY_REMOVABLE_STORAGE = "S-1-15-3-5" // USB drives, etc.

	// Network Access
	WELL_KNOWN_SID_CAPABILITY_INTERNET_CLIENT               = "S-1-15-3-1" // Outbound internet
	WELL_KNOWN_SID_CAPABILITY_INTERNET_CLIENT_SERVER        = "S-1-15-3-2" // Inbound + outbound internet
	WELL_KNOWN_SID_CAPABILITY_PRIVATE_NETWORK_CLIENT_SERVER = "S-1-15-3-3" // Local network

	// System Access
	WELL_KNOWN_SID_CAPABILITY_SHARED_USER_CERTIFICATES  = "S-1-15-3-9"  // Certificate access
	WELL_KNOWN_SID_CAPABILITY_ENTERPRISE_AUTHENTICATION = "S-1-15-3-10" // Enterprise auth

	// Registry Access (limited)
	WELL_KNOWN_SID_CAPABILITY_REGISTRY_READ = "S-1-15-3-1024-1065365936-1281604716-3511738428-1654721687-432734479-3232135806-4053264122-3456934681"
)

type SECURITY_CAPABILITIES struct {
	AppContainerSid *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

func getShellTool(allowNetwork bool) (*genai.OptionsTools, error) {
	return &genai.OptionsTools{
		Tools: []genai.ToolDef{
			{
				Name:        "powershell",
				Description: "Writes the script to a file, executes it via PowerShell on the Windows computer, and returns the output",
				Callback: func(ctx context.Context, args *shellArguments) (string, error) {
					scriptPath, err := writeTempFile("ask.*.ps1", args.Script)
					if err != nil {
						return "", fmt.Errorf("failed to create temp file: %v", err)
					}
					defer os.Remove(scriptPath)
					psCmd := fmt.Sprintf("powershell.exe -ExecutionPolicy Bypass -File \"%s\"", scriptPath)
					out, err := runWithAppContainer(psCmd)
					slog.DebugContext(ctx, "bash", "command", args.Script, "output", string(out), "err", err)
					_ = os.Remove(scriptPath)
					return string(out), err
				},
			},
		},
	}, nil
}

func runWithAppContainer(cmdLine string) (string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ALL_ACCESS, &token); err != nil {
		return "", fmt.Errorf("failed to open process token: %v", err)
	}
	defer token.Close()
	// https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-createrestrictedtoken
	var restrictedToken windows.Token
	ret, _, err := procCreateRestrictedToken.Call(
		uintptr(token),
		DISABLE_MAX_PRIVILEGE|LUA_TOKEN, // |WRITE_RESTRICTED
		0,                               // DisableSidCount
		0,                               // SidsToDisable
		0,                               // DeletePrivilegeCount
		0,                               // PrivilegesToDelete
		0,                               // RestrictedSidCount
		0,                               // SidsToRestrict
		uintptr(unsafe.Pointer(&restrictedToken)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CreateRestrictedToken failed: %v", err)
	}
	defer windows.CloseHandle(windows.Handle(restrictedToken))

	var attrList *windows.ProcThreadAttributeList
	if true {
		caps := []string{
			WELL_KNOWN_SID_CAPABILITY_DOCUMENTS_LIBRARY,
			WELL_KNOWN_SID_CAPABILITY_PICTURES_LIBRARY,
			WELL_KNOWN_SID_CAPABILITY_VIDEOS_LIBRARY,
			WELL_KNOWN_SID_CAPABILITY_MUSIC_LIBRARY,
			WELL_KNOWN_SID_CAPABILITY_REMOVABLE_STORAGE,
			WELL_KNOWN_SID_CAPABILITY_INTERNET_CLIENT,
			WELL_KNOWN_SID_CAPABILITY_INTERNET_CLIENT_SERVER,
			WELL_KNOWN_SID_CAPABILITY_PRIVATE_NETWORK_CLIENT_SERVER,
		}
		sidAndAttrs, err2 := createCapabilitySIDs(caps)
		if err2 != nil {
			return "", err2
		}
		profileName := "ReadOnlyAppContainer"
		if err = createContainer(windows.StringToUTF16Ptr(profileName)); err != nil {
			return "", err
		}
		defer procDeleteAppContainerProfile.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(profileName))))
		appContainerSid, err2 := createAppContainerSid(profileName)
		if err2 != nil {
			return "", fmt.Errorf("failed to get AppContainer SID: %v", err2)
		}
		secCaps := SECURITY_CAPABILITIES{
			AppContainerSid: appContainerSid,
			Capabilities:    &sidAndAttrs[0],
			CapabilityCount: uint32(len(sidAndAttrs)),
		}
		attrListCtr, err2 := setupAppContainerAttributes(&secCaps)
		if err2 != nil {
			return "", fmt.Errorf("failed to setup attribute list: %v", err2)
		}
		attrList = attrListCtr.List()
		defer attrListCtr.Delete()
	}

	// There isn't much point into separating stdout and stderr to send it back to the LLM, so merge both.
	stdoutRead, stdoutWrite, err := createPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	defer windows.CloseHandle(stdoutRead)
	defer windows.CloseHandle(stdoutWrite)

	si := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESHOWWINDOW | windows.STARTF_USESTDHANDLES,
			StdOutput: windows.Handle(stdoutWrite),
			StdErr:    windows.Handle(stdoutWrite),
		},
		ProcThreadAttributeList: attrList,
	}
	pi := windows.ProcessInformation{}
	var flag uint32 = windows.CREATE_NEW_CONSOLE | windows.EXTENDED_STARTUPINFO_PRESENT
	if err = windows.CreateProcessAsUser(restrictedToken, nil, windows.StringToUTF16Ptr(cmdLine), nil, nil, true, flag, nil, nil, &si.StartupInfo, &pi); err != nil {
		return "", err
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)
	// Close write handles in parent process to avoid blocking.
	_ = windows.CloseHandle(stdoutWrite)
	stdout := readFromPipe(stdoutRead)
	_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
	var exitCode uint32
	_ = windows.GetExitCodeProcess(pi.Process, &exitCode)
	err = nil
	if exitCode != 0 {
		if exitCode > 255 {
			err = fmt.Errorf("exit code 0x%08x", exitCode)
		} else {
			err = fmt.Errorf("exit code %d", exitCode)
		}
	}
	return stdout, err
}

func createPipe() (windows.Handle, windows.Handle, error) {
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	var r, w windows.Handle
	if err := windows.CreatePipe(&r, &w, &sa, 0); err != nil {
		return 0, 0, fmt.Errorf("CreatePipe failed: %w", err)
	}
	// Make sure the read handle is not inherited.
	_ = windows.SetHandleInformation(r, windows.HANDLE_FLAG_INHERIT, 0)
	return r, w, nil
}

func readFromPipe(handle windows.Handle) string {
	buf := bytes.Buffer{}
	buffer := make([]byte, 4096)
	var bytesRead uint32
	for {
		if err := windows.ReadFile(handle, buffer, &bytesRead, nil); err != nil {
			break
		}
		buf.Write(buffer[:bytesRead])
	}
	return buf.String()
}

func createContainer(profileNamePtr *uint16) error {
	displayNamePtr := windows.StringToUTF16Ptr("Read-Only App Container")
	descriptionPtr := windows.StringToUTF16Ptr("Highly restricted read-only App Container")
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
			_, _, _ = procDeleteAppContainerProfile.Call(uintptr(unsafe.Pointer(profileNamePtr)))
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
			return fmt.Errorf("CreateAppContainerProfile failed with code: 0x%08x, error: %w", ret, err)
		}
	}
	return nil
}

func createAppContainerSid(profileName string) (*windows.SID, error) {
	var sid *windows.SID
	ret, _, err := procDeriveAppContainerSidFromAppContainerName.Call(
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(profileName))),
		uintptr(unsafe.Pointer(&sid)),
	)
	if ret != 0 {
		return nil, fmt.Errorf("DeriveAppContainerSidFromAppContainerName failed: %w", err)
	}
	return sid, nil
}

// https://github.com/rancher-sandbox/rancher-desktop/blob/main/src/go/rdctl/pkg/process/process_windows.go shows job object use.
// https://blahcat.github.io/2020-12-29-cheap-sandboxing-with-appcontainers/
func setupAppContainerAttributes(secCaps *SECURITY_CAPABILITIES) (*windows.ProcThreadAttributeListContainer, error) {
	// TODO: Testing with zero.
	attributeList, err := windows.NewProcThreadAttributeList(0)
	if err != nil {
		return nil, fmt.Errorf("failed to NewProcThreadAttributeList: %w", err)
	}
	if false {
		// TODO: Another good idea is PROC_THREAD_ATTRIBUTE_HANDLE_LIST
		if err = attributeList.Update(PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES, unsafe.Pointer(secCaps), unsafe.Sizeof(*secCaps)); err != nil {
			return nil, fmt.Errorf("failed to update: %w", err)
		}
	}
	return attributeList, err
}

func createCapabilitySIDs(sidStrings []string) ([]windows.SIDAndAttributes, error) {
	if len(sidStrings) == 0 {
		return nil, nil
	}
	capabilities := make([]windows.SIDAndAttributes, len(sidStrings))
	for i, sidString := range sidStrings {
		var sid *windows.SID
		err := windows.ConvertStringSidToSid(windows.StringToUTF16Ptr(sidString), &sid)
		if err != nil {
			return nil, fmt.Errorf("ConvertStringSidToSid failed for %s: %w", sidString, err)
		}
		capabilities[i] = windows.SIDAndAttributes{Sid: sid}
	}
	return capabilities, nil
}
