//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// playFile plays a .wav without blocking.
func playFile(path string) {
	cmd := exec.Command("afplay", path)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

// userJSONPaths lists where the netplay Dolphin keeps user.json: the Ishiiruka build,
// then mainline stable and beta.
func userJSONPaths() []string {
	config, _ := os.UserConfigDir() // ~/Library/Application Support
	dolphin := filepath.Join(config, "com.project-slippi.dolphin")
	return []string{
		filepath.Join(dolphin, "Slippi", "user.json"),
		filepath.Join(dolphin, "netplay", "User", "Slippi", "user.json"),
		filepath.Join(dolphin, "netplay-beta", "User", "Slippi", "user.json"),
	}
}

// defaultReplayDir is Slippi Launcher's default on macOS: ~/Slippi, not ~/Documents.
func defaultReplayDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Slippi")
}

// lowerPriority makes the OS always favor Dolphin. Unlike Linux, macOS applies
// niceness to the whole process.
func lowerPriority() {
	syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10)
}

func ownsConsole() bool { return false }
