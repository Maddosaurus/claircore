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
func (ns *NodeScanner) Scan(ctx context.Context, _ claircore.Digest, _ []*claircore.Layer) error {
	//nodeFS := os.DirFS(mountLocation)
	//nodeLayer := claircore.Layer{
	//	Hash: claircore.Digest{},
	//}
	// TODO: How to create a layer out of the real FS?
	return nil
}
