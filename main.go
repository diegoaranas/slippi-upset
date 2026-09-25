// Slippi upset alert: plays a sound when you beat someone rated higher than you.
//
// Run it in a terminal while playing:   upset   (or double-click upset.exe)
// Test it on an existing replay:        upset --test path/to/Game.slp
// Re-download the sounds:               upset --get-sounds --volume 1
//
// Windows, Linux and macOS. It checks the replay folder every second, reads the first 2 KB
// of a replay when a game starts and the whole replay once after it ends, so it
// shouldn't affect Dolphin. It also runs at lower CPU priority (except on Linux).
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ---------------------------------------------------------------- config ----
var (
	myCode      = ""  // e.g. "ABCD#123"; "" = read it from Slippi Launcher
	replayDir   = ""  // e.g. `D:\Replays`; "" = read it from Slippi Launcher's settings
	pollSeconds = 1.0 // how often to check the replay folder

	// Any .wav, .mp3, .ogg or .flac file works, relative to the folder upset.exe is in.
	// Win sounds, highest precedence first:
	soundRecord  = "sounds/new_record.wav"      // "A new record!" - highest-rated opponent you've ever beaten
	soundPeak    = "sounds/incredible.wav"      // "Wow! Incredible!" - opp best season > your best
	soundCurrent = "sounds/congratulations.wav" // "Congratulations!" - opp current rating > yours
	soundWin     = "sounds/complete.wav"        // "Complete!" - any other win
	// First game vs a new opponent:
	soundChallenger = "sounds/challenger.wav" // Challenger Approaching jingle - they're rated higher
	soundConnect    = ""                      // everyone else (e.g. sounds/versus.wav); "" = silent
	// Opponent quits (resets) mid-game:
	soundQuit = "sounds/no_contest.wav" // "No contest!"

	recordFile = "record.json" // your best win so far
)

// -----------------------------------------------------------------------------

var version = "dev" // set by tools/build

var here = exeDir()

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func inHere(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(here, filepath.FromSlash(p))
}

// launcherDir is Electron's userData folder: %APPDATA% on Windows, ~/.config on Linux,
// ~/Library/Application Support on macOS.
func launcherDir() string {
	config, _ := os.UserConfigDir()
	return filepath.Join(config, "Slippi Launcher")
}

// detectCode reads your connect code from the account Slippi Launcher is logged into.
func detectCode() (string, error) {
	for _, p := range userJSONPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var user struct {
			ConnectCode string `json:"connectCode"` // only this field; the file also holds your login key
		}
		if json.Unmarshal(data, &user) == nil && user.ConnectCode != "" {
			return user.ConnectCode, nil
		}
	}
	return "", errors.New("Couldn't find your connect code. Log in to Slippi Launcher, or set myCode in main.go.")
}

// detectReplayDir returns Slippi Launcher's replay folder setting, or its default.
func detectReplayDir() string {
	var s struct {
		Settings struct {
			RootSlpPath string `json:"rootSlpPath"`
		} `json:"settings"`
	}
	if data, err := os.ReadFile(filepath.Join(launcherDir(), "Settings")); err == nil {
		if json.Unmarshal(data, &s) == nil && s.Settings.RootSlpPath != "" {
			return s.Settings.RootSlpPath
		}
	}
	return defaultReplayDir()
}

// ----------------------------------------------------------------- main ----
func isMe(code string) bool {
	return code != "" && strings.EqualFold(code, myCode)
}

