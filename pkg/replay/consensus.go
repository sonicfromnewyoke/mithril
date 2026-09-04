package replay

import (
	b "github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/blockstream"
	"github.com/sonicfromnewyoke/mithril/pkg/forkchoice"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/gagliardetto/solana-go"
)

const (
	defaultConsensusMaxDepth      = 64
	defaultConsensusPolicy        = "halt"
	defaultConsensusEnforceSource = "stream"
)

// ConsensusOpts contains vote-anchored consensus configuration.
// Nil means use defaults (max_depth=64, policy="halt").
type ConsensusOpts struct {
	SkipPathMaxDepth int    // Max slots for skip-path solver (default: 64)
	UnresolvedPolicy string // "halt" or "warn" (default: "halt")
	EnforceOnSource  string // "lightbringer", "turbine", "stream", or "all" (default: "stream")
}

type consensusConfig struct {
	maxDepth                int
	policy                  string
	enforceSource           string
	enforceActive           bool
	bufferedExecutionActive bool
}

// pendingConsensusPath tracks a vote-resolved path that replay has observed but
// has not yet executed through to the confirmed leaf.
type pendingConsensusPath struct {
	anchorSlot        uint64
	leafSlot          uint64
	leafBankhash      solana.Hash
	decisions         []forkchoice.SlotDecision
	originalDecisions []forkchoice.SlotDecision
}

func resolveConsensusConfig(opts *ConsensusOpts, useLightbringer, useTurbine, isLive bool) consensusConfig {
	cfg := consensusConfig{
		maxDepth:      defaultConsensusMaxDepth,
		policy:        defaultConsensusPolicy,
		enforceSource: defaultConsensusEnforceSource,
	}

	if opts != nil {
		if opts.SkipPathMaxDepth > 0 {
			cfg.maxDepth = opts.SkipPathMaxDepth
		}
		if opts.UnresolvedPolicy != "" {
			cfg.policy = opts.UnresolvedPolicy
		}
		if opts.EnforceOnSource != "" {
			cfg.enforceSource = opts.EnforceOnSource
		}
	}

	if isLive && useTurbine && !useLightbringer && cfg.enforceSource == "lightbringer" {
		mlog.Log.Warnf("forkchoice: consensus.enforce_on_source=%q is legacy Lightbringer-only while block source is native turbine; treating it as %q for this run",
			cfg.enforceSource, "turbine")
		cfg.enforceSource = "turbine"
	}

	switch cfg.enforceSource {
	case "lightbringer", "turbine", "stream", "all":
	default:
		mlog.Log.Warnf("forkchoice: invalid EnforceOnSource=%q, defaulting to %q", cfg.enforceSource, defaultConsensusEnforceSource)
		cfg.enforceSource = defaultConsensusEnforceSource
	}

	cfg.enforceActive = consensusAppliesToRun(cfg.enforceSource, useLightbringer, useTurbine)
	cfg.bufferedExecutionActive = !isLive || cfg.enforceSource == "all"
	return cfg
}

func consensusAppliesToRun(enforceSource string, useLightbringer, useTurbine bool) bool {
	switch enforceSource {
	case "all":
		return true
	case "stream":
		return useLightbringer || useTurbine
	case "lightbringer":
		return useLightbringer
	case "turbine":
		return useTurbine
	default:
		return false
	}
}

func consensusManagesLiveShredStream(enforceSource string, useLightbringer, useTurbine bool) bool {
	switch enforceSource {
	case "stream", "all":
		return useLightbringer || useTurbine
	case "lightbringer":
		return useLightbringer
	case "turbine":
		return useTurbine
	default:
		return false
	}
}

func newPendingConsensusPath(anchorSlot uint64, resolvedPath *forkchoice.ResolvedPath) *pendingConsensusPath {
	if resolvedPath == nil {
		return nil
	}
	decisions := append([]forkchoice.SlotDecision(nil), resolvedPath.SlotDecisions...)
	return &pendingConsensusPath{
		anchorSlot:        anchorSlot,
		leafSlot:          resolvedPath.LeafSlot,
		leafBankhash:      resolvedPath.LeafBankhash,
		decisions:         append([]forkchoice.SlotDecision(nil), decisions...),
		originalDecisions: decisions,
	}
}

func pruneObservedConsensusBlocks(blocks map[uint64]*b.Block, anchorSlot uint64) {
	if blocks == nil || anchorSlot == 0 {
		return
	}
	for slot := range blocks {
		if slot <= anchorSlot {
			delete(blocks, slot)
		}
	}
}

func clearObservedConsensusBlocks(blocks map[uint64]*b.Block) {
	for slot := range blocks {
		delete(blocks, slot)
	}
}

func shouldDiscardLightbringerObservationAfterFallback(isLive, useLightbringer bool, block *b.Block, stats blockstream.FetchStatsSnapshot) bool {
	return isLive &&
		useLightbringer &&
		block != nil &&
		block.FromLightbringer &&
		(!stats.IsNearTip || (stats.CurrentSource != "lightbringer" && stats.CurrentSource != "turbine"))
}
