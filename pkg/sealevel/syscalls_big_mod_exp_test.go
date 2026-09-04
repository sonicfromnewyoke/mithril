package sealevel

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigModExpAgaveVectors is the full test-vector set from the solana-big-mod-exp
// crate (tests/data/big_mod_exp_cases.json), including the zero-modulus and
// modulus-one edge cases.
var bigModExpAgaveVectors = []struct {
	base     string
	exponent string
	modulus  string
	expected string
}{
	{
		"1111111111111111111111111111111111111111111111111111111111111111",
		"1111111111111111111111111111111111111111111111111111111111111111",
		"111111111111111111111111111111111111111111111111111111111111110A",
		"0A7074864588D6847F33A168209E516F60005A0CEC3F33AAF70E8002FE964BCD",
	},
	{
		"2222222222222222222222222222222222222222222222222222222222222222",
		"2222222222222222222222222222222222222222222222222222222222222222",
		"1111111111111111111111111111111111111111111111111111111111111111",
		"0000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		"3333333333333333333333333333333333333333333333333333333333333333",
		"3333333333333333333333333333333333333333333333333333333333333333",
		"2222222222222222222222222222222222222222222222222222222222222222",
		"1111111111111111111111111111111111111111111111111111111111111111",
	},
	{
		"9874231472317432847923174392874918237439287492374932871937289719",
		"0948403985401232889438579475812347232099080051356165126166266222",
		"25532321a214321423124212222224222b242222222222222222222222222444",
		"220ECE1C42624E98AEE7EB86578B2FE5C4855DFFACCB43CCBB708A3AB37F184D",
	},
	{
		"3494396663463663636363662632666565656456646566786786676786768766",
		"2324324333246536456354655645656616169896565698987033121934984955",
		"0218305479243590485092843590249879879842313131156656565565656566",
		"012F2865E8B9E79B645FCE3A9E04156483AE1F9833F6BFCF86FCA38FC2D5BEF0",
	},
	{
		"0000000000000000000000000000000000000000000000000000000000000005",
		"0000000000000000000000000000000000000000000000000000000000000002",
		"0000000000000000000000000000000000000000000000000000000000000007",
		"0000000000000000000000000000000000000000000000000000000000000004",
	},
	{
		"0000000000000000000000000000000000000000000000000000000000000019",
		"0000000000000000000000000000000000000000000000000000000000000019",
		"0000000000000000000000000000000000000000000000000000000000000064",
		"0000000000000000000000000000000000000000000000000000000000000019",
	},
	{
		// zero modulus: result is all zeroes of modulus length
		"0000000000000000000000000000000000000000000000000000000000000019",
		"0000000000000000000000000000000000000000000000000000000000000019",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"0000000000000000000000000000000000000000000000000000000000000000",
	},
	{
		// modulus one: result is all zeroes of modulus length
		"0000000000000000000000000000000000000000000000000000000000000019",
		"0000000000000000000000000000000000000000000000000000000000000019",
		"0000000000000000000000000000000000000000000000000000000000000001",
		"0000000000000000000000000000000000000000000000000000000000000000",
	},
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestBigModExp_AgaveVectors(t *testing.T) {
	for i, vec := range bigModExpAgaveVectors {
		got := bigModExp(mustHex(t, vec.base), mustHex(t, vec.exponent), mustHex(t, vec.modulus))
		assert.Equal(t, mustHex(t, vec.expected), got, "vector %d", i)
	}
}

func TestBigModExp_EdgeCases(t *testing.T) {
	// zero exponent: base^0 mod m == 1, left-padded to modulus length
	assert.Equal(t, []byte{0, 1}, bigModExp([]byte{9}, []byte{}, []byte{0, 7}))
	// zero-length modulus yields a zero-length result
	assert.Equal(t, []byte{}, bigModExp([]byte{9}, []byte{2}, []byte{}))
	// zero-length base is treated as zero: 0^2 mod 7 == 0
	assert.Equal(t, []byte{0}, bigModExp([]byte{}, []byte{2}, []byte{7}))
	// result shorter than the modulus buffer is left-padded with zeroes
	assert.Equal(t, []byte{0, 0, 3}, bigModExp([]byte{2}, []byte{3}, []byte{0, 0, 5}))
}

// writeBigModExpParams lays out Agave's BigModExpParams struct
// {base_addr, base_len, exponent_addr, exponent_len, modulus_addr,
// modulus_len} at paramsAddr and copies the operand bytes into the mock
// VM memory. It returns the address of the return buffer (modulus-length
// bytes directly after the operands).
func writeBigModExpParams(vm *bls12_381SyscallTestVM, paramsAddr uint64, base, exponent, modulus []byte) uint64 {
	baseAddr := paramsAddr + bigModExpParamsSize
	expAddr := baseAddr + uint64(len(base))
	modAddr := expAddr + uint64(len(exponent))
	returnAddr := modAddr + uint64(len(modulus))

	binary.LittleEndian.PutUint64(vm.mem[paramsAddr:], baseAddr)
	binary.LittleEndian.PutUint64(vm.mem[paramsAddr+8:], uint64(len(base)))
	binary.LittleEndian.PutUint64(vm.mem[paramsAddr+16:], expAddr)
	binary.LittleEndian.PutUint64(vm.mem[paramsAddr+24:], uint64(len(exponent)))
	binary.LittleEndian.PutUint64(vm.mem[paramsAddr+32:], modAddr)
	binary.LittleEndian.PutUint64(vm.mem[paramsAddr+40:], uint64(len(modulus)))

	copy(vm.mem[baseAddr:], base)
	copy(vm.mem[expAddr:], exponent)
	copy(vm.mem[modAddr:], modulus)

	return returnAddr
}

func TestSyscallBigModExp_Success(t *testing.T) {
	vm := newBls12_381SyscallTestVM(4096)

	base := mustHex(t, bigModExpAgaveVectors[0].base)
	exponent := mustHex(t, bigModExpAgaveVectors[0].exponent)
	modulus := mustHex(t, bigModExpAgaveVectors[0].modulus)
	expected := mustHex(t, bigModExpAgaveVectors[0].expected)

	returnAddr := writeBigModExpParams(vm, 0, base, exponent, modulus)

	r0, err := SyscallBigModExpImpl(vm, 0, returnAddr)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), r0)
	assert.Equal(t, expected, vm.mem[returnAddr:returnAddr+uint64(len(modulus))])

	// CU cost: syscall_base_cost + max_input_len^2 / divisor + base cost
	// (agave: "0.5 input_len^2 + 190"); input_len = 32 here.
	wantCost := uint64(cu.CUSyscallBaseCost) + (32*32)/uint64(cu.CUBigModExpCostDivisor) + uint64(cu.CUBigModExpBaseCost)
	assert.Equal(t, uint64(1_000_000)-wantCost, vm.ComputeMeter().Remaining())
}

