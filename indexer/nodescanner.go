package indexer

import (
	"context"
	"fmt"
	"github.com/quay/claircore"
	"github.com/quay/zlog"
)

var (
	mountLocation = "/tmp/rhcos"
)

// TODO: This has no concurrency currently. Also, eval whether we can get the layerScanner to work with a single layer
type NodeScanner struct {
	// Pre-constructed and configured scanners.
	ps  []PackageScanner
	ds  []DistributionScanner
	rs  []RepositoryScanner
	fis []FileScanner
}

// NewNodeScanner .
func NewNodeScanner(ctx context.Context, concurrent int, opts *Options) (*NodeScanner, error) {
	ctx = zlog.ContextWithValues(ctx, "component", "indexer.NewNodeScanner")

	ps, ds, rs, fs, err := EcosystemsToScanners(ctx, opts.Ecosystems)
	if err != nil {
		return nil, fmt.Errorf("failed to extract scanners from ecosystems: %v", err)
	}
	return &NodeScanner{
		ps:  configAndFilter(ctx, opts, ps),
		ds:  configAndFilter(ctx, opts, ds),
		rs:  configAndFilter(ctx, opts, rs),
		fis: configAndFilter(ctx, opts, fs),
	}, nil
}

// Scan performs a scan of the target dir.
//
// It expects the root dir of a OS at the given location,
// e.g. a r/o mount of the Node OS.
func (ns *NodeScanner) Scan(ctx context.Context, _ claircore.Digest, layers []*claircore.Layer) error {
	for _, l := range layers {
		select {
		case <-ctx.Done():
			break
		default:
		}
		for _, s := range ns.ps {
			scanLayer(ctx, l, s)
		}
		for _, s := range ns.ds {
			scanLayer(ctx, l, s)
		}
		for _, s := range ns.rs {
			scanLayer(ctx, l, s)
		}
		for _, s := range ns.fis {
			scanLayer(ctx, l, s)
		}
	}
	// FIXME: Error handling for called scanners
	return nil
}

func scanLayer(ctx context.Context, l *claircore.Layer, s VersionedScanner) error {
	ctx = zlog.ContextWithValues(ctx,
		"component", "indexer/NodeScanner.scanLayer",
		"scanner", s.Name(),
		"kind", s.Kind(),
		"layer", l.Hash.String())
	zlog.Debug(ctx).Msg("scan start")
	defer zlog.Debug(ctx).Msg("scan done")

	var result result
	if err := result.Do(ctx, s, l); err != nil {
		return err
	}

	// FIXME: Add error handling and result collection
	return nil
}
