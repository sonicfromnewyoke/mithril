package sealevel

import (
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyscallTryFindProgramAddressRejectsUnalignedSeedVector(t *testing.T) {
	vm := newBls12_381SyscallTestVM(128)

	seedsAddr := uint64(1)
	binary.LittleEndian.PutUint64(vm.mem[seedsAddr:seedsAddr+8], 24)
	binary.LittleEndian.PutUint64(vm.mem[seedsAddr+8:seedsAddr+16], 1)
	vm.mem[24] = 'x'

	_, err := SyscallTryFindProgramAddressImpl(vm, seedsAddr, 1, 40, 72, 104)
	require.EqualError(t, err, "SyscallError::UnalignedPointer")
	assert.Equal(t, uint64(1_000_000-cu.CUCreateProgramAddressUnits), vm.ComputeMeter().Remaining())
}
