package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const lombokVersion = "1.18.48"
const lombokSHA256 = "85477a4655ebb2c074a9099cfb749be454449fee564d4282610df1b85f7c508b"

func lombokPath() string { return filepath.Join(storageDir(), "tools", "lombok-"+lombokVersion+".jar") }

func downloadVerified(client *http.Client, address, checksum, path string) error {
	response, err := client.Get(address)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("download exceeds 8 MiB")
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != checksum {
		return fmt.Errorf("download SHA-256 does not match the pinned release")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "download-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Windows cannot rename over an existing file. A verified existing copy is
	// already complete; otherwise leave it untouched and request removal.
	if err = os.Rename(tmp.Name(), path); err != nil {
		existing, readErr := os.ReadFile(path)
		if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(existing)) != checksum {
			return err
		}
	}
	return nil
}

func installLombok() error {
	if data, err := os.ReadFile(lombokPath()); err == nil && fmt.Sprintf("%x", sha256.Sum256(data)) == lombokSHA256 {
		return nil
	}
	address := "https://repo.maven.apache.org/maven2/org/projectlombok/lombok/" + lombokVersion + "/lombok-" + lombokVersion + ".jar"
	return downloadVerified(&http.Client{Timeout: 60 * time.Second}, address, lombokSHA256, lombokPath())
}
