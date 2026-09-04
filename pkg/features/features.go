package features

import (
	"fmt"

	"github.com/sonicfromnewyoke/mithril/pkg/base58"
)

type FeatureGate struct {
	Name    string
	Address [32]byte
}

type FeatureActivationInfo struct {
	Enabled        bool
	ActivationSlot uint64
}

type Features map[FeatureGate]FeatureActivationInfo

func NewFeaturesDefault() *Features {
	newMap := make(Features, 0)
	return &newMap
}

func (f *Features) EnableFeature(gate FeatureGate, activationSlot uint64) {
	(*f)[gate] = FeatureActivationInfo{Enabled: true, ActivationSlot: activationSlot}
}

func (f *Features) DisableFeature(gate FeatureGate) {
	(*f)[gate] = FeatureActivationInfo{Enabled: false}
}

func (f *Features) IsActive(gate FeatureGate) bool {
	if info, found := (*f)[gate]; found {
		return info.Enabled
	} else {
		return false
	}
}

func (f *Features) IsSbpfV3DeploymentAndExecutionActive() bool {
	return f.IsActive(EnableSbpfV3DeploymentAndExecution)
}

func (f *Features) ActivationSlot(gate FeatureGate) (uint64, bool) {
	if !f.IsActive(gate) {
		return 0, false
	}
	return (*f)[gate].ActivationSlot, true
}

func (f *Features) AllEnabled() []string {
	enabledFeatureStrs := make([]string, 0)
	for feat, enabled := range *f {
		if enabled.Enabled {
			enabledFeatureStrs = append(enabledFeatureStrs, fmt.Sprintf("feature %s (%s) enabled", feat.Name, base58.Encode(feat.Address[:])))
		}
	}
	return enabledFeatureStrs
}

func (f *Features) FullInflationFeaturesEnabled() []FeatureGate {
	var enabledFeatures []FeatureGate

	if f.IsActive(FullInflationVote) && f.IsActive(FullInflationEnable) {
		enabledFeatures = append(enabledFeatures, FullInflationEnable)
	}

	if f.IsActive(FullInflationDevnetAndTestnet) {
		enabledFeatures = append(enabledFeatures, FullInflationDevnetAndTestnet)
	}

	return enabledFeatures
}
