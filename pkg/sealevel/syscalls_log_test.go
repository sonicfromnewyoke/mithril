package sealevel

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyscallLogRejectsInvalidUTF8(t *testing.T) {
	vm := newBls12_381SyscallTestVM(4)
	vm.mem[0] = 0xff

	_, err := SyscallLogImpl(vm, 0, 1)
	require.ErrorIs(t, err, SyscallErrInvalidString)
	assert.Equal(t, uint64(1_000_000-cu.CUSyscallBaseCost), vm.ComputeMeter().Remaining())
}
