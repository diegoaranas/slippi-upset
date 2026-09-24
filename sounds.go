package main

// Downloads the Melee announcer clips into sounds/.
//
// The clips are Nintendo's, so they aren't included in this repo. This fetches the
// community rips from The Sounds Resource (https://sounds.spriters-resource.com),
// pulls out the seven clips, and saves them at reduced volume.

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"
)

const site = "https://sounds.spriters-resource.com"

type clip struct{ src, dst string }

var clipPacks = []struct {
	page  string
	clips []clip
}{
	{"/gamecube/ssbm/asset/394077/", []clip{ // Narrator
		{"nr_1p00.dsp.wav", "new_record.wav"},      // "A new record!"
		{"nr_1p05.dsp.wav", "incredible.wav"},      // "Wow! Incredible!"
		{"nr_1p01.dsp.wav", "congratulations.wav"}, // "Congratulations!"
		{"nr_1p06.dsp.wav", "complete.wav"},        // "Complete!"
		{"nr_1p0a.dsp.wav", "versus.wav"},          // "Versus!"
		{"nr_vs00.dsp.wav", "no_contest.wav"},      // "No contest!"
	}},
	{"/gamecube/ssbm/asset/394097/", []clip{ // Fanfares
		{"s_newcom.hps.wav", "challenger.wav"}, // Challenger Approaching jingle
	}},
}

var zipLink = regexp.MustCompile(`href="(/media/assets/[^"]+\.zip[^"]*)"`)

func fetch(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// scaleWAV multiplies 16-bit PCM samples by volume. Other formats are returned unchanged.
func scaleWAV(wav []byte, volume float64) []byte {
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return wav
	}
	out := bytes.Clone(wav)
	pcm16 := false
	for i := 12; i+8 <= len(out); {
		id, size := string(out[i:i+4]), int(binary.LittleEndian.Uint32(out[i+4:i+8]))
		body := out[i+8 : min(len(out), i+8+size)]
		switch id {
		case "fmt ":
			pcm16 = len(body) >= 16 &&
				binary.LittleEndian.Uint16(body[0:2]) == 1 && binary.LittleEndian.Uint16(body[14:16]) == 16
		case "data":
			if !pcm16 {
				return wav
			}
			for j := 0; j+1 < len(body); j += 2 {
				s := float64(int16(binary.LittleEndian.Uint16(body[j:]))) * volume
				s = max(-32768, min(32767, s))
				binary.LittleEndian.PutUint16(body[j:], uint16(int16(s)))
			}
			return out
		}
		i += 8 + size + size%2 // chunks are word-aligned
	}
	return wav
}

func downloadSounds(outDir string, volume float64) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, pack := range clipPacks {
		html, err := fetch(site + pack.page)
		if err != nil {
			return err
		}
		m := zipLink.FindSubmatch(html)
		if m == nil {
			return fmt.Errorf("couldn't find the download link on %s; the site may have changed", site+pack.page)
		}
		zipURL := site + string(m[1])
		fmt.Printf("Downloading %s ...\n", string(bytes.SplitN([]byte(zipURL), []byte("?"), 2)[0]))
		data, err := fetch(zipURL)
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return err
		}
		byName := map[string]*zip.File{}
		for _, f := range zr.File {
			byName[path.Base(f.Name)] = f
		}
		for _, c := range pack.clips {
			f, ok := byName[c.src]
			if !ok {
				fmt.Printf("  ! %s not found in the pack; skipping %s\n", c.src, c.dst)
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			wav, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(outDir, c.dst), scaleWAV(wav, volume), 0o644); err != nil {
				return err
			}
			fmt.Printf("  saved sounds/%s\n", c.dst)
		}
	}
	fmt.Println("Done.")
	return nil
}
