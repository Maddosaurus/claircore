package libindex

import (
	"context"
	"github.com/quay/claircore"
	"github.com/quay/claircore/indexer"
	"github.com/quay/claircore/indexer/controller"
)

// ControllerFactory is a factory method to return a Controller during libindex runtime.
type ControllerFactory func(opts *indexer.Options) *controller.Controller

type IndexController interface {
	Index(ctx context.Context, manifest *claircore.Manifest) (*claircore.IndexReport, error)
}