func handle(path string) error {
	g, err := parseGame(path)
	if err != nil {
		return err
	}
	me, opp := -1, ""
	for p, c := range g.Codes {
		if isMe(c) {
			me = p
		}
	}
	for p, c := range g.Codes {
		if p != me && c != "" {
			opp = c
		}
	}
	name := filepath.Base(path)
	if me == -1 || opp == "" {
		fmt.Printf("%s: not a 1v1 netplay game with you in it, skipping\n", name)
		return nil
	}
	if g.Quitter != -1 && g.Quitter != me {
		fmt.Printf("%s: vs %s: they quit  >>> NO CONTEST\n", name, opp)
		play(soundQuit)
		return nil
	}
	if g.Winner != me {
		result := "no result"
		if g.Winner != -1 {
			result = "loss"
		} else if g.Quitter == me {
			result = "you quit"
		}
		fmt.Printf("%s: vs %s: %s\n", name, opp, result)
		return nil
	}

	mine, err := fetchRatings(myCode)
	if err != nil {
		return err
	}
	theirs, err := fetchRatings(opp)
	if err != nil {
		return err
	}
	record := loadRecord()
	var sound, tag string
	switch {
	case higher(theirs.Current, &record.Rating):
		saveRecord(Record{Rating: *theirs.Current, Code: opp, Date: time.Now().Format("2006-01-02 15:04")})
		sound, tag = soundRecord, fmt.Sprintf("NEW RECORD! (was %.1f)", record.Rating)
	case higher(theirs.Peak, mine.Peak):
		sound, tag = soundPeak, "beat a higher peak!"
	case higher(theirs.Current, mine.Current):
		sound, tag = soundCurrent, "UPSET!"
	default:
		sound = soundWin
	}
	if tag != "" {
		tag = "  >>> " + tag
	}
	fmt.Printf("%s: WIN vs %s  them %s (peak %s)  you %s (peak %s)%s\n", name, opp,
		fmtRating(theirs.Current), fmtRating(theirs.Peak), fmtRating(mine.Current), fmtRating(mine.Peak), tag)
	play(sound)
	return nil
}

// Record is the highest current rating of any opponent you've beaten (starts at 0).
type Record struct {
	Rating float64 `json:"rating"`
	Code   string  `json:"code,omitempty"`
	Date   string  `json:"date,omitempty"`
}

func loadRecord() Record {
	var r Record
	if data, err := os.ReadFile(inHere(recordFile)); err == nil {
		if json.Unmarshal(data, &r) != nil {
			return Record{}
		}
	}
	return r
}

func saveRecord(r Record) {
	data, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(inHere(recordFile), data, 0o644); err != nil {
		fmt.Printf("couldn't save record: %v\n", err)
	}
}

