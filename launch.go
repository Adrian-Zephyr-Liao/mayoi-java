package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const minJava = 25

var javaUnsetEnv = []string{"CLIENT_HOST", "CLIENT_PORT", "JDTLS_JVM_ARGS", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS"}

type folder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}
type mapping struct {
	ClientURI string `json:"clientURI"`
	ServerURI string `json:"serverURI"`
}
type launch struct {
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	Env                   map[string]string `json:"env"`
	UnsetEnv              []string          `json:"unsetEnv"`
	InitializationOptions map[string]any    `json:"initializationOptions"`
	Configuration         map[string]any    `json:"configuration"`
	WorkspaceFolders      []folder          `json:"workspaceFolders"`
	PathMappings          []mapping         `json:"pathMappings"`
}

func fileURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func exists(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() }
func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

var javaVersion = regexp.MustCompile(`(?:openjdk|java) version "(?:1\.)?(\d+)`)

func javaMajor(path string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-version")
	cmd.Env = []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		remove := false
		for _, unset := range javaUnsetEnv {
			if key == unset {
				remove = true
				break
			}
		}
		if !remove {
			cmd.Env = append(cmd.Env, value)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	var major int
	if match := javaVersion.FindStringSubmatch(string(out)); len(match) == 2 {
		fmt.Sscan(match[1], &major)
	}
	return major
}

func findJava(o options) (string, error) {
	var candidates []string
	addHome := func(home string) {
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "bin", executable("java")))
		}
	}
	if o.JavaHome != "" {
		home, err := canonical(o.JavaHome)
		if err != nil {
			return "", err
		}
		addHome(home)
	} else {
		for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
			for _, name := range []string{"openjdk@25", "openjdk"} {
				addHome(filepath.Join(prefix, "opt", name))
			}
		}
		addHome(os.Getenv("JAVA_HOME"))
		if path, err := exec.LookPath("java"); err == nil {
			candidates = append(candidates, path)
		}
		if runtime.GOOS == "darwin" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out, err := exec.CommandContext(ctx, "/usr/libexec/java_home", "-v", "25+").Output()
			cancel()
			if err == nil {
				addHome(strings.TrimSpace(string(out)))
			}
		}
	}
	for _, path := range candidates {
		if exists(path) && javaMajor(path) >= minJava {
			return canonical(path)
		}
	}
	return "", fmt.Errorf("JDT LS needs JDK %d+. Install a JDK and set javaHome in %s; the project JDK can stay unchanged", minJava, filepath.Join(storageDir(), "config.json"))
}

func platformConfig(goos, arch string) string {
	switch goos {
	case "darwin":
		if arch == "arm64" {
			return "config_mac_arm"
		}
		return "config_mac"
	case "linux":
		if arch == "arm64" {
			return "config_linux_arm"
		}
		return "config_linux"
	case "windows":
		return "config_win"
	}
	return ""
}

func findJDTLS(o options) (home, jar, config string, err error) {
	var candidates []string
	if o.JDTLSHome != "" {
		candidates = append(candidates, o.JDTLSHome)
	} else {
		for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
			candidates = append(candidates, filepath.Join(prefix, "opt/jdtls/libexec"))
		}
		if path, e := exec.LookPath("jdtls"); e == nil {
			path, _ = canonical(path)
			base := filepath.Dir(filepath.Dir(path))
			candidates = append(candidates, base, filepath.Join(base, "libexec"))
		}
	}
	for _, candidate := range candidates {
		home, err = canonical(candidate)
		if err != nil {
			continue
		}
		jars, _ := filepath.Glob(filepath.Join(home, "plugins", "org.eclipse.equinox.launcher_*.jar"))
		sort.Strings(jars)
		config = filepath.Join(home, platformConfig(runtime.GOOS, runtime.GOARCH))
		if len(jars) > 0 && exists(filepath.Join(config, "config.ini")) {
			return home, jars[len(jars)-1], config, nil
		}
	}
	return "", "", "", fmt.Errorf("install Eclipse JDT LS and set jdtlsHome to its distribution directory (plugins/ and %s/)", platformConfig(runtime.GOOS, runtime.GOARCH))
}

func workspace(root string) ([]folder, []mapping, error) {
	root = filepath.Clean(root)
	canonicalRoot, err := canonical(root)
	if err != nil {
		return nil, nil, err
	}
	project := func(path string) bool {
		for _, name := range []string{"pom.xml", "build.gradle", "build.gradle.kts", ".project"} {
			if exists(filepath.Join(path, name)) {
				return true
			}
		}
		return false
	}
	paths := []string{root}
	if !project(root) {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, nil, err
		}
		var members []string
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			if project(path) {
				members = append(members, path)
			}
		}
		if len(members) > 0 {
			paths = members
		}
	}
	mappings := []mapping{{ClientURI: fileURI(root), ServerURI: fileURI(canonicalRoot)}}
	var folders []folder
	seen := map[string]bool{}
	for _, path := range paths {
		real, err := canonical(path)
		if err != nil {
			return nil, nil, err
		}
		uri := fileURI(real)
		if seen[uri] {
			continue
		}
		seen[uri] = true
		folders = append(folders, folder{uri, filepath.Base(path)})
		if path != root {
			mappings = append(mappings, mapping{fileURI(path), uri})
		}
	}
	return folders, mappings, nil
}

