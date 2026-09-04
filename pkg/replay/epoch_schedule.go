package replay

import (
	"fmt"

	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/sonicfromnewyoke/mithril/pkg/state"
)

func bankEpochScheduleFromState(s *state.MithrilState) (*sealevel.SysvarEpochSchedule, bool) {
	if s == nil || s.ManifestEpochSchedule == nil || s.ManifestEpochSchedule.SlotsPerEpoch == 0 {
		return nil, false
	}

	return &sealevel.SysvarEpochSchedule{
		SlotsPerEpoch:            s.ManifestEpochSchedule.SlotsPerEpoch,
		LeaderScheduleSlotOffset: s.ManifestEpochSchedule.LeaderScheduleSlotOffset,
		Warmup:                   s.ManifestEpochSchedule.Warmup,
		FirstNormalEpoch:         s.ManifestEpochSchedule.FirstNormalEpoch,
		FirstNormalSlot:          s.ManifestEpochSchedule.FirstNormalSlot,
	}, true
}

func bankEpochScheduleForReplay(s *state.MithrilState) (*sealevel.SysvarEpochSchedule, bool, error) {
	if epochSchedule, ok := bankEpochScheduleFromState(s); ok {
		return epochSchedule, true, nil
	}
	if sealevel.SysvarCache.EpochSchedule.Sysvar != nil {
		return sealevel.SysvarCache.EpochSchedule.Sysvar, false, nil
	}
	return nil, false, fmt.Errorf("missing epoch schedule")
}

func epochSchedulesEqual(a, b *sealevel.SysvarEpochSchedule) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.SlotsPerEpoch == b.SlotsPerEpoch &&
		a.LeaderScheduleSlotOffset == b.LeaderScheduleSlotOffset &&
		a.Warmup == b.Warmup &&
		a.FirstNormalEpoch == b.FirstNormalEpoch &&
		a.FirstNormalSlot == b.FirstNormalSlot
}
