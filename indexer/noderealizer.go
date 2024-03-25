package indexer

import (
	"context"
	"github.com/quay/claircore"
	"github.com/quay/claircore/internal/wart"
	"github.com/quay/zlog"
	"net/http"
)

var (
	_ FetchArena          = (*NodeArena)(nil)
	_ Realizer            = (*MountedFS)(nil)
	_ DescriptionRealizer = (*MountedFS)(nil)
)

type NodeArena struct {
	wc        *http.Client
	mountPath string
}

// NewNodeArena returns an initialized NodeArena.
func NewNodeArena(wc *http.Client, mountPath string) *NodeArena {
	return &NodeArena{
		wc:        wc,
		mountPath: mountPath,
	}
}

func (n *NodeArena) Realizer(_ context.Context) Realizer {
	return &MountedFS{n: n}
}

func (n *NodeArena) Close(ctx context.Context) error {
	ctx = zlog.ContextWithValues(ctx,
		"component", "indexer/nodearena.Close")
	// FIXME: Potentially abort any running scan and clean cache

	return nil
}

type MountedFS struct {
	n *NodeArena
}

// RealizeDescriptions operates on the r/o Node OS mount.
// The mounted OS dir is treated as a single layer.
func (m *MountedFS) RealizeDescriptions(ctx context.Context, _ []claircore.LayerDescription) ([]claircore.Layer, error) {
	// TODO: Make calling robust
	ls := make([]claircore.Layer, 1)
	d := claircore.LayerDescription{
		Digest:    "1234", // FIXME: Calc digest of whole OS? Ignore and scan anyways?
		URI:       "",     // Live system, no fetch URI
		MediaType: "",     // FIXME: Extend this for live system?
		Headers:   nil,
	}
	ls[0].Init(ctx, &d, nil)
	return ls, nil
}

func (m *MountedFS) Realize(ctx context.Context, ls []*claircore.Layer) error {
	l, err := m.RealizeDescriptions(ctx, []claircore.LayerDescription{})
	wart.CopyLayerPointers(ls, l)
	return err
}

func (m *MountedFS) Close() error {
	//TODO implement me
	panic("implement me")
}

// LayerOrPointer abstracts over a [claircore.Layer] or a pointer to a
// [claircore.Layer]. A user of this type will still need to do runtime
// reflection due to the lack of sum types.
//
// Taken from claircore's internal wart package
// FIXME: Sort this out.
type layerOrPointer interface {
	claircore.Layer | *claircore.Layer
}
