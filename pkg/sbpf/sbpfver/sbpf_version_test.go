package sbpfver

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/stretchr/testify/require"
)

func TestGetMinAndMaxSbpfVersionsAcceptsSbpfV3Gate(t *testing.T) {
	f := features.NewFeaturesDefault()
	f.EnableFeature(features.EnableSbpfV3DeploymentAndExecution, 0)

	_, maxVersion := GetMinAndMaxSbpfVersions(f)
	require.Equal(t, uint32(SbpfVersionV3), maxVersion)
}

func TestSbpfV3VersionPredicates(t *testing.T) {
	v3 := SbpfVersion{Version: SbpfVersionV3}

	require.False(t, v3.DynamicStackFrames())
	require.False(t, v3.StackFrameGaps())
	require.False(t, v3.EnablePqr())
	require.False(t, v3.DisableLddw())
	require.True(t, v3.EnableStaticSyscalls())
	require.True(t, v3.EnableStricterElfHeaders())
	require.True(t, v3.EnableJmp32())
	require.True(t, v3.CallXUsesDstReg())
}
