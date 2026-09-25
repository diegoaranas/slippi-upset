//go:build linux

package main

import (
	"os"
	"path/filepath"
)

// userJSONPaths lists where the netplay Dolphin keeps user.json: the Ishiiruka build,
// then mainline stable and beta. Dolphin honors XDG_CONFIG_HOME, as does os.UserConfigDir.
func userJSONPaths() []string {
	config, _ := os.UserConfigDir()
	return []string{
		filepath.Join(config, "SlippiOnline", "Slippi", "user.json"),
		filepath.Join(config, "slippi-dolphin", "netplay", "Slippi", "user.json"),
		filepath.Join(config, "slippi-dolphin", "netplay-beta", "Slippi", "user.json"),
	}
}

// defaultReplayDir is Slippi Launcher's default on Linux: ~/Slippi, not ~/Documents.
func defaultReplayDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Slippi")
}

// Niceness is per-thread on Linux, so it can't be set for the whole Go process.
// The watcher sleeps between polls, so it doesn't compete with Dolphin anyway.
func lowerPriority() {}

func ownsConsole() bool { return false }
