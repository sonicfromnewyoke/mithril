package sealevel

import (
	"fmt"

	"github.com/Overclock-Validator/mithril/pkg/sbpf"
)

func SyscallAbortImpl(_ sbpf.VM) (uint64, error) {
	return syscallErr(SyscallErrAbort)
}

var SyscallAbort = sbpf.SyscallFunc0(SyscallAbortImpl)

// SyscallPanicImpl is the implementation for the panic (sol_panic_) syscall.
// The Labs client implementation does CU accounting, checks for NULL
// termination, validates the utf8 string, etc, but we don't actually
// need to do this because this syscall returns an error and aborts the
// transaction either way. The error TEXT does matter for log parity: it is
// rendered in the "Program <id> failed: <err>" stable_log line, so it mirrors
// Agave's SyscallError::Panic Display ("SBF program Panicked in {file} at
// {line}:{column}", agave-syscalls-4.0.0).
func SyscallPanicImpl(vm sbpf.VM, fileNameAddr, len, line, column uint64) (uint64, error) {
	filenameData, err := vm.Translate(fileNameAddr, len, false)
	if err != nil {
		return syscallErr(err)
	}

	err = fmt.Errorf("SBF program Panicked in %s at %d:%d", string(filenameData), line, column)
	return syscallErr(err)
}

var SyscallPanic = sbpf.SyscallFunc4(SyscallPanicImpl)

//go:generate go run github.com/Overclock-Validator/mithril/pkg/sealevel/syscalls_gen syscalls.go
