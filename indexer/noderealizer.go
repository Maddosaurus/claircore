package indexer

import (
	"context"
	"github.com/quay/claircore"
	"github.com/quay/zlog"
	"net/http"
	"os"
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
	// TODO: Ensure mounting works
	ls := make([]claircore.Layer, 1)
	nodeFS := os.DirFS(m.n.mountPath)
	ls[0].InitROFS(ctx, nodeFS)
	return ls, nil
}

// Realize is a deprecated way of realizing.
// FIXME: dedup
func (m *MountedFS) Realize(ctx context.Context, ls []*claircore.Layer) error {
	// TODO: Ensure mounting works
	zlog.Info(ctx).Msgf("Realizing mount path: %s", m.n.mountPath)
	nodeFS := os.DirFS(m.n.mountPath)
	l := claircore.Layer{}
	err := l.InitROFS(ctx, nodeFS)
	if err != nil {
		return err
	}
	ls[0] = &l
	return nil
}

func (m *MountedFS) Close() error {
	// noop for os.DirFS
	return nil
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
