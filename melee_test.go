package main

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

var be = binary.BigEndian

// dspFrame is one DSP-ADPCM frame using coefficient pair 0 at scale 1: with zero
// coefficients, each sample is just its 4-bit value.
var dspFrame = []byte{0x00, 0x12, 0x3F, 0x80, 0x7E, 0x00, 0x00, 0x01}
var dspSamples = []int16{1, 2, 3, -1, -8, 0, 7, -2, 0, 0, 0, 0, 0, 1}

func TestDecodeDSP(t *testing.T) {
	if got := decodeDSP(dspFrame, &[16]int16{}, 0, 0, nil); !slices.Equal(got, dspSamples) {
		t.Errorf("decodeDSP = %v; want %v", got, dspSamples)
	}
}

func TestDiscFiles(t *testing.T) {
	img := make([]byte, 0x500)
	be.PutUint32(img[0x1C:], 0xC2339F3D)
	be.PutUint32(img[0x424:], 0x440) // file table offset
	be.PutUint32(img[0x428:], uint32(3*12+len("\x00audio\x00a.hps\x00")))
	fst := img[0x440:]
	be.PutUint32(fst[0:], 0x01000000) // root directory
	be.PutUint32(fst[8:], 3)          // entry count
	be.PutUint32(fst[12:], 0x01000001)
	be.PutUint32(fst[20:], 3) // "audio" holds entries up to 3
	be.PutUint32(fst[24:], 7) // "a.hps"
	be.PutUint32(fst[28:], 0x4F0)
	be.PutUint32(fst[32:], 4)
	copy(fst[36:], "\x00audio\x00a.hps\x00")
	copy(img[0x4F0:], "data")

	r := bytes.NewReader(img)
	files, err := discFiles(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readDiscFile(r, files, "audio/a.hps"); err != nil || string(got) != "data" {
		t.Errorf("audio/a.hps = %q, %v; files = %v", got, err, files)
	}
	if _, err := discFiles(bytes.NewReader(make([]byte, 0x500))); err == nil {
		t.Error("discFiles accepted an image without the GameCube magic number")
	}
}

func TestDecodeSSM(t *testing.T) {
	// One mono sound covering the frame's 14 samples: nibbles 2 (after the header) to 15.
	d := make([]byte, 0x60+8)
	be.PutUint32(d[0:], 8+0x40) // header length
	be.PutUint32(d[4:], 8)      // data length
	be.PutUint32(d[8:], 1)      // sound count
	be.PutUint32(d[0x10:], 1)
	be.PutUint32(d[0x14:], 12000)
	be.PutUint32(d[0x18+4:], 2)  // start nibble
	be.PutUint32(d[0x18+8:], 15) // end nibble
	copy(d[0x60:], dspFrame)     // data starts at the 32-byte boundary after 0x10+0x48
	s, err := decodeSSM(d, 0)
	if err != nil || s.rate != 12000 || len(s.chans) != 1 || !slices.Equal(s.chans[0], dspSamples) {
		t.Errorf("decodeSSM = %+v, %v", s, err)
	}
	if _, err := decodeSSM(d, 1); err == nil {
		t.Error("decodeSSM accepted an out-of-range sound index")
	}
}

func TestDecodeHPS(t *testing.T) {
	d := make([]byte, 0x80+0x20+8)
	copy(d, " HALPST\x00")
	be.PutUint32(d[8:], 32000)
	be.PutUint32(d[12:], 1)    // channels
	be.PutUint32(d[0x18:], 15) // end nibble
	be.PutUint32(d[0x80:], 8)  // block data size
	be.PutUint32(d[0x88:], 0xFFFFFFFF)
	copy(d[0xA0:], dspFrame)
	s, err := decodeHPS(d)
	if err != nil || s.rate != 32000 || !slices.Equal(s.chans[0], dspSamples) {
		t.Errorf("decodeHPS = %+v, %v", s, err)
	}

	w := s.wav(0.5)
	if string(w[:4]) != "RIFF" || string(w[36:40]) != "data" || be.Uint16(w[44:]) != 0 ||
		int16(binary.LittleEndian.Uint16(w[44+2*2:])) != 1 { // third sample: 3 * 0.5 -> 1
		t.Errorf("wav header or samples wrong: % x", w[:52])
	}
}
