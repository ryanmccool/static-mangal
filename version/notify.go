package version

import (
	"fmt"
	"github.com/ryanmccool/static-mangal/color"
	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/icon"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/style"
	"github.com/ryanmccool/static-mangal/util"
	"github.com/spf13/viper"
)

func Notify() {
	if !viper.GetBool(key.CliVersionCheck) {
		return
	}

	erase := util.PrintErasable(fmt.Sprintf("%s Checking if new version is available...", icon.Get(icon.Progress)))
	version, err := Latest()
	erase()
	if err == nil {
		comp, err := Compare(version, constant.Version)
		if err == nil && comp <= 0 {
			return
		}
	}

	fmt.Printf(`
%s New version is available %s %s
%s

`,
		style.Fg(color.Green)("▇▇▇"),
		style.Bold(version),
		style.Faint(fmt.Sprintf("(You're on %s)", constant.Version)),
		style.Faint("https://github.com/ryanmccool/static-mangal/releases/tag/v"+version),
	)

}