func play(sound string) {
	if sound == "" {
		return
	}
	p := inHere(sound)
	if !fileExists(p) {
		fmt.Printf("  (no sound: %s is missing; run upset --get-sounds)\n", p)
		return
	}
	playFile(p)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func announceOpponent(opp string) {
	mine, err := fetchRatings(myCode)
	var theirs Ratings
	if err == nil {
		theirs, err = fetchRatings(opp)
	}
	if err != nil {
		fmt.Printf("New opponent: %s  (rating lookup failed: %v)\n", opp, err)
		return
	}
	tag, sound := "", soundConnect
	if isHigher(mine, theirs) {
		tag, sound = "  >>> CHALLENGER APPROACHING", soundChallenger
	}
	fmt.Printf("New opponent: %s  %s (peak %s)%s\n", opp, fmtRating(theirs.Current), fmtRating(theirs.Peak), tag)
	play(sound)
}

// Ishiiruka Dolphin writes YYYY-MM month folders; mainline Dolphin writes YYYY-MM-Mainline.
var monthDir = regexp.MustCompile(`^(\d{4}-\d{2})(-Mainline)?$`)

// replayDirs returns the root folder plus the month folders for the two newest months.
// Other subfolders (Spectate, anything the user made) are ignored so they can't crowd
// out the current month.
func replayDirs() ([]string, error) {
	entries, err := os.ReadDir(replayDir)
	if err != nil {
		return nil, err
	}
	byMonth := map[string][]string{}
	for _, e := range entries {
		if m := monthDir.FindStringSubmatch(e.Name()); m != nil && e.IsDir() {
			byMonth[m[1]] = append(byMonth[m[1]], filepath.Join(replayDir, e.Name()))
		}
	}
	months := slices.Sorted(maps.Keys(byMonth))
	dirs := []string{replayDir}
	for _, m := range months[max(0, len(months)-2):] {
		dirs = append(dirs, byMonth[m]...)
	}
	return dirs, nil
}

// ensureSounds downloads the announcer clips if any are missing.
func ensureSounds() {
	var missing []string
	for _, s := range []string{soundRecord, soundPeak, soundCurrent, soundWin, soundChallenger, soundConnect, soundQuit} {
		if s != "" && !fileExists(inHere(s)) {
			missing = append(missing, filepath.Base(s))
		}
	}
	if len(missing) > 0 {
		fmt.Printf("Missing sounds: %s. Downloading them now (one time)...\n", strings.Join(missing, ", "))
		if err := downloadSounds(inHere("sounds"), 0.5); err != nil {
			fmt.Printf("Couldn't download the sounds (%v). Alerts for missing files will be silent.\n", err)
		}
	}
}

func watch() {
	lowerPriority()
	started := time.Now()
	done, seen := map[string]bool{}, map[string]bool{}
	lastOpp := ""
	fmt.Printf("Watching %s for new games as %s... (Ctrl+C to stop)\n", replayDir, myCode)
	for {
		dirs, err := replayDirs()
		if err != nil {
			fmt.Printf("folder scan error: %v\n", err)
		}
		for _, d := range dirs {
			entries, err := os.ReadDir(d)
			if err != nil {
				fmt.Printf("folder scan error: %v\n", err)
				continue
			}
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".slp") {
					continue
				}
				info, err := e.Info()
				if err != nil || !info.ModTime().After(started) {
					continue
				}
				path := filepath.Join(d, e.Name())
				if !seen[path] { // game just started
					if codes := startCodes(path); codes != nil {
						seen[path] = true
						var opps []string
						for _, c := range codes {
							if !isMe(c) {
								opps = append(opps, c)
							}
						}
						if len(codes) == 2 && len(opps) == 1 && opps[0] != lastOpp {
							lastOpp = opps[0]
							announceOpponent(lastOpp)
						}
					}
				}
				if !done[path] && rawLength(path) != 0 { // game just ended
					done[path] = true
					if err := handle(path); err != nil {
						fmt.Printf("%s: error: %v\n", e.Name(), err)
					}
				}
			}
		}
		time.Sleep(time.Duration(pollSeconds * float64(time.Second)))
	}
}

func fail(msg string) {
	fmt.Println(msg)
	if ownsConsole() { // double-clicked .exe: keep the window open to show the error
		fmt.Print("Press Enter to close.")
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(1)
}

func main() {
	test := flag.String("test", "", "check a `replay` you've already played instead of watching")
	getSounds := flag.Bool("get-sounds", false, "(re)download the announcer clips to sounds/ and exit")
	volume := flag.Float64("volume", 0.5, "volume for --get-sounds (1 = original)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("upset", version)
		return
	}

	if *getSounds {
		if err := downloadSounds(inHere("sounds"), *volume); err != nil {
			fail(err.Error())
		}
		return
	}
	if myCode == "" {
		code, err := detectCode()
		if err != nil {
			fail(err.Error())
		}
		myCode = code
	}
	if replayDir == "" {
		replayDir = detectReplayDir()
	}
	if info, err := os.Stat(replayDir); err != nil || !info.IsDir() {
		fail(fmt.Sprintf("Replay folder not found: %s\nSet replayDir in main.go, or check Slippi Launcher's replay settings.", replayDir))
	}
	ensureSounds()
	if *test != "" {
		if err := handle(*test); err != nil {
			fail(err.Error())
		}
		time.Sleep(3 * time.Second) // let the async sound finish
		return
	}
	watch()
}
