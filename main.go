package main

import (
	"github.com/ryanmccool/static-mangal/cmd"
	"github.com/ryanmccool/static-mangal/config"
	"github.com/ryanmccool/static-mangal/log"
	"github.com/samber/lo"
)

func main() {
	lo.Must0(config.Setup())
	lo.Must0(log.Setup())
	cmd.Execute()
}
