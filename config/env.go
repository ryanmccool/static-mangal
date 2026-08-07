package config

import (
	"os"
	"strings"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/spf13/viper"
)

var EnvExposed []string

// EnvName returns the environment variable name used for a configuration key.
func EnvName(name string) string {
	env := strings.ToUpper(EnvKeyReplacer.Replace(name))
	appPrefix := strings.ToUpper(constant.StaticMangal + "_")
	if strings.HasPrefix(env, appPrefix) {
		return env
	}

	return appPrefix + env
}

// SensitiveValue reads a credential from its legacy environment variable
// without registering it with Viper. Persisted configuration remains the
// fallback when the environment variable is unset or empty.
func SensitiveValue(name string) string {
	if key.IsSensitive(name) {
		if value, present := os.LookupEnv(EnvName(name)); present && value != "" {
			return value
		}
	}

	return viper.GetString(name)
}
