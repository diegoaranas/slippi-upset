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
	soundChallenger = "sounds/challenger.wav"  // Challenger Approaching jingle - rated above you (see assess)
	soundHiddenBoss = "sounds/hidden_boss.wav" // not rated above you, but leads you head-to-head; falls back to soundChallenger if missing
	soundConnect    = ""                       // everyone else (e.g. sounds/versus.wav); "" = silent
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

// criterion is one check made when a new opponent joins.
type criterion struct {
	name, detail string
	met          bool
	counts       bool // used in this mode
}

// assess runs every check against a new opponent. They're "rated above you" if a
// rating check that counts in this mode is met:
//   - unranked: their current rating, or their best season, beats yours;
//   - Ranked, where their current rating is already on screen: only their best
//     previous season beating your best.
//
// They're a "hidden boss" if they aren't rated above you but have more wins against
// you than you against them in the replays on this computer.
func assess(mine, theirs Ratings, ratingsOK bool, wins, losses int, ranked bool) (cs []criterion, rated, hidden bool) {
	vs := func(a, b *float64) string {
		if !ratingsOK {
			return "rating lookup failed"
		}
		return fmt.Sprintf("%s vs your %s", fmtRating(a), fmtRating(b))
	}
	cs = []criterion{
		{"current rating", vs(theirs.Current, mine.Current), ratingsOK && higher(theirs.Current, mine.Current), !ranked},
		{"best season", vs(theirs.Peak, mine.Peak), ratingsOK && higher(theirs.Peak, mine.Peak), !ranked},
		{"past season", vs(theirs.PastPeak, mine.Peak), ratingsOK && higher(theirs.PastPeak, mine.Peak), ranked},
		{"head-to-head", fmt.Sprintf("you %d-%d", wins, losses), losses > wins, true},
	}
	for _, c := range cs[:3] {
		rated = rated || (c.counts && c.met)
	}
	return cs, rated, !rated && cs[3].met
}

// announceOpponent prints every criterion for a new opponent and plays the
// Challenger Approaching jingle if they're rated above you, or the hidden-boss
// sound if they lead you head-to-head without being rated above you.
func announceOpponent(opp string, ranked bool) {
	wins, losses, complete := history.record(opp)
	mine, err := fetchRatings(myCode)
	var theirs Ratings
	if err == nil {
		theirs, err = fetchRatings(opp)
	}
	cs, rated, hidden := assess(mine, theirs, err == nil, wins, losses, ranked)

	mode, other := "unranked", "ranked"
	if ranked {
		mode, other = "ranked", "unranked"
	}
	ratings := fmt.Sprintf("%s (peak %s)", fmtRating(theirs.Current), fmtRating(theirs.Peak))
	if err != nil {
		ratings = fmt.Sprintf("(rating lookup failed: %v)", err)
	}
	tag, sound := "", soundConnect
	switch {
	case rated:
		tag, sound = "  >>> CHALLENGER APPROACHING", soundChallenger
	case hidden:
		tag, sound = "  >>> HIDDEN BOSS", soundHiddenBoss
		if !fileExists(inHere(sound)) {
			sound = soundChallenger
		}
	}
	fmt.Printf("New opponent: %s  %s  %s%s\n", opp, ratings, mode, tag)
	for _, c := range cs {
		mark, note := "no", ""
		if c.met {
			mark = "YES"
		}
		if !c.counts {
			note = "  (" + other + " only)"
		}
		if c.name == "head-to-head" && !complete {
			note = "  (still reading your replays)"
		}
		fmt.Printf("    %-15s %-30s %-3s%s\n", c.name, c.detail, mark, note)
	}
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
					if start, ok := readStart(path); ok {
						seen[path] = true
						var opps []string
						for _, c := range start.Codes {
							if !isMe(c) {
								opps = append(opps, c)
							}
						}
						if len(start.Codes) == 2 && len(opps) == 1 && opps[0] != lastOpp {
							lastOpp = opps[0]
							announceOpponent(lastOpp, start.Ranked)
						}
					}
				}
				if !done[path] && rawLength(path) != 0 { // game just ended
					done[path] = true
					if err := handle(path); err != nil {
						fmt.Printf("%s: error: %v\n", e.Name(), err)
					}
					history.add(path)
					go history.save()
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
	history = loadHistory(myCode)
	go history.scan()
	watch()
}
