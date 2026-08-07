package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ryanmccool/static-mangal/key"
	"github.com/spf13/viper"
)

func TestSensitiveFieldDisplaysRedactedValue(t *testing.T) {
	const secret = "offline-secret-value"
	previous := viper.Get(key.AnilistSecret)
	t.Cleanup(func() { viper.Set(key.AnilistSecret, previous) })
	viper.Set(key.AnilistSecret, secret)

	field := Field{Key: key.AnilistSecret, Value: ""}
	pretty := field.Pretty()
	if strings.Contains(pretty, secret) || !strings.Contains(pretty, RedactedValue) {
		t.Fatalf("Pretty() = %q, want redaction without stored value", pretty)
	}

	encoded, err := json.Marshal(&field)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Value != RedactedValue {
		t.Fatalf("JSON value = %q, want %q", decoded.Value, RedactedValue)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("JSON output contains stored secret: %s", encoded)
	}
}

func TestNonSensitiveFieldStillDisplaysValue(t *testing.T) {
	const clientID = "offline-client-id"
	previous := viper.Get(key.AnilistID)
	t.Cleanup(func() { viper.Set(key.AnilistID, previous) })
	viper.Set(key.AnilistID, clientID)

	field := Field{Key: key.AnilistID, Value: ""}
	if got := displayValue(key.AnilistID); got != clientID {
		t.Fatalf("displayValue() = %v, want %q", got, clientID)
	}
	if strings.Contains(field.Pretty(), RedactedValue) {
		t.Fatal("non-sensitive field was redacted")
	}
}