func TestSyscallBigModExp_InputLengthCap(t *testing.T) {
	vm := newBls12_381SyscallTestVM(4096)

	// base_len 513 exceeds Agave's 512-byte cap; the check runs before any
	// compute is consumed.
	base := make([]byte, 513)
	returnAddr := writeBigModExpParams(vm, 0, base, []byte{1}, []byte{7})

	_, err := SyscallBigModExpImpl(vm, 0, returnAddr)
	require.ErrorIs(t, err, SyscallErrInvalidLength)
	assert.Equal(t, uint64(1_000_000), vm.ComputeMeter().Remaining())
}

func TestSyscallBigModExp_MaxLenCharging(t *testing.T) {
	vm := newBls12_381SyscallTestVM(4096)

	// The cost is driven by max(base_len, exponent_len, modulus_len):
	// a 512-byte exponent dominates the 1-byte base/modulus.
	exponent := make([]byte, 512)
	returnAddr := writeBigModExpParams(vm, 0, []byte{2}, exponent, []byte{7})

	r0, err := SyscallBigModExpImpl(vm, 0, returnAddr)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), r0)
	// 2^0 mod 7 == 1
	assert.Equal(t, []byte{1}, vm.mem[returnAddr:returnAddr+1])

	wantCost := uint64(cu.CUSyscallBaseCost) + (512*512)/uint64(cu.CUBigModExpCostDivisor) + uint64(cu.CUBigModExpBaseCost)
	assert.Equal(t, uint64(1_000_000)-wantCost, vm.ComputeMeter().Remaining())
}

