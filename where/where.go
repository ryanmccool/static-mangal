package where

import (
	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/filesystem"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/samber/lo"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
)

const EnvConfigPath = "MANGAL_CONFIG_PATH"

// mkdir creates a directory and all parent directories if they don't exist
// will return the path of the directory
func mkdir(path string) string {
	lo.Must0(filesystem.Api().MkdirAll(path, os.ModePerm))
	return path
}

// Config path
// Will create the directory if it doesn't exist
func Config() string {
	return lo.Must(ConfigWithError())
}

// ConfigWithError returns the configuration directory, creating it when needed
// and enforcing the platform-specific configuration permissions.
func ConfigWithError() (string, error) {
	var (
		path   string
		custom bool
	)

	if customDir, present := os.LookupEnv(EnvConfigPath); present {
		path = customDir
		custom = true
	} else {
		path = filepath.Join(lo.Must(os.UserConfigDir()), constant.StaticMangal)
	}

	if err := ensureConfigDirectory(path, custom); err != nil {
		return "", err
	}

	return path, nil
}

// EnsureConfigFilePermissions applies the platform-specific permissions to an
// existing configuration file. A missing file is left for the config writer.
func EnsureConfigFilePermissions(path string) error {
	return ensureConfigFilePermissions(path)
}

// Sources path
// Will create the directory if it doesn't exist
func Sources() string {
	return mkdir(filepath.Join(Config(), "sources"))
}

func AnilistBinds() string {
	return filepath.Join(Config(), "anilist.json")
}

// Logs path
// Will create the directory if it doesn't exist
func Logs() string {
	return mkdir(filepath.Join(Config(), "logs"))
}

// Queries path
// Will create the directory if it doesn't exist
func Queries() string {
	return filepath.Join(Cache(), "queries.json")
}

// History path to the file
// Will create the directory if it doesn't exist
func History() string {
	return filepath.Join(Config(), "history.json")
}

// Downloads path
// Will create the directory if it doesn't exist
func Downloads() string {
	path, err := filepath.Abs(viper.GetString(key.DownloaderPath))

	if err != nil {
		path, err = os.Getwd()
		if err != nil {
			path = "."
		}
	}

	return mkdir(path)
}

// Cache path
// Will create the directory if it doesn't exist
func Cache() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = filepath.Join(".", "cache")
	}

	cacheDir = filepath.Join(cacheDir, constant.StaticMangal)
	return mkdir(cacheDir)
}

// Temp path
// Will create the directory if it doesn't exist
func Temp() string {
	tempDir := filepath.Join(os.TempDir(), constant.StaticMangal)
	return mkdir(tempDir)
}
