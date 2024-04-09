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
	store Store
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
		store: opts.Store,
		ps:    configAndFilter(ctx, opts, ps),
		ds:    configAndFilter(ctx, opts, ds),
		rs:    configAndFilter(ctx, opts, rs),
		fis:   configAndFilter(ctx, opts, fs),
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
			zlog.Info(ctx).Msg("Scanning with ps scanner")
			err := ns.scanLayer(ctx, l, s)
			if err != nil {
				zlog.Error(ctx).Err(err).Msg("error scanning fs layer")
			}
		}
		for _, s := range ns.ds {
			zlog.Info(ctx).Msg("Scanning with ds scanner")
			err := ns.scanLayer(ctx, l, s)
			if err != nil {
				zlog.Error(ctx).Err(err).Msg("error scanning fs layer")
			}
		}
		for _, s := range ns.rs {
			zlog.Info(ctx).Msg("Scanning with rs scanner")
			err := ns.scanLayer(ctx, l, s)
			if err != nil {
				zlog.Error(ctx).Err(err).Msg("error scanning fs layer")
			}
		}
		for _, s := range ns.fis {
			zlog.Info(ctx).Msg("Scanning with fis scanner")
			err := ns.scanLayer(ctx, l, s)
			if err != nil {
				zlog.Error(ctx).Err(err).Msg("error scanning fs layer")
			}
		}
	}
	// FIXME: Error handling for called scanners
	return nil
}

func (ns *NodeScanner) scanLayer(ctx context.Context, l *claircore.Layer, s VersionedScanner) error {
	ctx = zlog.ContextWithValues(ctx,
		"component", "indexer/NodeScanner.scanLayer",
		"scanner", s.Name(),
		"kind", s.Kind(),
		"layer", l.Hash.String())
	zlog.Debug(ctx).Msg("scan start")
	defer zlog.Debug(ctx).Msg("scan done")

	ok, err := ns.store.LayerScanned(ctx, l.Hash, s)
	if err != nil {
		return err
	}
	if ok {
		zlog.Debug(ctx).Msg("layer already scanned")
		return nil
	}

	var result result
	if err := result.Do(ctx, s, l); err != nil {
		return err
	}

	if err = ns.store.SetLayerScanned(ctx, l.Hash, s); err != nil {
		return fmt.Errorf("could not set layer scanned: %w", err)
	}

	return result.Store(ctx, ns.store, s, l)
}
