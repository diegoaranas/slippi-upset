package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
)

var header = []byte("{U\x03raw[$U#l")

// rawLength is 0 while the game is still being written; Slippi fills it in when the game ends.
func rawLength(path string) uint32 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	head := make([]byte, 15)
	if _, err := io.ReadFull(f, head); err != nil || !bytes.HasPrefix(head, header) {
		return 0
	}
	return binary.BigEndian.Uint32(head[11:15])
}

// payloadSizes reads the Event Payloads event at the start of the raw stream.
// Returns the size of each command and where the next event starts.
func payloadSizes(raw []byte) (map[byte]int, int) {
	if len(raw) < 2 || raw[0] != 0x35 {
		return nil, 0
	}
	blen := int(raw[1])
	sizes := map[byte]int{}
	for j := 2; j+2 < len(raw) && j < 1+blen; j += 3 {
		sizes[raw[j]] = int(binary.BigEndian.Uint16(raw[j+1 : j+3]))
	}
	return sizes, 1 + blen
}

// gameStartCodes returns connect codes by port from a Game Start event,
// or nil if the event is incomplete.
func gameStartCodes(gs []byte) map[int]string {
	if len(gs) < 0x221+0xA*4 || gs[0] != 0x36 {
		return nil
	}
	codes := map[int]string{}
	for port := 0; port < 4; port++ {
		field := gs[0x221+0xA*port : 0x22B+0xA*port]
		if i := bytes.IndexByte(field, 0); i >= 0 {
			field = field[:i]
		}
		if len(field) > 0 {
			codes[port] = decodeCode(field)
		}
	}
	return codes
}

// decodeCode decodes a Shift-JIS connect code. Codes are ASCII apart from the
// fullwidth '#' (0x81 0x94), so that's all this handles.
func decodeCode(b []byte) string {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == 0x81 && i+1 < len(b) && b[i+1] == 0x94:
			out = append(out, '#')
			i++
		case b[i] < 0x80:
			out = append(out, b[i])
		default:
			out = append(out, '?')
		}
	}
	return string(out)
}

// matchIDOffset is where Game Start stores the online match ID ("mode.ranked-...",
// "mode.unranked-...", ...). Replays from before Slippi 3.14 don't have it.
const matchIDOffset = 0x2BE

// gameStartRanked reports whether a Game Start event is from a Ranked match.
func gameStartRanked(gs []byte) bool {
	return len(gs) > matchIDOffset && bytes.HasPrefix(gs[matchIDOffset:], []byte("mode.ranked"))
}

// Start is what the Game Start event tells us as soon as a game begins.
type Start struct {
	Codes  map[int]string
	Ranked bool
}

// readStart reads the Game Start event, which Slippi writes as soon as the game
// begins. ok is false if it isn't on disk yet.
func readStart(path string) (s Start, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return s, false
	}
	defer f.Close()
	head := make([]byte, 2048)
	n, _ := io.ReadFull(f, head)
	if n < 15 {
		return s, false
	}
	raw := head[15:n]
	sizes, pos := payloadSizes(raw)
	if sizes == nil || pos >= len(raw) {
		return s, false
	}
	gs := raw[pos:min(len(raw), pos+1+sizes[0x36])]
	if s.Codes = gameStartCodes(gs); s.Codes == nil {
		return s, false
	}
	s.Ranked = gameStartRanked(gs)
	return s, true
}

// quickGame returns the same result as parseGame, but for most replays reads only
// the first 2 KB (connect codes) and the Game End event at the end of the raw stream.
// Replays without placements in Game End fall back to the full parse.
func quickGame(path string) (Game, error) {
	f, err := os.Open(path)
	if err != nil {
		return Game{Winner: -1, Quitter: -1}, err
	}
	defer f.Close()
	head := make([]byte, 2048)
	n, _ := io.ReadFull(f, head)
	if n < 15 || !bytes.HasPrefix(head, header) {
		return Game{Winner: -1, Quitter: -1}, errors.New("not a Slippi replay")
	}
	rawLen := int(binary.BigEndian.Uint32(head[11:15]))
	raw := head[15:n]
	sizes, pos := payloadSizes(raw)
	endSize, ok := sizes[0x39]
	if !ok || endSize < 6 || rawLen < 1+endSize || pos >= len(raw) {
		return parseGame(path)
	}
	end := make([]byte, 1+endSize)
	if _, err := f.ReadAt(end, int64(15+rawLen-1-endSize)); err != nil || end[0] != 0x39 {
		return parseGame(path)
	}
	g := Game{Codes: gameStartCodes(raw[pos:min(len(raw), pos+1+sizes[0x36])]), Winner: -1, Quitter: -1}
	if len(g.Codes) != 2 {
		return parseGame(path) // only 1v1s are worth the shortcut; let parseGame decide the rest
	}
	if g.Quitter = int(int8(end[2])); g.Quitter != -1 {
		return g, nil
	}
	for port := range g.Codes {
		if port < 4 && int8(end[3+port]) == 0 {
			g.Winner = port
			return g, nil
		}
	}
	return parseGame(path) // no placement winner (e.g. a timeout): compare stocks and percent
}

// Game is the outcome of a finished replay. Winner and Quitter are -1 when there is none.
type Game struct {
	Codes   map[int]string
	Winner  int
	Quitter int
}

func parseGame(path string) (Game, error) {
	g := Game{Winner: -1, Quitter: -1}
	data, err := os.ReadFile(path)
	if err != nil {
		return g, err
	}
	if len(data) < 15 || !bytes.HasPrefix(data, header) {
		return g, errors.New("not a Slippi replay")
	}
	n := int(binary.BigEndian.Uint32(data[11:15]))
	if 15+n > len(data) {
		return g, errors.New("replay is truncated")
	}
	raw := data[15 : 15+n]

	// event stream -> connect codes, stocks and game-end info
	sizes, pos := payloadSizes(raw)
	stocks, percent := map[int]int{}, map[int]float32{}
	var ports []int // in first-seen order
	var end []byte
	for pos < len(raw) {
		cmd := raw[pos]
		size, ok := sizes[cmd]
		if !ok || pos+1+size > len(raw) {
			break
		}
		ev := raw[pos : pos+1+size]
		switch {
		case cmd == 0x36:
			g.Codes = gameStartCodes(ev)
		case cmd == 0x38 && len(ev) > 0x21 && ev[6] == 0: // post-frame, non-follower (ignore Nana)
			port := int(ev[5])
			if _, seen := stocks[port]; !seen {
				ports = append(ports, port)
			}
			percent[port] = math.Float32frombits(binary.BigEndian.Uint32(ev[0x16:0x1A]))
			stocks[port] = int(ev[0x21])
		case cmd == 0x39:
			end = ev[1:]
		}
		pos += 1 + size
	}

	if end == nil || len(ports) != 2 {
		return g, nil // crashed/doubles/etc.
	}
	if len(end) >= 2 {
		g.Quitter = int(int8(end[1]))
	}
	if g.Quitter != -1 {
		return g, nil // someone quit (LRAS): no winner
	}
	if len(end) >= 6 { // newer replays store placements directly
		for _, port := range ports {
			if port < 4 && int8(end[2+port]) == 0 {
				g.Winner = port
				return g, nil
			}
		}
	}
	a, b := ports[0], ports[1]
	switch {
	case stocks[a] != stocks[b]:
		g.Winner = pick(stocks[a] > stocks[b], a, b)
	case percent[a] != percent[b]: // timeout with equal stocks
		g.Winner = pick(percent[a] < percent[b], a, b)
	}
	return g, nil
}

func pick(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}
