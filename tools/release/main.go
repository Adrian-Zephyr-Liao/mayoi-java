// Build portable release archives with only the Go toolchain.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func archive(path, binary string, windows bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	files := []string{binary, "LICENSE", "README.md"}
	if windows {
		w := zip.NewWriter(f)
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry, err := w.Create(filepath.Base(path))
			if err != nil {
				return err
			}
			if _, err = entry.Write(data); err != nil {
				return err
			}
		}
		return w.Close()
	}
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := int64(0644)
		if path == binary {
			mode = 0755
		}
		if err = w.WriteHeader(&tar.Header{Name: filepath.Base(path), Mode: mode, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err = w.Write(data); err != nil {
			return err
		}
	}
	if err = w.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func run() error {
	if len(os.Args) != 2 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(os.Args[1]) {
		return fmt.Errorf("usage: go run ./tools/release VERSION")
	}
	version := os.Args[1]
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	var checksums strings.Builder
	for _, target := range []string{"darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64", "windows/amd64"} {
		parts := strings.Split(target, "/")
		windows := parts[0] == "windows"
		tmp, err := os.MkdirTemp("", "mayoi-java-release-")
		if err != nil {
			return err
		}
		name := "mayoi-java"
		extension := ".tar.gz"
		if windows {
			name += ".exe"
			extension = ".zip"
		}
		binary := filepath.Join(tmp, name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", binary, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			os.RemoveAll(tmp)
			return err
		}
		name = fmt.Sprintf("mayoi-java_%s_%s_%s%s", version, parts[0], parts[1], extension)
		path := filepath.Join("dist", name)
		err = archive(path, binary, windows)
		os.RemoveAll(tmp)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		sum := sha256.New()
		_, err = io.Copy(sum, f)
		f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(&checksums, "%x  %s\n", sum.Sum(nil), name)
		fmt.Println(path)
	}
	return os.WriteFile("dist/SHA256SUMS", []byte(checksums.String()), 0644)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
