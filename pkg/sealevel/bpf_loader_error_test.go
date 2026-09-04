package sealevel

import (
	"fmt"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProgramRunErrMapsOutOfCU(t *testing.T) {
	err := &sbpf.Exception{Detail: fmt.Errorf("wrapped: %w", sbpf.ExcOutOfCU)}

	require.ErrorIs(t, normalizeProgramRunErr(err), InstrErrProgramFailedToComplete)
	require.ErrorIs(t, normalizeProgramRunErr(cu.ErrComputeExceeded), InstrErrProgramFailedToComplete)
}

func TestNormalizeProgramRunErrPreservesSyscallInstructionErr(t *testing.T) {
	err := &sbpf.Exception{Detail: fmt.Errorf("wrapped: %w", sbpf.ExcSyscallError{Err: InstrErrComputationalBudgetExceeded})}

	require.ErrorIs(t, normalizeProgramRunErr(err), InstrErrComputationalBudgetExceeded)
}

func TestInstrErrFromProgramStatus(t *testing.T) {
	require.ErrorAs(t, instrErrFromProgramStatus(uint64(1)<<32), &InstrErrCustomCode{})
	require.ErrorIs(t, instrErrFromProgramStatus(uint64(2)<<32), InstrErrInvalidArgument)
	require.ErrorIs(t, instrErrFromProgramStatus(uint64(3)<<32), InstrErrInvalidInstructionData)
	require.ErrorIs(t, instrErrFromProgramStatus(uint64(23)<<32), InstrErrInvalidAccountOwner)
	require.ErrorIs(t, instrErrFromProgramStatus(uint64(26)<<32), InstrErrIncorrectAuthority)

	var custom InstrErrCustomCode
	require.ErrorAs(t, instrErrFromProgramStatus(42), &custom)
	require.Equal(t, uint32(42), custom.Code)
	require.ErrorIs(t, instrErrFromProgramStatus(uint64(99)<<32), InstrErrInvalidError)
}
