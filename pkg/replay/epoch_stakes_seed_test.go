package replay

import (
	"encoding/json"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/epochstakes"
	"github.com/sonicfromnewyoke/mithril/pkg/state"
)

func TestPrepareManifestEpochStakesForRuntimeRepairsPreviouslyRebasedDevnetFrame(t *testing.T) {
	const (
		parentSlot    = 463538376
		snapshotEpoch = 1073
		currentEpoch  = 1073
	)

	mithrilState := &state.MithrilState{
		ManifestParentSlot:  parentSlot,
		ManifestEpochStakes: make(map[uint64]string),
	}
	for _, epoch := range []uint64{56581, 56582, 56583, 56584, 56585} {
		mithrilState.ManifestEpochStakes[epoch] = persistedEpochStakeJSON(t, epoch)
	}

	seeds, rebased, err := prepareManifestEpochStakesForRuntime(mithrilState, currentEpoch, snapshotEpoch)
	if err != nil {
		t.Fatalf("prepareManifestEpochStakesForRuntime returned error: %v", err)
	}
	if !rebased {
		t.Fatalf("expected manifest epoch stakes to be rebased")
	}

	wantEpochs := []uint64{1070, 1071, 1072, 1073, 1074}
	if len(seeds) != len(wantEpochs) {
		t.Fatalf("expected %d seeds, got %d", len(wantEpochs), len(seeds))
	}
	for i, wantEpoch := range wantEpochs {
		if seeds[i].runtimeEpoch != wantEpoch {
			t.Fatalf("seed %d runtime epoch = %d, want %d", i, seeds[i].runtimeEpoch, wantEpoch)
		}
		var persisted epochstakes.PersistedEpochStakes
		if err := json.Unmarshal(seeds[i].data, &persisted); err != nil {
			t.Fatalf("failed to decode seed %d: %v", i, err)
		}
		if persisted.Epoch != wantEpoch {
			t.Fatalf("seed %d payload epoch = %d, want %d", i, persisted.Epoch, wantEpoch)
		}
	}
}

func TestPrepareManifestEpochStakesForRuntimeKeepsRuntimeFrame(t *testing.T) {
	mithrilState := &state.MithrilState{
		ManifestParentSlot: 463538376,
		ManifestEpochStakes: map[uint64]string{
			1073: persistedEpochStakeJSON(t, 1073),
			1074: persistedEpochStakeJSON(t, 1074),
		},
	}

	seeds, rebased, err := prepareManifestEpochStakesForRuntime(mithrilState, 1073, 1073)
	if err != nil {
		t.Fatalf("prepareManifestEpochStakesForRuntime returned error: %v", err)
	}
	if rebased {
		t.Fatalf("did not expect manifest epoch stakes to be rebased")
	}
	if len(seeds) != 2 || seeds[0].runtimeEpoch != 1073 || seeds[1].runtimeEpoch != 1074 {
		t.Fatalf("unexpected seeds: %#v", seeds)
	}
}

func persistedEpochStakeJSON(t *testing.T, epoch uint64) string {
	t.Helper()

	data, err := json.Marshal(epochstakes.PersistedEpochStakes{
		Epoch:      epoch,
		TotalStake: 42,
		Stakes:     map[string]uint64{},
		VoteAccts:  map[string]*epochstakes.VoteAccountJSON{},
	})
	if err != nil {
		t.Fatalf("failed to marshal epoch stakes: %v", err)
	}
	return string(data)
}
