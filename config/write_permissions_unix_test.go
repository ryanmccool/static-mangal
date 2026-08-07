//go:build aix || darwin || dragonfly || freebsd || ios || linux || netbsd || openbsd || solaris

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/spf13/viper"
)

func TestWriteEnforcesUnixConfigModes(t *testing.T) {
	configDir := setupTempConfig(t, false)
	viper.Set("anilist.id", "client-id")

	if err := Write(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, configDir, 0700)
	assertMode(t, filepath.Join(configDir, constant.StaticMangal+".toml"), 0600)
}

func TestLegacyConfigModesRemainSecureOnEveryWrite(t *testing.T) {
	configDir := setupTempConfig(t, true)
	configFile := filepath.Join(configDir, constant.StaticMangal+".toml")
	viper.Set("anilist.id", "client-id")
	assertMode(t, configDir, 0700)
	assertMode(t, configFile, 0600)

	if err := os.Chmod(configFile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteExisting(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, configDir, 0700)
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
