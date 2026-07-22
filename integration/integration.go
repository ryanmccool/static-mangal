package integration

import (
	"github.com/ryanmccool/static-mangal/integration/anilist"
	"github.com/ryanmccool/static-mangal/source"
)

// Integrator is the interface that wraps the basic integration methods.
type Integrator interface {
	// MarkRead marks a chapter as read
	MarkRead(chapter *source.Chapter) error
}

var (
	Anilist Integrator = anilist.New()
)
