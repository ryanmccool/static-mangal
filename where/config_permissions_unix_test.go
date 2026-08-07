//go:build aix || darwin || dragonfly || freebsd || ios || linux || netbsd || openbsd || solaris

package where

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/filesystem"
)

func TestDefaultConfigDirectoryModeForNewAndLegacyDirectories(t *testing.T) {
	filesystem.SetOsFs()
	t.Cleanup(filesystem.SetMemMapFs)

	configPath := filepath.Join(t.TempDir(), "mangal")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ensureConfigDirectory(configPath, false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, configPath, 0700)

	if err := os.Chmod(configPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensureConfigDirectory(configPath, false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, configPath, 0700)
}

func TestCustomConfigDirectoryValidation(t *testing.T) {
	filesystem.SetOsFs()
	t.Cleanup(filesystem.SetMemMapFs)

	configPath := filepath.Join(t.TempDir(), "custom-mangal")
	t.Setenv(EnvConfigPath, configPath)

	path, err := ConfigWithError()
	if err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0700)

	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigWithError(); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("ConfigWithError() error = %v, want owner-only remediation error", err)
	}
	assertMode(t, path, 0755)

	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigWithError(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0700)
}

func TestConfigFileModeForLegacyFile(t *testing.T) {
	filesystem.SetOsFs()
	t.Cleanup(filesystem.SetMemMapFs)

	configPath := filepath.Join(t.TempDir(), "mangal")
	if err := os.MkdirAll(configPath, 0700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configPath, constant.StaticMangal+".toml")
	if err := os.WriteFile(configFile, []byte("[formats]\nuse = \"pdf\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureConfigFilePermissions(configFile); err != nil {
		t.Fatal(err)
	}
	assertMode(t, configFile, 0600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
