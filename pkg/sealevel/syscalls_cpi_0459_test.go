package sealevel

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	feat "github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/stretchr/testify/require"
)

func TestSyscallParameterAddressRangeRestricted(t *testing.T) {
	features := feat.NewFeaturesDefault()
	execCtx := &ExecutionCtx{Features: *features}

	require.False(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput-1, 1))

	features.EnableFeature(feat.SyscallParameterAddressRestrictions, 0)
	require.True(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput, 0))
	require.True(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput-1, 1))
	require.False(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput-2, 1))
}

func TestCheckCpiDataLengthSyscallParameterAddressRestrictions(t *testing.T) {
	features := feat.NewFeaturesDefault()
	execCtx := &ExecutionCtx{Features: *features}

	require.NoError(t, checkCpiDataLength(execCtx, MaxPermittedDataIncrease+2, 1, true))

	features.EnableFeature(feat.SyscallParameterAddressRestrictions, 0)
	require.NoError(t, checkCpiDataLength(execCtx, 1, 1, true))
	require.ErrorIs(t, checkCpiDataLength(execCtx, 2, 1, true), InstrErrInvalidRealloc)
	require.NoError(t, checkCpiDataLength(execCtx, MaxPermittedDataIncrease+1, 1, false))
	require.ErrorIs(t, checkCpiDataLength(execCtx, MaxPermittedDataIncrease+2, 1, false), InstrErrInvalidRealloc)
}

func TestTranslateSerializedAccountDataDirectMappingSkipsSerializedBytes(t *testing.T) {
	features := feat.NewFeaturesDefault()
	features.EnableFeature(feat.VirtualAddressSpaceAdjustments, 0)
	features.EnableFeature(feat.AccountDataDirectMapping, 0)
	execCtx := &ExecutionCtx{Features: *features}

	meter := cu.NewComputeMeter(1)
	vm := sbpf.NewInterpreter(&sbpf.Program{TextVA: sbpf.VaddrProgram, Funcs: map[uint32]int64{}}, &sbpf.VMOpts{
		Input:        []byte{0x42},
		Context:      execCtx,
		ComputeMeter: &meter,
	})
	defer vm.Finish()

	data, err := translateSerializedAccountData(vm, execCtx, sbpf.VaddrInput+42, 1)
	require.NoError(t, err)
	require.Nil(t, data)

	features.DisableFeature(feat.AccountDataDirectMapping)
	_, err = translateSerializedAccountData(vm, execCtx, sbpf.VaddrInput+42, 1)
	require.Error(t, err)
}
