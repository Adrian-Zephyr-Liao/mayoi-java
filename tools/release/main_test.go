package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseArchivesInstallable(t *testing.T) {
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	})
	files := map[string]string{
		"plugin.json": `{"name":"mayoi-java","main":"main.ts"}`,
		"main.ts":     "export function activate() {}\n",
		"LICENSE":     "test license\n",
		"README.md":   "installation instructions\n",
	}
	for name, data := range files {
		if err := os.WriteFile(name, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, windows := range []bool{false, true} {
		name, extension := "mayoi-java", ".tar.gz"
		if windows {
			name, extension = "mayoi-java.exe", ".zip"
		}
		t.Run(extension, func(t *testing.T) {
			binary := filepath.Join(root, name)
			if err := os.WriteFile(binary, []byte("test executable\n"), 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "release"+extension)
			if err := archive(path, binary, windows); err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			if windows {
				r, err := zip.OpenReader(path)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				for _, f := range r.File {
					entry, err := f.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(entry)
					entry.Close()
					if err != nil {
						t.Fatal(err)
					}
					got[f.Name] = string(data)
				}
			} else {
				f, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				gz, err := gzip.NewReader(f)
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				r := tar.NewReader(gz)
				for {
					h, err := r.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if h.Name == name && h.Mode != 0755 {
						t.Fatalf("executable mode = %o", h.Mode)
					}
					data, err := io.ReadAll(r)
					if err != nil {
						t.Fatal(err)
					}
					got[h.Name] = string(data)
				}
			}
			if len(got) != len(files)+1 || got[name] != "test executable\n" {
				t.Fatalf("archive files: %v", got)
			}
			for path, data := range got {
				if strings.ContainsAny(path, `/\\`) {
					t.Fatalf("unexpected archive path %q", path)
				}
				if path != name && files[path] != data {
					t.Fatalf("unexpected content in %q", path)
				}
			}
		})
	}
}
