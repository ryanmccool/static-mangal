package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/AlecAivazis/survey/v2"
	"github.com/ryanmccool/static-mangal/config"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/spf13/viper"
)

func TestConfigGetRedactsSensitiveValue(t *testing.T) {
	const secret = "offline-config-secret"
	previous := viper.Get(key.AnilistSecret)
	t.Cleanup(func() { viper.Set(key.AnilistSecret, previous) })
	viper.Set(key.AnilistSecret, secret)

	if got := configGetValue(key.AnilistSecret); got != config.RedactedValue {
		t.Fatalf("configGetValue() = %v, want %q", got, config.RedactedValue)
	}
	if got := configGetValue(key.AnilistID); got == secret {
		t.Fatal("redaction unexpectedly changed non-sensitive handling")
	}
}

func TestConfigSetRejectsSensitiveArgvValue(t *testing.T) {
	called := false
	originalAsk := askConfigPassword
	t.Cleanup(func() { askConfigPassword = originalAsk })
	askConfigPassword = func(*survey.Password, *string) error {
		called = true
		return errors.New("prompt should not run")
	}

	const secret = "offline-argv-secret"
	_, err := configSetValue(key.AnilistSecret, []string{secret}, true)
	if err == nil || !strings.Contains(err.Error(), "--value") || !strings.Contains(err.Error(), "interactively") {
		t.Fatalf("configSetValue() error = %v, want interactive-entry instruction", err)
	}
	if called {
		t.Fatal("sensitive argv value triggered the interactive prompt")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("sensitive argv value was echoed in the error")
	}
}

func TestConfigSetSensitiveValueUsesPasswordAndDoesNotEcho(t *testing.T) {
	originalAsk := askConfigPassword
	t.Cleanup(func() { askConfigPassword = originalAsk })

	const secret = "offline-interactive-secret"
	var gotPrompt *survey.Password
	askConfigPassword = func(prompt *survey.Password, response *string) error {
		gotPrompt = prompt
		*response = secret
		return nil
	}

	value, err := configSetValue(key.AnilistSecret, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if value != secret {
		t.Fatalf("interactive value = %v, want %q", value, secret)
	}
	if gotPrompt == nil {
		t.Fatal("sensitive config set did not use a password prompt")
	}
	if got := configSetDisplayValue(key.AnilistSecret, value); got != config.RedactedValue {
		t.Fatalf("success display value = %v, want %q", got, config.RedactedValue)
	}
	if strings.Contains(configSetDisplayValue(key.AnilistSecret, value).(string), secret) {
		t.Fatal("sensitive value was echoed by success display handling")
	}
}

func TestConfigSetClientIDRemainsNonSensitive(t *testing.T) {
	const clientID = "offline-client-id"
	value, err := configSetValue(key.AnilistID, []string{clientID}, true)
	if err != nil {
		t.Fatal(err)
	}
	if value != clientID {
		t.Fatalf("client ID value = %v, want %q", value, clientID)
	}
}
