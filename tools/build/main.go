// Command build compiles release binaries for every platform into dist/.
//
//	go run ./tools/build <version>
//
// Run it from the repo root. semantic-release runs it with the new version
// (.releaserc.yaml). Don't set GOOS/GOARCH when running it: it sets them per target.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var targets = []struct{ os, arch string }{
	{"windows", "amd64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
}

func main() {
	log.SetFlags(0)
	version := "dev"
	if len(os.Args) > 1 {
		version = os.Args[1]
	}
	must(os.RemoveAll("dist"))
	must(os.Mkdir("dist", 0o755))

	for _, t := range targets {
		name := "dist/upset_" + t.os + "_" + t.arch
		if t.os == "windows" {
			build(t.os, t.arch, version, name+".exe") // bare .exe, so the README can link to it
			continue
		}
		build(t.os, t.arch, version, "dist/upset")
		tarGz(name+".tar.gz", "dist/upset") // tar keeps the executable bit
		must(os.Remove("dist/upset"))
	}
	checksums("dist")
}

func build(goos, goarch, version, out string) {
	// -trimpath: no build-machine paths in the binary. -s -w: strip debug info, ~30% smaller.
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", out, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	must(cmd.Run())
	fmt.Println("built", out)
}

func tarGz(dst, src string) {
	data, err := os.ReadFile(src)
	must(err)
	f, err := os.Create(dst)
	must(err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	// A hand-made header, so the archive doesn't record the build machine's user and group.
	must(tw.WriteHeader(&tar.Header{Name: "upset", Mode: 0o755, Size: int64(len(data)), ModTime: time.Now()}))
	_, err = tw.Write(data)
	must(err)
	must(tw.Close())
	must(gz.Close())
	must(f.Close())
	fmt.Println("packed", dst)
}

func checksums(dir string) {
	entries, err := os.ReadDir(dir)
	must(err)
	var out strings.Builder
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		must(err)
		fmt.Fprintf(&out, "%x  %s\n", sha256.Sum256(data), e.Name())
	}
	must(os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(out.String()), 0o644))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
