package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/quay/claircore"
	"github.com/quay/claircore/indexer"
	"github.com/quay/zlog"
	"golang.org/x/sync/errgroup"
)

var (
	_ IndexController = (*NodescanController)(nil)
)

// NodescanController is a specialized controller for scanning running OSes instead of manifests.
type NodescanController struct {
	// holds dependencies for an indexer.controller
	*indexer.Options
	// the result of this scan. Each stateFunc manipulates this field.
	report *claircore.IndexReport
	// a fatal error halting the scanning process
	err error
	//the current state of the controller
	currentState State
	// Realizer is scoped to a single request
	Realizer indexer.Realizer
	// Vscnrs are the scanners that are used during indexing
	Vscnrs indexer.VersionedScanners
	// TODO: Probably not the  best way/place to store this
	nodeLayers []*claircore.Layer
}

// NewNodescanController constructs a controller given an Opts struct
func NewNodescanController(options *indexer.Options) *NodescanController {
	// fully init any maps and arrays
	scanRes := &claircore.IndexReport{
		Packages:      map[string]*claircore.Package{},
		Environments:  map[string][]*claircore.Environment{},
		Distributions: map[string]*claircore.Distribution{},
		Repositories:  map[string]*claircore.Repository{},
		Files:         map[string]claircore.File{},
	}

	s := &NodescanController{
		Options:      options,
		currentState: CheckManifest,
		report:       scanRes,
		Vscnrs:       options.Vscnrs,
		nodeLayers:   make([]*claircore.Layer, 1),
	}

	return s
}

func (s *NodescanController) Index(ctx context.Context, _ *claircore.Manifest) (*claircore.IndexReport, error) {
	zlog.Error(ctx).Msg("Not implemented for Nodescan Controller. Use IndexNode!")
	return nil, errors.New("not implemented for Nodescan Controller. Use IndexNode")
}

