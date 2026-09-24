//go:build windows

package main

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Not wrapped by x/sys/windows.
var (
	procPlaySound             = windows.NewLazySystemDLL("winmm.dll").NewProc("PlaySoundW")
	procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndFilename  = 0x00020000
)

// playFile plays a .wav without blocking.
func playFile(path string) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	procPlaySound.Call(uintptr(unsafe.Pointer(p)), 0, sndFilename|sndAsync|sndNoDefault)
}

// userJSONPaths lists where the netplay Dolphin keeps user.json: the Ishiiruka
// build and mainline stable, then mainline beta.
func userJSONPaths() []string {
	return []string{
		filepath.Join(launcherDir(), "netplay", "User", "Slippi", "user.json"),
		filepath.Join(launcherDir(), "netplay-beta", "User", "Slippi", "user.json"),
	}
}

// defaultReplayDir is Documents\Slippi. KnownFolderPath follows OneDrive
// redirection, unlike %USERPROFILE%\Documents.
func defaultReplayDir() string {
	docs, _ := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	return filepath.Join(docs, "Slippi")
}

// lowerPriority makes the OS always favor Dolphin.
func lowerPriority() {
	windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS)
}

// ownsConsole is true when the console window was opened just for us (the .exe
// was double-clicked), so it would vanish before an error could be read.
func ownsConsole() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	return n == 1
}