func copyConfig(source, target string) error {
	if exists(filepath.Join(target, "config.ini")) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(target), "configuration-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in JDT LS configuration: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, target); err != nil && !exists(filepath.Join(target, "config.ini")) {
		return err
	}
	return nil
}

func resolve(root string, settings map[string]json.RawMessage) (launch, error) {
	var result launch
	o, err := loadOptions(root, settings)
	if err != nil {
		return result, err
	}
	java, err := findJava(o)
	if err != nil {
		return result, err
	}
	home, jar, source, err := findJDTLS(o)
	if err != nil {
		return result, err
	}
	folders, mappings, err := workspace(root)
	if err != nil {
		return result, err
	}
	real, err := canonical(root)
	if err != nil {
		return result, err
	}
	cache := o.CacheDir
	if cache == "" {
		cache = filepath.Join(storageDir(), "cache")
	}
	cache, err = canonical(cache)
	if err != nil {
		return result, err
	}
	identity := fmt.Sprintf("%x", sha256.Sum256([]byte(real)))[:24]
	base := filepath.Join(cache, identity)
	ini, err := os.ReadFile(filepath.Join(source, "config.ini"))
	if err != nil {
		return result, err
	}
	distribution := fmt.Sprintf("%x", sha256.Sum256(append([]byte(home), ini...)))[:12]
	configuration := filepath.Join(base, distribution, "configuration")
	if err = copyConfig(source, configuration); err != nil {
		return result, err
	}
	javaSettings := map[string]any{
		"configuration": map[string]any{"updateBuildConfiguration": "automatic"},
		"import":        map[string]any{"maven": map[string]any{"enabled": true}, "gradle": map[string]any{"enabled": true}},
		"autobuild":     map[string]any{"enabled": enabled(o.AutoBuild)},
	}
	if !enabled(o.AnnotationProcessing) {
		prefs := filepath.Join(base, "navigation.prefs")
		if err = os.WriteFile(prefs, []byte("org.eclipse.jdt.core.compiler.processAnnotations=disabled\n"), 0600); err != nil {
			return result, err
		}
		javaSettings["settings"] = map[string]any{"url": fileURI(prefs)}
	}
	settingsForServer := map[string]any{"java": javaSettings}
	heap := 2048
	if o.MaxHeapMiB != nil {
		heap = *o.MaxHeapMiB
	}
	args := []string{
		"-Declipse.application=org.eclipse.jdt.ls.core.id1", "-Dosgi.bundles.defaultStartLevel=4",
		"-Declipse.product=org.eclipse.jdt.ls.core.product", "-Dlog.protocol=false", "-Dlog.level=WARNING",
		"-Xms128m", fmt.Sprintf("-Xmx%dm", heap), "--add-modules=ALL-SYSTEM",
		"--add-opens", "java.base/java.util=ALL-UNNAMED", "--add-opens", "java.base/java.lang=ALL-UNNAMED",
	}
	lombok := o.LombokJar
	if lombok == "" {
		lombok = lombokPath()
	}
	lombok, err = canonical(lombok)
	if err != nil {
		return result, err
	}
	if exists(lombok) {
		args = append(args, "-javaagent:"+lombok)
	} else if o.LombokJar != "" {
		return result, fmt.Errorf("missing Lombok agent: %s", lombok)
	}
	// A new native cache avoids locking or reusing an incomplete proxy session.
	// Separate processor modes prevent old disabled-APT preferences leaking back.
	mode := "standard"
	if !enabled(o.AnnotationProcessing) {
		mode = "no-apt"
	}
	args = append(args, "-jar", jar, "-configuration", configuration, "-data", filepath.Join(base, "data-v3-"+mode))
	uriList := make([]string, len(folders))
	for i, f := range folders {
		uriList[i] = f.URI
	}
	return launch{
		Command: java, Args: args, Env: map[string]string{"JAVA_HOME": filepath.Dir(filepath.Dir(java))},
		UnsetEnv:              javaUnsetEnv,
		InitializationOptions: map[string]any{"workspaceFolders": uriList, "settings": settingsForServer},
		Configuration:         settingsForServer, WorkspaceFolders: folders, PathMappings: mappings,
	}, nil
}
