package sealevel

import (
	"errors"
	"math"

	a "github.com/sonicfromnewyoke/mithril/pkg/addresses"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
)

func isNonOverlapping(src, srcLen, dst, dstLen uint64) bool {
	if srcLen == 0 || dstLen == 0 {
		return true
	}

	if src > dst {
		return src-dst >= dstLen
	} else {
		return dst-src >= srcLen
	}
}

func syscallErrCustom(msg string) (uint64, error) {
	return math.MaxUint64, errors.New(msg)
}

func syscallCheckAligned(execCtx *ExecutionCtx) bool {
	txCtx := execCtx.TransactionContext
	if txCtx == nil {
		return true
	}
	instrCtx, err := txCtx.CurrentInstructionCtx()
	if err != nil {
		return true
	}
	programAcct, err := instrCtx.BorrowLastProgramAccount(txCtx)
	if err != nil {
		return true
	}
	defer programAcct.Drop()

	return programAcct.Owner() != a.BpfLoaderDeprecatedAddr
}

func syscallAddressIsAligned(execCtx *ExecutionCtx, addr uint64, alignment uint64) bool {
	if !syscallCheckAligned(execCtx) {
		return false
	}
	if alignment <= 1 {
		return true
	}
	return addr%alignment == 0
}

func syscallAddressRequiresAlignment(execCtx *ExecutionCtx, addr uint64, alignment uint64) bool {
	return syscallCheckAligned(execCtx) && alignment > 1 && addr%alignment != 0
}

func syscallParameterAddressRestricted(execCtx *ExecutionCtx, addr uint64) bool {
	return execCtx.Features.IsActive(features.SyscallParameterAddressRestrictions) && addr >= sbpf.VaddrInput
}

func syscallErr(err error) (uint64, error) {
	return math.MaxUint64, err
}

func syscallCuErr() (uint64, error) {
	return math.MaxUint64, InstrErrComputationalBudgetExceeded
}

func syscallSuccess(result uint64) (uint64, error) {
	return result, nil
}
