//go:build aix || darwin || dragonfly || freebsd || ios || linux || netbsd || openbsd || solaris

package config

import (
	"os"

	"github.com/spf13/viper"
)

func setConfigFilePermissions() {
	viper.SetConfigPermissions(os.FileMode(0600))
}
