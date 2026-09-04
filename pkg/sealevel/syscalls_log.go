package sealevel

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	//"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/safemath"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/gagliardetto/solana-go"
)

func SyscallLogImpl(vm sbpf.VM, ptr, strlen uint64) (uint64, error) {
	//mlog.Log.Debugf("SyscallLog")

	execCtx := executionCtx(vm)

	cost := max(cu.CUSyscallBaseCost, strlen)
	err := execCtx.ComputeMeter.Consume(cost)
	if err != nil {
		return syscallCuErr()
	}

	buf := make([]byte, strlen)
	if err = vm.Read(ptr, buf); err != nil {
		return syscallErr(err)
	}
	if !utf8.Valid(buf) {
		return syscallErr(SyscallErrInvalidString)
	}

	execCtx.Log.Log("Program log: " + string(buf))

	return syscallSuccess(0)
}

var SyscallLog = sbpf.SyscallFunc2(SyscallLogImpl)

func SyscallLog64Impl(vm sbpf.VM, r1, r2, r3, r4, r5 uint64) (uint64, error) {
	//mlog.Log.Debugf("SyscallLog64")

	execCtx := executionCtx(vm)
	err := execCtx.ComputeMeter.Consume(cu.CULog64Units)
	if err != nil {
		return syscallCuErr()
	}

	// Agave: stable_log::program_log(&log_collector,
	// &format!("{arg1:#x}, {arg2:#x}, {arg3:#x}, {arg4:#x}, {arg5:#x}"))
	msg := fmt.Sprintf("Program log: %#x, %#x, %#x, %#x, %#x", r1, r2, r3, r4, r5)
	execCtx.Log.Log(msg)
	return syscallSuccess(0)
}

var SyscallLog64 = sbpf.SyscallFunc5(SyscallLog64Impl)

func SyscallLogCUsImpl(vm sbpf.VM) (uint64, error) {
	//mlog.Log.Debugf("SyscallLogCUs")

	execCtx := executionCtx(vm)
	err := execCtx.ComputeMeter.Consume(cu.CUSyscallBaseCost)
	if err != nil {
		return syscallCuErr()
	}

	msg := fmt.Sprintf("Program consumption: %d units remaining", execCtx.ComputeMeter.Remaining())
	execCtx.Log.Log(msg)
	return syscallSuccess(0)
}

var SyscallLogCUs = sbpf.SyscallFunc0(SyscallLogCUsImpl)

func SyscallLogPubkeyImpl(vm sbpf.VM, pubkeyAddr uint64) (uint64, error) {
	//mlog.Log.Debugf("SyscallLogPubkey")

	execCtx := executionCtx(vm)
	err := execCtx.ComputeMeter.Consume(cu.CULogPubkeyUnits)
	if err != nil {
		return syscallCuErr()
	}

	var pubkey solana.PublicKey
	if err = vm.Read(pubkeyAddr, pubkey[:]); err != nil {
		return syscallErr(err)
	}

	execCtx.Log.Log("Program log: " + pubkey.String())
	return syscallSuccess(0)
}

var SyscallLogPubkey = sbpf.SyscallFunc1(SyscallLogPubkeyImpl)

func SyscallLogDataImpl(vm sbpf.VM, addr uint64, len uint64) (uint64, error) {
	//mlog.Log.Debugf("SyscallLogData")

	execCtx := executionCtx(vm)
	err := execCtx.ComputeMeter.Consume(cu.CUSyscallBaseCost)
	if err != nil {
		return syscallCuErr()
	}

	size, err := safemath.CheckedMulU64(len, 16)
	if err != nil {
		return syscallErr(err)
	}

	mem, err := vm.Translate(addr, size, false)
	if err != nil {
		return syscallErr(err)
	}

	err = execCtx.ComputeMeter.Consume(safemath.SaturatingMulU64(len, cu.CUSyscallBaseCost))
	if err != nil {
		return syscallCuErr()
	}

	fields := make([]string, 0, len)

	var data []byte
	reader := bytes.NewReader(mem)

	for count := uint64(0); count < len; count++ {
		var vec VectorDescrC
		err = vec.Unmarshal(reader)
		if err != nil {
			return syscallErr(err)
		}

		err = execCtx.ComputeMeter.Consume(vec.Len)
		if err != nil {
			return syscallCuErr()
		}

		data, err = vm.Translate(vec.Addr, vec.Len, false)
		if err != nil {
			return syscallErr(err)
		}
		fields = append(fields, base64.StdEncoding.EncodeToString(data))
	}

	// Agave: stable_log::program_data - "Program data: <base64 fields
	// joined by single spaces>".
	execCtx.Log.Log("Program data: " + strings.Join(fields, " "))

	return syscallSuccess(0)
}

var SyscallLogData = sbpf.SyscallFunc2(SyscallLogDataImpl)
