package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type options struct {
	JavaHome             string `json:"javaHome,omitempty"`
	JDTLSHome            string `json:"jdtlsHome,omitempty"`
	CacheDir             string `json:"cacheDir,omitempty"`
	LombokJar            string `json:"lombokJar,omitempty"`
	MaxHeapMiB           *int   `json:"maxHeapMiB,omitempty"`
	AutoBuild            *bool  `json:"autoBuild,omitempty"`
	AnnotationProcessing *bool  `json:"annotationProcessing,omitempty"`
}

type configuration struct {
	options
	Workspaces map[string]options `json:"workspaces,omitempty"`
}

func dataDir() string {
	if value := os.Getenv("MAYOI_DATA_DIR"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mayoi")
}

func storageDir() string { return filepath.Join(dataDir(), "storage", "mayoi-java") }

func canonical(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("expected an absolute path: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		return filepath.Clean(path), nil
	}
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func decodeStrict(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}

func (o *options) overlay(other options) {
	if other.JavaHome != "" {
		o.JavaHome = other.JavaHome
	}
	if other.JDTLSHome != "" {
		o.JDTLSHome = other.JDTLSHome
	}
	if other.CacheDir != "" {
		o.CacheDir = other.CacheDir
	}
	if other.LombokJar != "" {
		o.LombokJar = other.LombokJar
	}
	if other.MaxHeapMiB != nil {
		o.MaxHeapMiB = other.MaxHeapMiB
	}
	if other.AutoBuild != nil {
		o.AutoBuild = other.AutoBuild
	}
	if other.AnnotationProcessing != nil {
		o.AnnotationProcessing = other.AnnotationProcessing
	}
}

func (o options) validate() error {
	for key, path := range map[string]string{"javaHome": o.JavaHome, "jdtlsHome": o.JDTLSHome, "cacheDir": o.CacheDir, "lombokJar": o.LombokJar} {
		if path != "" {
			if _, err := canonical(path); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if o.MaxHeapMiB != nil && (*o.MaxHeapMiB < 256 || *o.MaxHeapMiB > 32768) {
		return fmt.Errorf("maxHeapMiB must be between 256 and 32768")
	}
	return nil
}

// Explicit editor settings override the profile; workspace options take
// precedence over global values. Environment variables are the final override.
func loadOptions(root string, settings map[string]json.RawMessage) (options, error) {
	var config configuration
	path := filepath.Join(storageDir(), "config.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return options{}, err
	}
	if err == nil {
		if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return options{}, fmt.Errorf("%s must be an object", path)
		}
		if err = decodeStrict(data, &config); err != nil {
			return options{}, fmt.Errorf("%s: %w", path, err)
		}
	}
	resolvedRoot, err := canonical(root)
	if err != nil {
		return options{}, err
	}
	o := config.options
	if err = o.validate(); err != nil {
		return o, err
	}
	for workspace, value := range config.Workspaces {
		path, err := canonical(workspace)
		if err != nil {
			return o, err
		}
		if err = value.validate(); err != nil {
			return o, fmt.Errorf("workspace %s: %w", workspace, err)
		}
		if path == resolvedRoot {
			o.overlay(value)
		}
	}
	values := map[string]json.RawMessage{}
	for key, value := range settings {
		if key != "helperPath" {
			values[key] = value
		}
	}
	data, _ = json.Marshal(values)
	var editor options
	if err := decodeStrict(data, &editor); err != nil {
		return o, fmt.Errorf("plugin settings: %w", err)
	}
	o.overlay(editor)
	for variable, target := range map[string]*string{
		"MAYOI_JAVA_HOME": &o.JavaHome, "MAYOI_JAVA_JDTLS_HOME": &o.JDTLSHome,
		"MAYOI_JAVA_CACHE_DIR": &o.CacheDir,
	} {
		if value := os.Getenv(variable); value != "" {
			*target = value
		}
	}
	if value := os.Getenv("MAYOI_JAVA_MAX_HEAP_MIB"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil {
			return o, fmt.Errorf("invalid MAYOI_JAVA_MAX_HEAP_MIB: %w", err)
		}
		o.MaxHeapMiB = &n
	}
	return o, o.validate()
}

func enabled(value *bool) bool { return value == nil || *value }
