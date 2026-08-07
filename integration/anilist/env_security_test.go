package anilist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanmccool/static-mangal/config"
	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/filesystem"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/where"
	"github.com/spf13/viper"
)

func TestSensitiveEnvironmentCredentialsStayOutOfConfigWrites(t *testing.T) {
	const (
		secret = "environment-only-anilist-secret"
		code   = "environment-only-anilist-code"
	)

	filesystem.SetOsFs()
	viper.Reset()
	configDir := filepath.Join(t.TempDir(), "mangal")
	t.Setenv(where.EnvConfigPath, configDir)
	t.Setenv(config.EnvName(key.AnilistSecret), secret)
	t.Setenv(config.EnvName(key.AnilistCode), code)
	t.Cleanup(func() {
		viper.Reset()
		filesystem.SetOsFs()
	})

	if err := config.Setup(); err != nil {
		t.Fatal(err)
	}
	if got := New().secret(); got != secret {
		t.Fatalf("secret() = %q, want environment value", got)
	}
	if got := New().code(); got != code {
		t.Fatalf("code() = %q, want environment value", got)
	}
	if got := viper.GetString(key.AnilistSecret); got == secret {
		t.Fatal("sensitive environment value entered generic Viper lookup")
	}
	if got := viper.GetString(key.AnilistCode); got == code {
		t.Fatal("sensitive environment value entered generic Viper lookup")
	}

	viper.Set(key.FormatsUse, "zip")
	if err := config.Write(); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, constant.StaticMangal+".toml")
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{secret, code} {
		if strings.Contains(string(content), value) {
			t.Fatalf("config write contains environment-only credential %q: %s", value, content)
		}
	}

	t.Setenv(config.EnvName(key.AnilistSecret), "")
	viper.Set(key.AnilistSecret, "persisted-secret")
	if got := New().secret(); got != "persisted-secret" {
		t.Fatalf("secret() fallback = %q, want persisted config value", got)
	}
}
