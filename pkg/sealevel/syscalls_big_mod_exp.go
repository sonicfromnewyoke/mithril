package sealevel

import (
	"encoding/binary"
	"math/big"

	"github.com/Overclock-Validator/mithril/pkg/cu"
	"github.com/Overclock-Validator/mithril/pkg/safemath"
	"github.com/Overclock-Validator/mithril/pkg/sbpf"
)

// bigModExpParamsSize is the size of Agave's BigModExpParams struct: three
// (ptr: u64, len: u64) pairs for base, exponent and modulus.
const bigModExpParamsSize = 48

// bigModExpMaxInputLen mirrors Agave's SyscallBigModExp input cap: each of
// base, exponent and modulus may be at most 512 bytes long.
const bigModExpMaxInputLen = 512

// bigModExp ports Agave's solana_big_mod_exp::big_mod_exp. All inputs are
// big-endian unsigned integers; the result is (base ^ exponent) % modulus,
// left-padded with zeroes to the length of the modulus buffer. A modulus of
// zero or one yields an all-zero result of the same length.
func bigModExp(base, exponent, modulus []byte) []byte {
	modulusLen := len(modulus)
	out := make([]byte, modulusLen)

	m := new(big.Int).SetBytes(modulus)
	if m.Sign() == 0 || m.Cmp(big.NewInt(1)) == 0 {
		return out
	}

	b := new(big.Int).SetBytes(base)
	e := new(big.Int).SetBytes(exponent)

	ret := new(big.Int).Exp(b, e, m).Bytes()
	copy(out[modulusLen-len(ret):], ret)
	return out
}

// SyscallBigModExpImpl implements the sol_big_mod_exp syscall with Agave
// semantics (agave-syscalls SyscallBigModExp): r1 points at a BigModExpParams
// struct, r2 at the return buffer which receives modulus_len bytes.
//
// IMPORTANT - PINNED SEMANTICS / FUTURE ACTIVATION HAZARD:
// This implementation was verified byte-for-byte (param layout, 512-byte
// input caps, 100 + n*n/2 + 190 CU formula, big-endian padding, zero/one
// modulus handling, Ok(0) return) against the pinned agave generation this
// fork targets: agave-syscalls-4.0.0 and solana-big-mod-exp-3.0.0. Rust
// litesvm 0.13 with the gate enabled runs these same semantics, and that
// parity is intentional. The feature gate
// EBq48m8irRKuE7ZnMTLvLg2UuGSqhe8s8oMqnmja1fJw (enable_big_mod_exp_syscall)
// is INACTIVE on mainnet as of 2026-07.
//
// However, agave master has REPLACED this syscall's body with an Ok(1) no-op
// stub (no CU consumption, no translation, no memory writes) pending
// SIMD-0529. IF this gate ever activates on mainnet, mainnet agave will run
// the stub/SIMD-0529 semantics while this code runs the old removed
// implementation, diverging on compute-meter state, return value (0 vs 1)
// and memory effects on the first sol_big_mod_exp call - a bank hash
// mismatch. This implementation MUST be revisited BEFORE the activation
// epoch or the node will fork. pkg/replay scanAndEnableFeatures logs a loud
// warning if the gate is ever seen active.
func SyscallBigModExpImpl(vm sbpf.VM, paramsAddr, returnAddr uint64) (uint64, error) {
	execCtx := executionCtx(vm)

	if syscallAddressRequiresAlignment(execCtx, paramsAddr, 8) {
		return syscallErrCustom("SyscallError::UnalignedPointer")
	}

	paramsMem, err := vm.Translate(paramsAddr, bigModExpParamsSize, false)
	if err != nil {
		return syscallErr(err)
	}

	baseAddr := binary.LittleEndian.Uint64(paramsMem[0:8])
	baseLen := binary.LittleEndian.Uint64(paramsMem[8:16])
	exponentAddr := binary.LittleEndian.Uint64(paramsMem[16:24])
	exponentLen := binary.LittleEndian.Uint64(paramsMem[24:32])
	modulusAddr := binary.LittleEndian.Uint64(paramsMem[32:40])
	modulusLen := binary.LittleEndian.Uint64(paramsMem[40:48])

	if baseLen > bigModExpMaxInputLen || exponentLen > bigModExpMaxInputLen || modulusLen > bigModExpMaxInputLen {
		return syscallErr(SyscallErrInvalidLength)
	}

	inputLen := max(baseLen, exponentLen, modulusLen)

	// "the compute units are calculated by the quadratic equation
	// `0.5 input_len^2 + 190`" (plus the syscall base cost).
	cost := safemath.SaturatingAddU64(cu.CUSyscallBaseCost,
		safemath.SaturatingAddU64(
			safemath.SaturatingMulU64(inputLen, inputLen)/cu.CUBigModExpCostDivisor,
			cu.CUBigModExpBaseCost))
	err = execCtx.ComputeMeter.Consume(cost)
	if err != nil {
		return syscallCuErr()
	}

	base, err := vm.Translate(baseAddr, baseLen, false)
	if err != nil {
		return syscallErr(err)
	}

	exponent, err := vm.Translate(exponentAddr, exponentLen, false)
	if err != nil {
		return syscallErr(err)
	}

	modulus, err := vm.Translate(modulusAddr, modulusLen, false)
	if err != nil {
		return syscallErr(err)
	}

	value := bigModExp(base, exponent, modulus)

	returnValue, err := vm.Translate(returnAddr, modulusLen, true)
	if err != nil {
		return syscallErr(err)
	}
	copy(returnValue, value)

	return syscallSuccess(0)
}

var SyscallBigModExp = sbpf.SyscallFunc2(SyscallBigModExpImpl)

// SyscallRemainingComputeUnitsImpl implements sol_remaining_compute_units:
// it consumes the syscall base cost and returns the remaining compute units
// (agave-syscalls SyscallRemainingComputeUnits).
func SyscallRemainingComputeUnitsImpl(vm sbpf.VM) (uint64, error) {
	execCtx := executionCtx(vm)

	err := execCtx.ComputeMeter.Consume(cu.CUSyscallBaseCost)
	if err != nil {
		return syscallCuErr()
	}

	return syscallSuccess(execCtx.ComputeMeter.Remaining())
}

var SyscallRemainingComputeUnits = sbpf.SyscallFunc0(SyscallRemainingComputeUnitsImpl)