func TestSyscallBigModExp_UnalignedParams(t *testing.T) {
	vm := newBls12_381SyscallTestVM(4096)

	returnAddr := writeBigModExpParams(vm, 8, []byte{2}, []byte{3}, []byte{5})

	// params pointer must be 8-byte aligned when the loader enforces
	// alignment (mock ctx has no deprecated-loader program: enforced).
	_, err := SyscallBigModExpImpl(vm, 8+4, returnAddr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UnalignedPointer")
}

func TestSyscallBigModExp_OutOfBoundsOperand(t *testing.T) {
	vm := newBls12_381SyscallTestVM(64)

	// Params claim a base slice beyond the mapped memory: translation of
	// the operand fails after the cost was charged.
	binary.LittleEndian.PutUint64(vm.mem[0:], 1<<32) // base addr (unmapped)
	binary.LittleEndian.PutUint64(vm.mem[8:], 4)     // base len
	binary.LittleEndian.PutUint64(vm.mem[16:], 48)   // exponent addr
	binary.LittleEndian.PutUint64(vm.mem[24:], 1)    // exponent len
	binary.LittleEndian.PutUint64(vm.mem[32:], 49)   // modulus addr
	binary.LittleEndian.PutUint64(vm.mem[40:], 1)    // modulus len

	_, err := SyscallBigModExpImpl(vm, 0, 50)
	require.Error(t, err)

	wantCost := uint64(cu.CUSyscallBaseCost) + (4*4)/uint64(cu.CUBigModExpCostDivisor) + uint64(cu.CUBigModExpBaseCost)
	assert.Equal(t, uint64(1_000_000)-wantCost, vm.ComputeMeter().Remaining())
}

func TestSyscallRemainingComputeUnits(t *testing.T) {
	vm := newBls12_381SyscallTestVM(8)

	r0, err := SyscallRemainingComputeUnitsImpl(vm)
	require.NoError(t, err)
	// The syscall base cost is consumed first; r0 is the remainder.
	assert.Equal(t, uint64(1_000_000-cu.CUSyscallBaseCost), r0)
	assert.Equal(t, uint64(1_000_000-cu.CUSyscallBaseCost), vm.ComputeMeter().Remaining())

	r0, err = SyscallRemainingComputeUnitsImpl(vm)
	require.NoError(t, err)
	assert.Equal(t, uint64(1_000_000-2*cu.CUSyscallBaseCost), r0)
}

func TestSyscallRemainingComputeUnits_Exhausted(t *testing.T) {
	vm := newBls12_381SyscallTestVM(8)
	require.NoError(t, vm.ComputeMeter().Consume(1_000_000-cu.CUSyscallBaseCost+1))

	_, err := SyscallRemainingComputeUnitsImpl(vm)
	require.ErrorIs(t, err, InstrErrComputationalBudgetExceeded)
}

// TestSyscalls_BigModExpAndRemainingCUs_FeatureGated checks the registration
// path: both syscalls resolve only when their agave-feature-set gate is
// active (enable_big_mod_exp_syscall / remaining_compute_units_syscall_enabled).
func TestSyscalls_BigModExpAndRemainingCUs_FeatureGated(t *testing.T) {
	ft := features.NewFeaturesDefault()
	_, ok := Syscalls(ft, false, hash_sol_big_mod_exp)
	assert.False(t, ok)
	_, ok = Syscalls(ft, false, hash_sol_remaining_compute_units)
	assert.False(t, ok)

	ft.EnableFeature(features.EnableBigModExpSyscall, 0)
	ft.EnableFeature(features.RemainingComputeUnitsSyscallEnabled, 0)
	f, ok := Syscalls(ft, false, hash_sol_big_mod_exp)
	assert.True(t, ok)
	assert.NotNil(t, f)
	f, ok = Syscalls(ft, false, hash_sol_remaining_compute_units)
	assert.True(t, ok)
	assert.NotNil(t, f)
}
