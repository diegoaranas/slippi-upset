package main

// Reads Melee's audio straight from the user's disc image: the GameCube file system,
// DSP-ADPCM samples, sound banks (.ssm, short effects and voices) and streams (.hps,
// music and jingles).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// discFile is a file's position inside a disc image.
type discFile struct{ off, size int64 }

// discFiles lists the files in a GameCube disc image (.iso), by path ("audio/main.ssm").
func discFiles(r io.ReaderAt) (map[string]discFile, error) {
	head := make([]byte, 0x430)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint32(head[0x1C:]) != 0xC2339F3D {
		return nil, errors.New("not a GameCube disc image (compressed formats like .ciso, .rvz or NKit aren't supported)")
	}
	fstOff, fstSize := int64(binary.BigEndian.Uint32(head[0x424:])), int64(binary.BigEndian.Uint32(head[0x428:]))
	if fstSize < 12 || fstSize > 1<<24 {
		return nil, errors.New("disc image has no valid file table")
	}
	fst := make([]byte, fstSize)
	if _, err := r.ReadAt(fst, fstOff); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(fst[8:12])) // the root entry holds the entry count
	if n*12 > len(fst) {
		return nil, errors.New("disc image has no valid file table")
	}
	names := fst[n*12:]
	type dir struct {
		path string
		end  int // index just past the directory's last entry
	}
	files, stack := map[string]discFile{}, []dir{{"", n}}
	for i := 1; i < n; i++ {
		for len(stack) > 1 && i >= stack[len(stack)-1].end {
			stack = stack[:len(stack)-1]
		}
		e := fst[i*12 : i*12+12]
		nameOff := int(binary.BigEndian.Uint32(e) & 0xFFFFFF)
		if nameOff >= len(names) {
			return nil, errors.New("disc image has no valid file table")
		}
		name := names[nameOff:]
		if j := bytes.IndexByte(name, 0); j >= 0 {
			name = name[:j]
		}
		a, b := binary.BigEndian.Uint32(e[4:]), binary.BigEndian.Uint32(e[8:])
		if e[0] != 0 { // directory
			stack = append(stack, dir{stack[len(stack)-1].path + string(name) + "/", int(b)})
		} else {
			files[stack[len(stack)-1].path+string(name)] = discFile{int64(a), int64(b)}
		}
	}
	return files, nil
}

// readDiscFile reads one file out of a disc image.
func readDiscFile(r io.ReaderAt, files map[string]discFile, path string) ([]byte, error) {
	f, ok := files[path]
	if !ok {
		return nil, fmt.Errorf("%s isn't on the disc", path)
	}
	data := make([]byte, f.size)
	_, err := r.ReadAt(data, f.off)
	return data, err
}

// decodeDSP decodes GameCube DSP-ADPCM: 8-byte frames of one header byte (coefficient
// pair and scale) followed by 14 4-bit samples.
func decodeDSP(data []byte, coefs *[16]int16, hist1, hist2 int32, out []int16) []int16 {
	for f := 0; f+8 <= len(data); f += 8 {
		ps := data[f]
		c1, c2 := int32(coefs[2*(ps>>4&7)]), int32(coefs[2*(ps>>4&7)+1])
		scale := int32(1) << (ps & 0xF)
		for _, b := range data[f+1 : f+8] {
			for _, nib := range [2]int32{int32(b >> 4), int32(b & 0xF)} {
				if nib >= 8 {
					nib -= 16
				}
				s := max(-32768, min(32767, (nib*scale<<11+c1*hist1+c2*hist2+1024)>>11))
				hist2, hist1 = hist1, s
				out = append(out, int16(s))
			}
		}
	}
	return out
}

// nibbleToSample turns a DSP nibble address into a sample index: each 16-nibble
// frame holds 2 header nibbles and 14 samples.
func nibbleToSample(a int) int { return a/16*14 + a%16 - 2 }

