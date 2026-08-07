//go:build !darwin && !linux

package selfupdate

import "os"

func ownedByEffectiveUser(os.FileInfo) bool { return true }
