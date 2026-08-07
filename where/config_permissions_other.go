//go:build !aix && !darwin && !dragonfly && !freebsd && !ios && !linux && !netbsd && !openbsd && !solaris && !windows

package where

import (
	"fmt"
	"os"

	"github.com/ryanmccool/static-mangal/filesystem"
)

// Platforms without Unix permission semantics and without Windows ACL
// handling retain the existing filesystem behavior.
func ensureConfigDirectory(path string, custom bool) error {
	if custom {
		info, err := filesystem.Api().Stat(path)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("MANGAL_CONFIG_PATH %q must be a dedicated directory", path)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("MANGAL_CONFIG_PATH %q: inspect directory: %w", path, err)
		}
	}

	if err := filesystem.Api().MkdirAll(path, os.ModePerm); err != nil {
		return fmt.Errorf("MANGAL_CONFIG_PATH %q: create directory: %w", path, err)
	}
	return nil
}

func ensureConfigFilePermissions(string) error {
	return nil
}
