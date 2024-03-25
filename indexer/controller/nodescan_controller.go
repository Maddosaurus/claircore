package controller

import (
	"context"
	"errors"
	"fmt"
	"github.com/quay/claircore"
	"github.com/quay/claircore/indexer"
	"github.com/quay/zlog"
	"time"
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
	}

	return s
}

func (s *NodescanController) Index(ctx context.Context, _ *claircore.Manifest) (*claircore.IndexReport, error) {
	zlog.Error(ctx).Msg("Not implemented for Nodescan Controller. Use IndexNode!")
	return nil, errors.New("not implemented for Nodescan Controller. Use IndexNode")
}

// IndexNode kicks off an index of a node, given its filesystem
// Initial state set in the constructor.
func (s *NodescanController) IndexNode(ctx context.Context) (*claircore.IndexReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.report, s.runNodescan(ctx)
}

// Run executes each stateFunc and blocks until either an error occurs or a
// Terminal state is encountered.
func (s *NodescanController) runNodescan(ctx context.Context) (err error) {
	var next State
	var retry bool
	var w time.Duration

	// As long as there's not an error and the current state isn't Terminal, run
	// the corresponding function.
	for err == nil && s.currentState != Terminal {
		ctx := zlog.ContextWithValues(ctx, "state", s.currentState.String())
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
// provides a mapping of States to their implemented stateFunc methods
var nsStateToStateFunc = map[State]nodescanStateFunc{
	ScanLayers: scanFS,
}

// TODO: Move me
func scanFS(ctx context.Context, n *NodescanController) (State, error) {
	zlog.Info(ctx).Msg("FS scan start")
	defer zlog.Info(ctx).Msg("FS scan done")
	err := n.LayerScanner.Scan(ctx, claircore.Digest{}, nil)
	if err != nil {
		return Terminal, fmt.Errorf("failed to scan node filesystem: %w", err)
		zlog.Debug(ctx).Msg("FS scan ok")
	}
	return Terminal, nil // FIXME: Use correct status
}
