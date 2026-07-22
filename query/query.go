package query

import (
	"github.com/metafates/gache"
	"github.com/ryanmccool/static-mangal/filesystem"
	"github.com/ryanmccool/static-mangal/where"
)

type queryRecord struct {
	Rank  int    `json:"rank"`
	Query string `json:"query"`
}

var cacher = gache.New[map[string]*queryRecord](
	&gache.Options{
		Path:       where.Queries(),
		FileSystem: &filesystem.GacheFs{},
	},
)
