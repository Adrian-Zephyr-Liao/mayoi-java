package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0700); err != nil {
		t.Fatal(err)
	}
}

func isolated(t *testing.T) {
	t.Helper()
	t.Setenv("MAYOI_DATA_DIR", t.TempDir())
	for _, key := range []string{"MAYOI_JAVA_HOME", "MAYOI_JAVA_JDTLS_HOME", "MAYOI_JAVA_CACHE_DIR", "MAYOI_JAVA_MAX_HEAP_MIB"} {
		t.Setenv(key, "")
	}
}

func TestConfigurationPrecedence(t *testing.T) {
	isolated(t)
	root := t.TempDir()
	data, _ := json.Marshal(map[string]any{"maxHeapMiB": 1024, "workspaces": map[string]any{root: map[string]any{"maxHeapMiB": 4096, "autoBuild": false, "annotationProcessing": false}}})
	writeFile(t, filepath.Join(storageDir(), "config.json"), string(data))
	o, err := loadOptions(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *o.MaxHeapMiB != 4096 || enabled(o.AutoBuild) || enabled(o.AnnotationProcessing) {
		t.Fatalf("workspace override: %+v", o)
	}
	o, err = loadOptions(t.TempDir(), nil)
	if err != nil || !enabled(o.AutoBuild) || !enabled(o.AnnotationProcessing) {
		t.Fatal("compatibility settings leaked", o, err)
	}
	o, err = loadOptions(root, map[string]json.RawMessage{"maxHeapMiB": json.RawMessage(`3072`), "helperPath": json.RawMessage(`"/a helper"`)})
	if err != nil || *o.MaxHeapMiB != 3072 {
		t.Fatal(o, err)
	}
	t.Setenv("MAYOI_JAVA_MAX_HEAP_MIB", "5120")
	o, err = loadOptions(root, nil)
	if err != nil || *o.MaxHeapMiB != 5120 {
		t.Fatal(o, err)
	}
	t.Setenv("MAYOI_JAVA_MAX_HEAP_MIB", "true")
	if _, err = loadOptions(root, nil); err == nil {
		t.Fatal("accepted invalid heap")
	}
}

func TestRejectInvalidConfiguration(t *testing.T) {
	isolated(t)
	root := t.TempDir()
	for _, data := range []string{`null`, `{"unknown":1}`, `{"autoBuild":"false"}`, `{"maxHeapMiB":64}`, `{"javaHome":"relative"}`} {
		writeFile(t, filepath.Join(storageDir(), "config.json"), data)
		if _, err := loadOptions(root, nil); err == nil {
			t.Fatal("accepted", data)
		}
	}
}

func TestWorkspaceAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege not assumed")
	}
	root := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "pom.xml"), "<project/>")
	if err := os.Symlink(repo, filepath.Join(root, "a 仓库 #1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, filepath.Join(root, "z-duplicate")); err != nil {
		t.Fatal(err)
	}
	folders, mappings, err := workspace(root)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := canonical(repo)
	if len(folders) != 1 || len(mappings) != 2 || folders[0].URI != fileURI(real) {
		t.Fatalf("folders %v mappings %v", folders, mappings)
	}
	if !strings.Contains(mappings[1].ClientURI, "%23") {
		t.Fatal("URI not escaped", mappings)
	}
}

func TestLaunchDescription(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Java executable uses a POSIX script")
	}
	isolated(t)
	root := t.TempDir()
	jdk := t.TempDir()
	jdtls := t.TempDir()
	writeFile(t, filepath.Join(root, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(jdk, "bin/java"), "#!/bin/sh\ntest -z \"$JAVA_TOOL_OPTIONS\" || exit 1\nprintf 'openjdk version \"26.0.1\"\\n' >&2\n")
	t.Setenv("JAVA_TOOL_OPTIONS", "-a-project-specific-option")
	writeFile(t, filepath.Join(jdtls, "plugins/org.eclipse.equinox.launcher_1.jar"), "")
	source := filepath.Join(jdtls, platformConfig(runtime.GOOS, runtime.GOARCH), "config.ini")
	writeFile(t, source, "eclipse.product=test\n")
	t.Setenv("MAYOI_JAVA_HOME", jdk)
	t.Setenv("MAYOI_JAVA_JDTLS_HOME", jdtls)
	plan, err := resolve(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := canonical(filepath.Join(jdk, "bin/java"))
	if plan.Command != real || !strings.Contains(strings.Join(plan.Args, " "), "-Xmx2048m") {
		t.Fatal(plan)
	}
	java := plan.Configuration["java"].(map[string]any)
	if java["autobuild"].(map[string]any)["enabled"] != true || java["settings"] != nil {
		t.Fatal("defaults", java)
	}
	writeFile(t, lombokPath(), "test agent")
	settings := map[string]json.RawMessage{"annotationProcessing": json.RawMessage(`false`)}
	plan, err = resolve(root, settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Args, " "), "-javaagent:") || !strings.HasSuffix(plan.Args[len(plan.Args)-1], "data-v3-no-apt") {
		t.Fatal(plan)
	}
	if data, _ := os.ReadFile(source); string(data) != "eclipse.product=test\n" {
		t.Fatal("modified distribution")
	}
	if len(plan.WorkspaceFolders) != 1 || plan.InitializationOptions["settings"] == nil {
		t.Fatal(plan)
	}
}

func TestVerifiedDownload(t *testing.T) {
	content := []byte("verified content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(content) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "tools/lombok.jar")
	if err := downloadVerified(srv.Client(), srv.URL, "wrong", path); err == nil {
		t.Fatal("accepted checksum mismatch")
	}
	if exists(path) {
		t.Fatal("installed unverified data")
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(content))
	if err := downloadVerified(srv.Client(), srv.URL, checksum, path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(content) {
		t.Fatal("wrong installed content")
	}
}

func TestPlatforms(t *testing.T) {
	for _, p := range []struct{ os, arch, want string }{{"darwin", "arm64", "config_mac_arm"}, {"darwin", "amd64", "config_mac"}, {"linux", "arm64", "config_linux_arm"}, {"linux", "amd64", "config_linux"}, {"windows", "amd64", "config_win"}} {
		if got := platformConfig(p.os, p.arch); got != p.want {
			t.Fatal(p, got)
		}
	}
}
