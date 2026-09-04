package replay

import (
	"testing"

	b "github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/blockstream"
)

func TestShouldDiscardLightbringerObservationAfterFallback(t *testing.T) {
	lightbringerBlock := &b.Block{Slot: 123, FromLightbringer: true}

	if !shouldDiscardLightbringerObservationAfterFallback(true, true, lightbringerBlock, blockstream.FetchStatsSnapshot{
		IsNearTip:     false,
		CurrentSource: "rpc",
	}) {
		t.Fatalf("expected Lightbringer observation to be discarded after catchup fallback")
	}

	if !shouldDiscardLightbringerObservationAfterFallback(true, true, lightbringerBlock, blockstream.FetchStatsSnapshot{
		IsNearTip:     true,
		CurrentSource: "rpc",
	}) {
		t.Fatalf("expected Lightbringer observation to be discarded while near-tip has not handed back to Lightbringer")
	}

	if shouldDiscardLightbringerObservationAfterFallback(true, true, lightbringerBlock, blockstream.FetchStatsSnapshot{
		IsNearTip:     true,
		CurrentSource: "lightbringer",
	}) {
		t.Fatalf("expected active Lightbringer observations to be retained")
	}

	if shouldDiscardLightbringerObservationAfterFallback(true, true, lightbringerBlock, blockstream.FetchStatsSnapshot{
		IsNearTip:     true,
		CurrentSource: "turbine",
	}) {
		t.Fatalf("expected active native turbine observations to be retained")
	}

	if shouldDiscardLightbringerObservationAfterFallback(true, true, &b.Block{Slot: 123}, blockstream.FetchStatsSnapshot{
		IsNearTip:     false,
		CurrentSource: "rpc",
	}) {
		t.Fatalf("expected RPC block to be retained")
	}
}

func TestResolveConsensusConfigAppliesToNativeTurbineByDefault(t *testing.T) {
	cfg := resolveConsensusConfig(nil, false, true, true)
	if !cfg.enforceActive {
		t.Fatalf("default stream consensus should apply to native turbine")
	}
	if cfg.enforceSource != "stream" {
		t.Fatalf("default consensus source = %q, want stream", cfg.enforceSource)
	}
	if cfg.bufferedExecutionActive {
		t.Fatalf("live turbine consensus should arm buffered execution only after handoff")
	}

	cfg = resolveConsensusConfig(&ConsensusOpts{EnforceOnSource: "lightbringer"}, false, true, true)
	if !cfg.enforceActive || cfg.enforceSource != "turbine" {
		t.Fatalf("legacy lightbringer consensus should be upgraded for native turbine, got active=%v source=%q", cfg.enforceActive, cfg.enforceSource)
	}
	if cfg.bufferedExecutionActive {
		t.Fatalf("live turbine consensus should arm buffered execution only after handoff")
	}

	cfg = resolveConsensusConfig(&ConsensusOpts{EnforceOnSource: "turbine"}, false, true, true)
	if !cfg.enforceActive {
		t.Fatalf("explicit turbine consensus should apply to native turbine")
	}
	if cfg.bufferedExecutionActive {
		t.Fatalf("live turbine consensus should arm buffered execution only after handoff")
	}

	cfg = resolveConsensusConfig(&ConsensusOpts{EnforceOnSource: "stream"}, false, true, true)
	if !cfg.enforceActive {
		t.Fatalf("stream consensus should apply to native turbine")
	}

	cfg = resolveConsensusConfig(&ConsensusOpts{EnforceOnSource: "lightbringer"}, true, false, true)
	if !cfg.enforceActive {
		t.Fatalf("lightbringer consensus should apply to lightbringer")
	}

	cfg = resolveConsensusConfig(&ConsensusOpts{EnforceOnSource: "all"}, false, false, true)
	if !cfg.enforceActive || !cfg.bufferedExecutionActive {
		t.Fatalf("all consensus should apply immediately")
	}
}