// sound is decoded 16-bit PCM, one slice per channel.
type sound struct {
	rate  int
	chans [][]int16
}

func readCoefs(b []byte) *[16]int16 {
	var c [16]int16
	for i := range c {
		c[i] = int16(binary.BigEndian.Uint16(b[2*i:]))
	}
	return &c
}

var errBadAudio = errors.New("unrecognized audio data")

// decodeSSM decodes sound number index from a sound bank (.ssm).
func decodeSSM(d []byte, index int) (sound, error) {
	if len(d) < 16 {
		return sound{}, errBadAudio
	}
	headLen, dataLen, count := int(binary.BigEndian.Uint32(d)), int(binary.BigEndian.Uint32(d[4:])), int(binary.BigEndian.Uint32(d[8:]))
	dataOff := (0x10 + headLen + 0x1F) &^ 0x1F // sample data starts at the next 32-byte boundary
	if index < 0 || index >= count || dataOff+dataLen > len(d) || 0x10+headLen > len(d) {
		return sound{}, errBadAudio
	}
	data := d[dataOff : dataOff+dataLen]
	p := 0x10
	for i := 0; ; i++ {
		if p+8 > 0x10+headLen {
			return sound{}, errBadAudio
		}
		nch, rate := int(binary.BigEndian.Uint32(d[p:])), int(binary.BigEndian.Uint32(d[p+4:]))
		p += 8
		if nch < 1 || nch > 2 || p+0x40*nch > 0x10+headLen {
			return sound{}, errBadAudio
		}
		if i < index {
			p += 0x40 * nch
			continue
		}
		s := sound{rate: rate}
		for c := 0; c < nch; c++ {
			h := d[p : p+0x40]
			p += 0x40
			start, end := int(binary.BigEndian.Uint32(h[4:])), int(binary.BigEndian.Uint32(h[8:]))
			from, to := start/16*8, end/16*8+8
			if start%16 < 2 || end < start || to > len(data) {
				return sound{}, errBadAudio
			}
			hist1, hist2 := int32(int16(binary.BigEndian.Uint16(h[0x34:]))), int32(int16(binary.BigEndian.Uint16(h[0x36:])))
			pcm := decodeDSP(data[from:to], readCoefs(h[0x10:]), hist1, hist2, nil)
			skip := start%16 - 2
			s.chans = append(s.chans, pcm[skip:skip+nibbleToSample(end)-nibbleToSample(start)+1])
		}
		return s, nil
	}
}

// decodeHPS decodes a stream (.hps), once through (a looping stream's loop isn't repeated).
func decodeHPS(d []byte) (sound, error) {
	if len(d) < 0x80 || string(d[:8]) != " HALPST\x00" {
		return sound{}, errBadAudio
	}
	s := sound{rate: int(binary.BigEndian.Uint32(d[8:]))}
	nch := int(binary.BigEndian.Uint32(d[12:]))
	if nch < 1 || nch > 2 {
		return sound{}, errBadAudio
	}
	coefs, ends := make([]*[16]int16, nch), make([]int, nch)
	for c := range nch {
		h := d[0x10+0x38*c:]
		ends[c] = int(binary.BigEndian.Uint32(h[8:]))
		coefs[c] = readCoefs(h[0x10:])
	}
	s.chans = make([][]int16, nch)
	seen := map[int]bool{}
	for off := 0x80; !seen[off] && off >= 0 && off+0x20 <= len(d); {
		seen[off] = true
		size, next := int(binary.BigEndian.Uint32(d[off:])), int(int32(binary.BigEndian.Uint32(d[off+8:])))
		per := size / nch
		if off+0x20+size > len(d) {
			return sound{}, errBadAudio
		}
		for c := range nch {
			hist1 := int32(int16(binary.BigEndian.Uint16(d[off+14+8*c:])))
			hist2 := int32(int16(binary.BigEndian.Uint16(d[off+16+8*c:])))
			s.chans[c] = decodeDSP(d[off+0x20+per*c:off+0x20+per*(c+1)], coefs[c], hist1, hist2, s.chans[c])
		}
		off = next // the last block points back to the loop start, or is -1
	}
	for c := range nch {
		s.chans[c] = s.chans[c][:min(len(s.chans[c]), nibbleToSample(ends[c])+1)]
	}
	return s, nil
}

