package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gameStartSize = 0x221 + 0xA*4
	postFrameSize = 0x22
	gameEndSize   = 6
)

func postFrame(port, stocks byte, pct float32) []byte {
	ev := make([]byte, 1+postFrameSize)
	ev[0], ev[5], ev[6] = 0x38, port, 0
	binary.BigEndian.PutUint32(ev[0x16:], math.Float32bits(pct))
	ev[0x21] = stocks
	return ev
}

// buildReplay writes a minimal 1v1 replay between ports 0 and 1.
func buildReplay(t *testing.T, stocks0, stocks1 byte, pct0, pct1 float32, end []byte) string {
	t.Helper()
	var raw bytes.Buffer
	sizes := [][3]byte{{0x36, 0, 0}, {0x38, 0, 0}, {0x39, 0, 0}}
	binary.BigEndian.PutUint16(sizes[0][1:], gameStartSize)
	binary.BigEndian.PutUint16(sizes[1][1:], postFrameSize)
	binary.BigEndian.PutUint16(sizes[2][1:], uint16(len(end)))
	raw.Write([]byte{0x35, byte(1 + 3*len(sizes))})
	for _, s := range sizes {
		raw.Write(s[:])
	}
	gs := make([]byte, 1+gameStartSize)
	gs[0] = 0x36
	copy(gs[0x221:], "ABCD\x81\x94123") // Shift-JIS fullwidth '#'
	copy(gs[0x221+0xA:], "EFGH\x81\x94456")
	raw.Write(gs)
	raw.Write(postFrame(0, stocks0, pct0))
	raw.Write(postFrame(1, stocks1, pct1))
	raw.Write(append([]byte{0x39}, end...))

	var f bytes.Buffer
	f.Write(header)
	binary.Write(&f, binary.BigEndian, uint32(raw.Len()))
	f.Write(raw.Bytes())
	f.WriteString("U\x08metadata{}}")

	path := filepath.Join(t.TempDir(), "Game.slp")
	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseGame(t *testing.T) {
	cases := []struct {
		name            string
		s0, s1          byte
		p0, p1          float32
		end             []byte
		winner, quitter int
	}{
		{"placements", 1, 1, 50, 20, []byte{2, 0xFF, 1, 0, 0xFF, 0xFF}, 1, -1},
		{"stocks, old format", 2, 0, 0, 0, []byte{2}, 0, -1},
		{"timeout by percent", 1, 1, 80, 30, []byte{1, 0xFF}, 1, -1},
		{"quit", 3, 3, 0, 0, []byte{7, 1, 0xFF, 0xFF, 0xFF, 0xFF}, -1, 1},
		{"no result", 1, 1, 30, 30, []byte{1, 0xFF}, -1, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := buildReplay(t, c.s0, c.s1, c.p0, c.p1, c.end)
			g, err := parseGame(path)
			if err != nil {
				t.Fatal(err)
			}
			if g.Codes[0] != "ABCD#123" || g.Codes[1] != "EFGH#456" {
				t.Errorf("codes = %v", g.Codes)
			}
			if g.Winner != c.winner || g.Quitter != c.quitter {
				t.Errorf("winner, quitter = %d, %d; want %d, %d", g.Winner, g.Quitter, c.winner, c.quitter)
			}
			if rawLength(path) == 0 {
				t.Error("rawLength = 0 for a finished replay")
			}
			codes := startCodes(path)
			if len(codes) != 2 || codes[0] != "ABCD#123" || codes[1] != "EFGH#456" {
				t.Errorf("startCodes = %v", codes)
			}
		})
	}
}

func TestScaleWAV(t *testing.T) {
	wav := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00data\x04\x00\x00\x00")
	wav = binary.LittleEndian.AppendUint16(wav, 1000)
	wav = binary.LittleEndian.AppendUint16(wav, uint16(0x10000-1000)) // -1000
	out := scaleWAV(wav, 0.5)
	n := len(out)
	if a, b := int16(binary.LittleEndian.Uint16(out[n-4:])), int16(binary.LittleEndian.Uint16(out[n-2:])); a != 500 || b != -500 {
		t.Errorf("samples = %d, %d; want 500, -500", a, b)
	}
}

func TestReplayDirs(t *testing.T) {
	replayDir = t.TempDir()
	for _, d := range []string{"2026-07", "2026-08", "2026-08-Mainline", "2026-09-Mainline", "Spectate"} {
		if err := os.Mkdir(filepath.Join(replayDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := replayDirs()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range dirs[1:] {
		got = append(got, filepath.Base(d))
	}
	if want := "2026-08 2026-08-Mainline 2026-09-Mainline"; strings.Join(got, " ") != want || dirs[0] != replayDir {
		t.Errorf("replayDirs = %v; want root + %s", dirs, want)
	}
}
