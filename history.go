package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// historyFile caches the result of every replay in the replay folder, so the
// head-to-head record only has to read replays it hasn't seen before.
const historyFile = "history.json"

// histGame is one replay's result from your side. Opp is "" for replays that aren't
// your 1v1 netplay games; they're kept so they aren't read again.
type histGame struct {
	Opp    string `json:"o,omitempty"`
	Result int8   `json:"r,omitempty"` // 1 = you won, -1 = you lost, 0 = no result
}

// History is your head-to-head record against every opponent in your replays.
type History struct {
	mu    sync.Mutex
	me    string
	games map[string]histGame // by path relative to the replay folder, with forward slashes
	ready bool                // the first scan has finished
}

type historyJSON struct {
	Me    string              `json:"me"`
	Games map[string]histGame `json:"games"`
}

var history = &History{games: map[string]histGame{}}

// loadHistory reads history.json, discarding it if it belongs to another connect code.
func loadHistory(me string) *History {
	h := &History{me: me, games: map[string]histGame{}}
	var saved historyJSON
	if data, err := os.ReadFile(inHere(historyFile)); err == nil && json.Unmarshal(data, &saved) == nil &&
		strings.EqualFold(saved.Me, me) && saved.Games != nil {
		h.games = saved.Games
	}
	return h
}

func (h *History) save() {
	h.mu.Lock()
	data, err := json.Marshal(historyJSON{Me: h.me, Games: h.games})
	h.mu.Unlock()
	if err != nil {
		return
	}
	tmp := inHere(historyFile + ".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err == nil {
		err = os.Rename(tmp, inHere(historyFile))
	}
	if err != nil {
		fmt.Printf("couldn't save %s: %v\n", historyFile, err)
	}
}

func histKey(path string) string {
	rel, err := filepath.Rel(replayDir, path)
	if err != nil {
		rel = path
	}
	return filepath.ToSlash(rel)
}

// result turns a parsed game into your side of it.
func (h *History) result(g Game) histGame {
	me, opp := -1, ""
	for p, c := range g.Codes {
		if strings.EqualFold(c, h.me) {
			me = p
		}
	}
	for p, c := range g.Codes {
		if p != me && c != "" {
			opp = c
		}
	}
	if me == -1 || opp == "" || len(g.Codes) != 2 {
		return histGame{}
	}
	hg := histGame{Opp: opp}
	switch {
	case g.Winner == me:
		hg.Result = 1
	case g.Winner != -1:
		hg.Result = -1
	}
	return hg
}

// add records a finished replay. Unreadable replays are recorded as "not yours"
// so they aren't retried every run.
func (h *History) add(path string) {
	g, _ := quickGame(path)
	hg := h.result(g)
	h.mu.Lock()
	h.games[histKey(path)] = hg
	h.mu.Unlock()
}

// scan reads every finished replay not in the cache yet, then saves the cache.
// Meant to run in the background: lookups work meanwhile, on what's been read so far.
func (h *History) scan() {
	var todo []string
	filepath.WalkDir(replayDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".slp") {
			h.mu.Lock()
			_, known := h.games[histKey(path)]
			h.mu.Unlock()
			if !known && rawLength(path) != 0 { // skip a game in progress; it's added when it ends
				todo = append(todo, path)
			}
		}
		return nil
	})
	if len(todo) > 200 {
		fmt.Printf("Reading %d replays for your head-to-head records (one time, in the background)...\n", len(todo))
	}
	for i, path := range todo {
		h.add(path)
		if i%1000 == 999 {
			h.save()
		}
	}
	h.mu.Lock()
	h.ready = true
	h.mu.Unlock()
	if len(todo) > 0 {
		h.save()
	}
	if len(todo) > 200 {
		fmt.Println("Head-to-head records ready.")
	}
}

// record returns your wins and losses against opp, and whether the first scan
// has finished (if not, the counts may be incomplete).
func (h *History) record(opp string) (wins, losses int, ready bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, g := range h.games {
		if strings.EqualFold(g.Opp, opp) {
			switch g.Result {
			case 1:
				wins++
			case -1:
				losses++
			}
		}
	}
	return wins, losses, h.ready
}
