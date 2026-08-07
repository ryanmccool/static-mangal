package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/filesystem"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/spf13/viper"
)

func setupTempConfig(t *testing.T, legacy bool) string {
	t.Helper()

	filesystem.SetOsFs()
	viper.Reset()
	configDir := filepath.Join(t.TempDir(), "mangal")
	t.Setenv("MANGAL_CONFIG_PATH", configDir)
	if legacy {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configFilePath(configDir), []byte("[formats]\nuse = \"zip\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := Setup(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		viper.Reset()
		filesystem.SetOsFs()
	})
	return configDir
}

func TestWriteCreatesConfig(t *testing.T) {
	configDir := setupTempConfig(t, false)
	viper.Set(key.FormatsUse, "cbz")

	if err := Write(); err != nil {
		t.Fatal(err)
	}
	assertConfigContent(t, configDir, "cbz")
}

func TestSafeWriteCreatesConfigAndRefusesOverwrite(t *testing.T) {
	configDir := setupTempConfig(t, false)
	viper.Set(key.FormatsUse, "pdf")

	if err := SafeWrite(); err != nil {
		t.Fatal(err)
	}
	if err := SafeWrite(); err == nil {
		t.Fatal("SafeWrite() unexpectedly overwrote the existing config")
	}
	assertConfigContent(t, configDir, "pdf")
}

func TestWriteExistingHardensAndWritesLegacyConfig(t *testing.T) {
	configDir := setupTempConfig(t, true)
	viper.Set(key.FormatsUse, "plain")

	if err := WriteExisting(); err != nil {
		t.Fatal(err)
	}
	assertConfigContent(t, configDir, "plain")
}

func assertConfigContent(t *testing.T, configDir, value string) {
	t.Helper()
	content, err := os.ReadFile(configFilePath(configDir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), value) {
		t.Fatalf("config content = %q, want it to contain %q", content, value)
	}
}

func configFilePath(configDir string) string {
	return filepath.Join(configDir, constant.StaticMangal+".toml")
}
