//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// player is the first installed command-line .wav player: PipeWire, PulseAudio, then ALSA.
var player = func() []string {
	for _, p := range [][]string{{"pw-play"}, {"paplay"}, {"aplay", "-q"}} {
		if _, err := exec.LookPath(p[0]); err == nil {
			return p
		}
	}
	return nil
}()

// playFile plays a .wav without blocking.
func playFile(path string) {
	if player == nil {
		fmt.Println("  (no sound: install pw-play, paplay or aplay)")
		return
	}
	cmd := exec.Command(player[0], append(player[1:], path)...)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

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