func getRandomSHA256() string {
	data := make([]byte, 10)
	rand.Read(data)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// IndexNode kicks off an index of a node, given its filesystem
// Initial state set in the constructor.
func (s *NodescanController) IndexNode(ctx context.Context) (*claircore.IndexReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sha := getRandomSHA256()
	ctx = context.WithValue(ctx, "manifest_id", sha)
	h, err := claircore.ParseDigest(`sha256:` + sha) // FIXME: Calc this hash on request, based on the FS
	if err != nil {
		return nil, err
	}
	s.report.Hash = h
	s.Realizer = s.FetchArena.Realizer(ctx)
	err = s.Realizer.Realize(ctx, s.nodeLayers) // FIXME: Do that differently
	if err != nil {
		return nil, err
	}
	err = s.checkMount(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Realizer.Close()
	return s.report, s.runNodescan(ctx)
}

func (s *NodescanController) checkMount(ctx context.Context) error {
	if len(s.nodeLayers) != 1 {
		return errors.New(fmt.Sprintf("unexpected amount of layers in slice. Expected 1, got %d", len(s.nodeLayers)))
	}
	fs, err := s.nodeLayers[0].FS()
	if err != nil {
		zlog.Error(ctx).
			Err(err).
			Msg("error opening filesystem")
		return err
	}
	_, err = fs.Open("etc") // FIXME: Do a better sanity check
	if err != nil {
		zlog.Error(ctx).
			Err(err).
			Msg("failed sanity check for filesystem. Ensure it is mounted correctly.")
	}
	return err
}

// Run executes each stateFunc and blocks until either an error occurs or a
// Terminal state is encountered.
func (s *NodescanController) runNodescan(ctx context.Context) (err error) {
	var next State
	var retry bool
	var w time.Duration
	zlog.Info(ctx).Msg("runNodescan starts")

	// As long as there's not an error and the current state isn't Terminal, run
	// the corresponding function.
	for err == nil && s.currentState != Terminal {
		ctx := zlog.ContextWithValues(ctx, "state", s.currentState.String())
		zlog.Info(ctx).Msgf("Currently report has %d packages", len(s.report.Packages))
		next, err = nsStateToStateFunc[s.currentState](ctx, s)
		switch {
		case errors.Is(err, nil) && !errors.Is(ctx.Err(), nil):
			// If the passed-in context reports an error, drop out of the loop.
			// This is an odd state but not impossible: a deadline could time
			// out while returning from the call above.
			//
			// In all the other switch arms, we now know that the parent context
			// is OK.
			err = ctx.Err()
			continue
		case errors.Is(err, nil):
			// OK
		case errors.Is(err, context.DeadlineExceeded):
			// Either the function's internal deadline or the parent's deadline
			// was hit.
			retry = true
		case errors.Is(err, context.Canceled):
			// The parent context was canceled and the stateFunc noticed.
			// Continuing the loop should drop execution out of it.
			continue
		default:
			s.setState(IndexError)
			zlog.Error(ctx).
				Err(err).
				Msg("error during scan")
			s.report.Success = false
			s.report.Err = err.Error()
		}
		if setReportErr := s.Store.SetIndexReport(ctx, s.report); !errors.Is(setReportErr, nil) {
			zlog.Info(ctx).
				Err(setReportErr).
				Msg("failed persisting index report")
			s.setState(IndexError)
			s.report.Err = fmt.Sprintf("failed persisting index report: %s", setReportErr.Error())
			err = setReportErr
			break
		}
		if retry {
			t := time.NewTimer(w)
			select {
			case <-ctx.Done():
			case <-t.C:
			}
			t.Stop()
			w = jitter()
			retry = false
			// Execution is in this conditional because err ==
			// context.DeadlineExceeded, so reset the err value to make sure the
			// loop makes it back to the error handling switch above. If the
			// context was canceled, the loop will end there; if not, there will
			// be a retry.
			err = nil
		}
		// This if statement preserves current behaviour of not setting
		// currentState to Terminal when it's returned. This should be an
		// internal detail, but is codified in the tests (for now).
		if next == Terminal {
			break
		}
		s.setState(next)
	}
	if err != nil {
		return err
	}
	return nil

}

// setState is a helper method to transition the controller to the provided next state
// FIXME: Dedup
func (s *NodescanController) setState(state State) {
	s.currentState = state
	s.report.State = state.String()
}

// FIXME: possibly dedup both of these
type nodescanStateFunc func(context.Context, *NodescanController) (State, error)

// Reduced state machine
// TODO: Add more states
// TODO: Move all of the state functions somewhere else
// provides a mapping of States to their implemented stateFunc methods
var nsStateToStateFunc = map[State]nodescanStateFunc{
	CheckManifest: advanceToFetch,
	FetchLayers:   prepareFS,
	ScanLayers:    scanFS,
	Coalesce:      coalesceFS,
	IndexManifest: indexFS,
	IndexFinished: indexFinishedFS,
	IndexError: func(ctx context.Context, controller *NodescanController) (State, error) {
		return Terminal, errors.New("Index Error state reached")
	},
	// FIXME: Add the remaining states, as the state machine needs them
}

// FIXME: Mostly 1:1 copy of controller.checkManifest
func advanceToFetch(ctx context.Context, n *NodescanController) (State, error) {
	// TODO: Check whether the node fs has changed, and persist the current checksum if so

	zlog.Info(ctx).Msg("advanceToFetch: start")
	defer zlog.Info(ctx).Msg("advanceToFetch: done")
	ok, err := n.Store.ManifestScanned(ctx, n.report.Hash, n.Vscnrs)
	if err != nil {
		zlog.Debug(ctx).Msg("advanceToFetch: error checking whether manifest was scanned")
		return Terminal, err
	}

	if ok {
		zlog.Info(ctx).Msg("advanceToFetch: Manifest is known, should not scan again")
	}

	if !ok {
		// FIXME: Reintroduce scanner selection here like in the controller state func
		zlog.Info(ctx).Msg("advanceToFetch: manifest will be scanned")

		m := claircore.Manifest{
			Hash:   n.report.Hash,
			Layers: n.nodeLayers,
		}
		err := n.Store.PersistManifest(ctx, m)
		if err != nil {
			return Terminal, fmt.Errorf("failed to persist manifest: %w", err)
		}
		return FetchLayers, nil
	}
	return FetchLayers, nil
}

func prepareFS(_ context.Context, n *NodescanController) (State, error) {
	return ScanLayers, nil
}

func scanFS(ctx context.Context, n *NodescanController) (State, error) {
	zlog.Info(ctx).Msg("FS scan start")
	defer zlog.Info(ctx).Msg("FS scan done")
	err := n.LayerScanner.Scan(ctx, n.report.Hash, n.nodeLayers)
	if err != nil {
		return Terminal, fmt.Errorf("failed to scan node filesystem: %w", err)
		zlog.Debug(ctx).Msg("FS scan ok")
	}
	return Coalesce, nil
}

// TODO: This is almost a 1:1 copy of controller.coalesce. Dedup!
func coalesceFS(ctx context.Context, n *NodescanController) (State, error) {
	mu := sync.Mutex{}
	reports := []*claircore.IndexReport{}
	g, gctx := errgroup.WithContext(ctx)
	// Dispatch a Coalescer goroutine for each ecosystem.
	for _, ecosystem := range n.Ecosystems {
		select {
		case <-gctx.Done():
			break
		default:
		}
		artifacts := []*indexer.LayerArtifacts{}
		pkgScanners, _ := ecosystem.PackageScanners(gctx)
		distScanners, _ := ecosystem.DistributionScanners(gctx)
		repoScanners, _ := ecosystem.RepositoryScanners(gctx)
		fileScanners := []indexer.FileScanner{}
		if ecosystem.FileScanners != nil {
			fileScanners, _ = ecosystem.FileScanners(gctx)
		}
		// Pack "artifacts" variable.
		for _, layer := range n.nodeLayers {
			la := &indexer.LayerArtifacts{
				Hash: layer.Hash,
			}
			var vscnrs indexer.VersionedScanners
			vscnrs.PStoVS(pkgScanners)
			// Get packages from layer.
			pkgs, err := n.Store.PackagesByLayer(gctx, layer.Hash, vscnrs)
			if err != nil {
				// On an early return gctx is canceled, and all in-flight
				// Coalescers are canceled as well.
				return Terminal, fmt.Errorf("failed to retrieve packages for %v: %w", layer.Hash, err)
			}
			la.Pkgs = append(la.Pkgs, pkgs...)
			// Get repos that have been created via the package scanners.
			pkgRepos, err := n.Store.RepositoriesByLayer(gctx, layer.Hash, vscnrs)
			if err != nil {
				return Terminal, fmt.Errorf("failed to retrieve repositories for %v: %w", layer.Hash, err)
			}
			la.Repos = append(la.Repos, pkgRepos...)

			// Get distributions from layer.
			vscnrs.DStoVS(distScanners) // Method allocates new "vscnr" underlying array, clearing old contents.
			dists, err := n.Store.DistributionsByLayer(gctx, layer.Hash, vscnrs)
			if err != nil {
				return Terminal, fmt.Errorf("failed to retrieve distributions for %v: %w", layer.Hash, err)
			}
			la.Dist = append(la.Dist, dists...)
			// Get repositories from layer.
			vscnrs.RStoVS(repoScanners)
			repos, err := n.Store.RepositoriesByLayer(gctx, layer.Hash, vscnrs)
			if err != nil {
				return Terminal, fmt.Errorf("failed to retrieve repositories for %v: %w", layer.Hash, err)
			}
			la.Repos = append(la.Repos, repos...)
			// Get files from layer.
			vscnrs.FStoVS(fileScanners)
			files, err := n.Store.FilesByLayer(gctx, layer.Hash, vscnrs)
			if err != nil {
				return Terminal, fmt.Errorf("failed to retrieve files for %v: %w", layer.Hash, err)
			}
			la.Files = append(la.Files, files...)
			// Pack artifacts array in layer order.
			artifacts = append(artifacts, la)
		}
		coalescer, err := ecosystem.Coalescer(gctx)
		if err != nil {
			return Terminal, fmt.Errorf("failed to get coalescer from ecosystem: %v", err)
		}
		// Dispatch.
		g.Go(func() error {
			sr, err := coalescer.Coalesce(gctx, artifacts)
			if err != nil {
				return err
			}

			mu.Lock()
			defer mu.Unlock()
			reports = append(reports, sr)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Terminal, err
	}
	n.report = MergeSR(n.report, reports)
	for _, r := range n.Resolvers {
		n.report = r.Resolve(ctx, n.report, n.nodeLayers)
	}
	return IndexManifest, nil
}

// TODO: If controller.indexManifest would be generalized, we could save on this one
func indexFS(ctx context.Context, n *NodescanController) (State, error) {
	if n.report == nil {
		return Terminal, fmt.Errorf("reached IndexManifest state with a nil report field. cannot continue")
	}
	err := n.Store.IndexManifest(ctx, n.report)
	if err != nil {
		return Terminal, fmt.Errorf("indexing manifest contents failed: %w", err)
	}
	return IndexFinished, nil
}

func indexFinishedFS(ctx context.Context, n *NodescanController) (State, error) {
	n.report.Success = true
	zlog.Info(ctx).Msg("finishing scan")

	err := n.Store.SetIndexFinished(ctx, n.report, n.Vscnrs)
	if err != nil {
		return Terminal, fmt.Errorf("failed finish scan: %w", err)
	}

	zlog.Info(ctx).Msg("node successfully scanned")
	return Terminal, nil
}