// wav encodes the sound as a 16-bit PCM .wav file, scaled by volume.
func (s sound) wav(volume float64) []byte {
	nch, n := len(s.chans), len(s.chans[0])
	le := binary.LittleEndian
	b := make([]byte, 0, 44+n*nch*2)
	b = append(b, "RIFF"...)
	b = le.AppendUint32(b, uint32(36+n*nch*2))
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 16)                   // fmt chunk size
	b = le.AppendUint16(b, 1)                    // PCM
	b = le.AppendUint16(b, uint16(nch))          // channels
	b = le.AppendUint32(b, uint32(s.rate))       // sample rate
	b = le.AppendUint32(b, uint32(s.rate*nch*2)) // bytes per second
	b = le.AppendUint16(b, uint16(nch*2))        // bytes per frame
	b = le.AppendUint16(b, 16)                   // bits per sample
	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(n*nch*2))
	for i := range n {
		for _, ch := range s.chans {
			b = le.AppendUint16(b, uint16(int16(max(-32768, min(32767, float64(ch[i])*volume)))))
		}
	}
	return b
}

// isoClip is a sound to pull from the disc: sound number index of a .ssm bank, or a .hps stream.
type isoClip struct {
	file  string
	index int
	dst   string
}

var isoClips = []isoClip{
	{"audio/us/nr_1p.ssm", 0x00, "new_record.wav"},      // "A new record!"
	{"audio/us/nr_1p.ssm", 0x05, "incredible.wav"},      // "Wow! Incredible!"
	{"audio/us/nr_1p.ssm", 0x01, "congratulations.wav"}, // "Congratulations!"
	{"audio/us/nr_1p.ssm", 0x06, "complete.wav"},        // "Complete!"
	{"audio/us/nr_1p.ssm", 0x0A, "versus.wav"},          // "Versus!"
	{"audio/us/nr_vs.ssm", 0x00, "no_contest.wav"},      // "No contest!"
	{"audio/s_newcom.hps", 0, "challenger.wav"},         // Challenger Approaching jingle
	{"audio/vl_last_v2.hps", 0, "hidden_boss.wav"},      // Adventure: Bowser's trophy breaking into Giga Bowser
}

// extractSounds writes the announcer clips from the user's Melee disc image to outDir.
func extractSounds(isoPath, outDir string, volume float64) error {
	f, err := os.Open(isoPath)
	if err != nil {
		return err
	}
	defer f.Close()
	files, err := discFiles(f)
	if err != nil {
		return fmt.Errorf("%s: %w", isoPath, err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	fmt.Printf("Extracting the sounds from %s ...\n", isoPath)
	cache := map[string][]byte{}
	for _, c := range isoClips {
		path := c.file
		if _, ok := files[path]; !ok { // non-US discs keep the narrator's banks in audio/, not audio/us/
			path = strings.Replace(path, "audio/us/", "audio/", 1)
		}
		data, ok := cache[path]
		if !ok {
			if data, err = readDiscFile(f, files, path); err != nil {
				return err
			}
			cache[path] = data
		}
		var s sound
		if strings.HasSuffix(path, ".hps") {
			s, err = decodeHPS(data)
		} else {
			s, err = decodeSSM(data, c.index)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, c.dst), s.wav(volume), 0o644); err != nil {
			return err
		}
		fmt.Printf("  saved sounds/%s\n", c.dst)
	}
	fmt.Println("Done.")
	return nil
}
