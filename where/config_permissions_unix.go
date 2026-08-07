//go:build aix || darwin || dragonfly || freebsd || ios || linux || netbsd || openbsd || solaris

package where

import (
	"fmt"
	"os"

	"github.com/ryanmccool/static-mangal/filesystem"
)

const (
	configDirectoryMode os.FileMode = 0700
	configFileMode      os.FileMode = 0600
)

func ensureConfigDirectory(path string, custom bool) error {
	if custom {
		return ensureCustomConfigDirectory(path)
	}

	return ensureDefaultConfigDirectory(path)
}

func ensureDefaultConfigDirectory(path string) error {
	if err := filesystem.Api().MkdirAll(path, configDirectoryMode); err != nil {
		return fmt.Errorf("secure config directory %q: create: %w", path, err)
	}

	if err := filesystem.Api().Chmod(path, configDirectoryMode); err != nil {
		return fmt.Errorf("secure config directory %q: chmod to 0700: %w", path, err)
	}

	info, err := filesystem.Api().Stat(path)
	if err != nil {
		return fmt.Errorf("secure config directory %q: stat: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("secure config directory %q: path is not a directory", path)
	}
	if mode := info.Mode().Perm(); mode != configDirectoryMode {
		return fmt.Errorf("secure config directory %q: got mode %04o, want 0700", path, mode)
	}

	return nil
}

func ensureCustomConfigDirectory(path string) error {
	info, err := filesystem.Api().Stat(path)
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("MANGAL_CONFIG_PATH %q: inspect directory: %w", path, err)
		}

		if err := filesystem.Api().MkdirAll(path, configDirectoryMode); err != nil {
			return fmt.Errorf("MANGAL_CONFIG_PATH %q: create private directory with mode 0700: %w", path, err)
		}
		created = true
		info, err = filesystem.Api().Stat(path)
		if err != nil {
			return fmt.Errorf("MANGAL_CONFIG_PATH %q: stat after creation: %w", path, err)
		}
	}

	if !info.IsDir() {
		return fmt.Errorf("MANGAL_CONFIG_PATH %q must be a dedicated owner-only directory; choose a directory path", path)
	}
	if mode := info.Mode().Perm(); mode != configDirectoryMode {
		if created {
			if err := filesystem.Api().Chmod(path, configDirectoryMode); err != nil {
				return fmt.Errorf("MANGAL_CONFIG_PATH %q: set new directory mode to 0700: %w", path, err)
			}
			info, err = filesystem.Api().Stat(path)
			if err != nil {
				return fmt.Errorf("MANGAL_CONFIG_PATH %q: stat after permission enforcement: %w", path, err)
			}
			if mode := info.Mode().Perm(); mode == configDirectoryMode {
				return nil
			}
		}
		return fmt.Errorf("MANGAL_CONFIG_PATH %q must already have owner-only mode 0700 (found %04o); chmod it to 0700 or choose a dedicated private directory", path, mode)
	}

	return nil
}

func ensureConfigFilePermissions(path string) error {
	exists, err := filesystem.Api().Exists(path)
	if err != nil {
		return fmt.Errorf("secure config file %q: check existence: %w", path, err)
	}
	if !exists {
		return nil
	}

	info, err := filesystem.Api().Stat(path)
	if err != nil {
		return fmt.Errorf("secure config file %q: stat: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("secure config file %q: path is not a regular file", path)
	}

	if err := filesystem.Api().Chmod(path, configFileMode); err != nil {
		return fmt.Errorf("secure config file %q: chmod to 0600: %w", path, err)
	}

	info, err = filesystem.Api().Stat(path)
	if err != nil {
		return fmt.Errorf("secure config file %q: stat after chmod: %w", path, err)
	}
	if mode := info.Mode().Perm(); mode != configFileMode {
		return fmt.Errorf("secure config file %q: got mode %04o, want 0600", path, mode)
	}

	return nil
}
