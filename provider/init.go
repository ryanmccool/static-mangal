package provider

import (
	"github.com/ryanmccool/static-mangal/provider/generic"
	"github.com/ryanmccool/static-mangal/provider/mangadex"
	"github.com/ryanmccool/static-mangal/provider/manganato"
	"github.com/ryanmccool/static-mangal/provider/manganelo"
	"github.com/ryanmccool/static-mangal/provider/mangapill"
	"github.com/ryanmccool/static-mangal/source"
)

const CustomProviderExtension = ".lua"

var builtinProviders = []*Provider{
	{
		ID:   mangadex.ID,
		Name: mangadex.Name,
		CreateSource: func() (source.Source, error) {
			return mangadex.New(), nil
		},
	},
}

func init() {
	for _, conf := range []*generic.Configuration{
		manganelo.Config,
		manganato.Config,
		mangapill.Config,
	} {
		conf := conf
		builtinProviders = append(builtinProviders, &Provider{
			ID:   conf.ID(),
			Name: conf.Name,
			CreateSource: func() (source.Source, error) {
				return generic.New(conf), nil
			},
		})
	}
}
