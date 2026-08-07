package config

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/where"
	"github.com/spf13/viper"
)

// Write writes the current configuration, creating the default file when it
// has not been created yet.
func Write() error {
	return write("write", func() error {
		err := viper.WriteConfig()
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return viper.SafeWriteConfig()
		}
		return err
	})
}

// WriteExisting writes an already discovered configuration file without
// changing Viper's existing no-file behavior.
func WriteExisting() error {
	return write("write existing", viper.WriteConfig)
}

// SafeWrite creates the default configuration file only when it does not
// already exist.
func SafeWrite() error {
	return write("safe write", viper.SafeWriteConfig)
}

func write(operation string, writeConfig func() error) error {
	configDir, err := where.ConfigWithError()
	if err != nil {
		return fmt.Errorf("secure config %s: %w", operation, err)
	}

	setConfigFilePermissions()
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		configFile = filepath.Join(configDir, constant.StaticMangal+".toml")
	}

	// Protect an existing legacy file before truncating it. The same check is
	// repeated after the write because Viper opens files itself.
	if err := where.EnsureConfigFilePermissions(configFile); err != nil {
		return fmt.Errorf("secure config %s: %w", operation, err)
	}

	if err := writeConfig(); err != nil {
		return fmt.Errorf("secure config %s: %w", operation, err)
	}

	if _, err := where.ConfigWithError(); err != nil {
		return fmt.Errorf("secure config %s after write: %w", operation, err)
	}
	if err := where.EnsureConfigFilePermissions(configFile); err != nil {
		return fmt.Errorf("secure config %s after write: %w", operation, err)
	}

	return nil
}
