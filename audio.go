package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// The speaker runs at one fixed rate; clips at other rates are resampled to it.
const speakerRate = beep.SampleRate(48000)

var (
	speakerOnce sync.Once
	speakerErr  error
)

var decoders = map[string]func(io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error){
	".wav":  func(r io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error) { return wav.Decode(r) },
	".flac": func(r io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error) { return flac.Decode(r) },
	".mp3":  mp3.Decode,
	".ogg":  vorbis.Decode,
}

// playFile plays a .wav, .mp3, .ogg or .flac without blocking.
func playFile(path string) {
	decode, ok := decoders[strings.ToLower(filepath.Ext(path))]
	if !ok {
		fmt.Printf("  (no sound: %s isn't a .wav, .mp3, .ogg or .flac)\n", path)
		return
	}
	// Opened lazily so the audio device is only held once there's something to play.
	speakerOnce.Do(func() { speakerErr = speaker.Init(speakerRate, speakerRate.N(100*time.Millisecond)) })
	if speakerErr != nil {
		fmt.Printf("  (no sound: %v)\n", speakerErr)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("  (no sound: %v)\n", err)
		return
	}
	stream, format, err := decode(f)
	if err != nil {
		f.Close()
		fmt.Printf("  (no sound: %s: %v)\n", filepath.Base(path), err)
		return
	}
	speaker.Play(beep.Seq(
		beep.Resample(4, format.SampleRate, speakerRate, stream),
		beep.Callback(func() { stream.Close() }),
	))
}
