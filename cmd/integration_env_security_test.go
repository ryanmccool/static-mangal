package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlecAivazis/survey/v2"
	"github.com/ryanmccool/static-mangal/config"
	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/filesystem"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/where"
	"github.com/spf13/viper"
)

func TestAnilistCommandUsesEnvironmentCredentialsWithoutPromptingOrPersisting(t *testing.T) {
	const (
		secret = "command-environment-secret"
		code   = "command-environment-code"
	)

	filesystem.SetOsFs()
	viper.Reset()
	configDir := filepath.Join(t.TempDir(), "mangal")
	configFile := filepath.Join(configDir, constant.StaticMangal+".toml")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, []byte("[anilist]\nenable = true\nid = \"client-id\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
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
	viper.Set(key.AnilistEnable, true)
	viper.Set(key.AnilistID, "client-id")
	previousAsk := askAnilist
	t.Cleanup(func() { askAnilist = previousAsk })

	var passwordPrompts int
	askAnilist = func(prompt survey.Prompt, response interface{}, _ ...survey.AskOpt) error {
		switch prompt.(type) {
		case *survey.Password:
			passwordPrompts++
			*(response.(*string)) = "prompt-value"
		case *survey.Confirm:
			*(response.(*bool)) = false
		case *survey.Input:
			*(response.(*string)) = "prompt-value"
		}
		return nil
	}
	previousDisable := integrationAnilistCmd.Flags().Lookup("disable").Value.String()
	if err := integrationAnilistCmd.Flags().Set("disable", "false"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = integrationAnilistCmd.Flags().Set("disable", previousDisable)
	})

	integrationAnilistCmd.Run(integrationAnilistCmd, nil)

	if passwordPrompts != 0 {
		t.Fatalf("AniList credential prompts = %d, want none", passwordPrompts)
	}
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{secret, code, "prompt-value"} {
		if strings.Contains(string(content), value) {
			t.Fatalf("config contains credential/prompt value %q: %s", value, content)
		}
	}
}
