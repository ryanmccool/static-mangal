package cmd

import (
	"testing"

	"github.com/AlecAivazis/survey/v2"
)

func TestAnilistCredentialPromptsUsePassword(t *testing.T) {
	secretPrompt := anilistSecretPrompt()
	if _, ok := interface{}(secretPrompt).(survey.Prompt); !ok {
		t.Fatal("secret prompt does not implement survey.Prompt")
	}
	if secretPrompt.Message == "" {
		t.Fatal("secret prompt has no message")
	}

	codePrompt := anilistCodePrompt()
	if _, ok := interface{}(codePrompt).(survey.Prompt); !ok {
		t.Fatal("code prompt does not implement survey.Prompt")
	}
	if codePrompt.Message == "" {
		t.Fatal("code prompt has no message")
	}
}
