package sealevel

import (
	"fmt"
	"testing"

	"github.com/Overclock-Validator/mithril/fixtures"
	"github.com/Overclock-Validator/mithril/pkg/accounts"
	a "github.com/Overclock-Validator/mithril/pkg/addresses"
	"github.com/Overclock-Validator/mithril/pkg/cu"
	"github.com/Overclock-Validator/mithril/pkg/features"
	"github.com/Overclock-Validator/mithril/pkg/sbpf"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

// newBpfProgramExecCtx wires up an execution context around the vendored
// spl_memo-1.0.0.so ELF (mirrored from litesvm-go's mithrilsvm/elf vendor
// set) deployed as a loader-v2 owned program, with an explicit compute meter
// and ComputeBudgetLimits so tests can drive CU / heap-cost exhaustion.
func newBpfProgramExecCtx(t *testing.T, computeUnits uint64, budgetLimits *ComputeBudgetLimits) (*ExecutionCtx, *LogRecorder, solana.PublicKey) {
	t.Helper()

	programBytes := fixtures.Load(t, "sbpf", "spl_memo-1.0.0.so")

	programPrivKey, err := solana.NewRandomPrivateKey()
	require.NoError(t, err)
	programKey := programPrivKey.PublicKey()
	programAcct := accounts.Account{Key: programKey, Lamports: 1, Data: programBytes, Owner: a.BpfLoader2Addr, Executable: true, RentEpoch: 100}

	transactionAccts := NewTransactionAccounts([]accounts.Account{programAcct})

	log := new(LogRecorder)
	execCtx := &ExecutionCtx{Log: log, ComputeMeter: cu.NewComputeMeter(computeUnits), Accounts: accounts.NewMemAccounts()}
	execCtx.Features = *features.NewFeaturesDefault()
	execCtx.SlotCtx = &SlotCtx{}

	txCtx := NewTransactionCtx(*transactionAccts, 5, 64)
	txCtx.ComputeBudgetLimits = budgetLimits
	execCtx.TransactionContext = txCtx

	return execCtx, log, programKey
}

// TestBpfFraming_VMComputeExhaustion: a BPF program run under a tiny compute
// budget trips the VM-level CU meter. Agave logs the ORIGINAL EbpfError
// Display ("exceeded CUs meter at BPF instruction",
// EbpfError::ExceededMaxInstructions in solana-sbpf) in the failed framing
// line while the returned error is InstructionError::ProgramFailedToComplete
// (solana-program-runtime-4.0.0 invoke_context.rs process_executable_chain).
func TestBpfFraming_VMComputeExhaustion(t *testing.T) {
	const budget = 150
	execCtx, log, programKey := newBpfProgramExecCtx(t, budget,
		&ComputeBudgetLimits{ComputeUnitLimit: budget, UpdatedHeapBytes: 32 * 1024})

	err := execCtx.ProcessInstruction([]byte("hello memo"),
		InstructionAcctsFromAccountMetas(nil, execCtx.TransactionContext.Accounts), []uint64{0})
	require.ErrorIs(t, err, InstrErrProgramFailedToComplete)

	require.Equal(t, []string{
		fmt.Sprintf("Program %s invoke [1]", programKey),
		fmt.Sprintf("Program %s consumed %d of %d compute units", programKey, budget, budget),
		fmt.Sprintf("Program %s failed: exceeded CUs meter at BPF instruction", programKey),
	}, log.Logs)
}

// TestBpfFraming_HeapCostExhaustion: requesting a 256KiB heap costs 56 CU
// (calculateHeapCost); with fewer remaining CUs the VM cannot be created.
// Agave logs "Failed to create SBF VM: Computational budget exceeded" and
// returns InstructionError::ProgramEnvironmentSetupFailure, which Displays
// as "Failed to create program execution environment" in the failed framing
// line (solana-program-runtime-4.0.0 src/vm.rs execute()/create_vm!).
func TestBpfFraming_HeapCostExhaustion(t *testing.T) {
	const budget = 50 // below the 56 CU heap cost for a 256KiB heap
	execCtx, log, programKey := newBpfProgramExecCtx(t, budget,
		&ComputeBudgetLimits{ComputeUnitLimit: budget, UpdatedHeapBytes: 256 * 1024})

	err := execCtx.ProcessInstruction([]byte("hello memo"),
		InstructionAcctsFromAccountMetas(nil, execCtx.TransactionContext.Accounts), []uint64{0})
	require.ErrorIs(t, err, InstrErrProgramEnvironmentSetupFailure)

	require.Equal(t, []string{
		fmt.Sprintf("Program %s invoke [1]", programKey),
		"Failed to create SBF VM: Computational budget exceeded",
		fmt.Sprintf("Program %s failed: Failed to create program execution environment", programKey),
	}, log.Logs)
}

// TestProgramRunErrDisplay_AgaveTexts pins the raw-run-error Display strings
// the failed framing line renders, matching the Agave sources they were
// verified against (agave-syscalls-4.0.0 SyscallError, solana-sbpf-0.14.4
// EbpfError).
func TestProgramRunErrDisplay_AgaveTexts(t *testing.T) {
	wrap := func(inner error) error {
		return &sbpf.Exception{PC: 5, Detail: fmt.Errorf("tx: sig, programId: pid - %w:", inner)}
	}

	// SyscallError::Abort
	require.Equal(t, "SBF program panicked",
		programRunErrDisplay(wrap(sbpf.ExcSyscallError{Err: SyscallErrAbort})))

	// SyscallError::Panic
	require.Equal(t, "SBF program Panicked in src/lib.rs at 12:34",
		programRunErrDisplay(wrap(sbpf.ExcSyscallError{Err: fmt.Errorf("SBF program Panicked in %s at %d:%d", "src/lib.rs", 12, 34)})))

	// EbpfError::ExceededMaxInstructions, wrapped and bare
	require.Equal(t, "exceeded CUs meter at BPF instruction", programRunErrDisplay(wrap(sbpf.ExcOutOfCU)))
	require.Equal(t, "exceeded CUs meter at BPF instruction", programRunErrDisplay(sbpf.ExcOutOfCU))

	// EbpfError::AccessViolation with the section derived from the vaddr
	require.Equal(t, "Access violation in input section at address 0x400000010 of size 8",
		programRunErrDisplay(wrap(sbpf.NewExcBadAccess(0x4_0000_0010, 8, false, "read oob"))))
	require.Equal(t, "Access violation in stack section at address 0x200001000 of size 4",
		programRunErrDisplay(wrap(sbpf.NewExcBadAccess(0x2_0000_1000, 4, true, "write oob"))))

	// Unmapped errors keep their sentinel text as a fallback.
	require.Equal(t, "unknown symbol or syscall 0x000004d2",
		programRunErrDisplay(wrap(sbpf.ExcCallDest{Imm: 1234})))
}

// TestAgaveInstrErrDisplay_WrappedChains: InstructionErrors and custom
// program errors that propagate out of the interpreter arrive wrapped in
// sbpf exception chains; the framing must still render the InstructionError
// Display (Agave downcasts the syscall error to InstructionError before
// logging).
func TestAgaveInstrErrDisplay_WrappedChains(t *testing.T) {
	wrap := func(inner error) error {
		return &sbpf.Exception{PC: 7, Detail: fmt.Errorf("tx: sig, programId: pid - %w:", sbpf.ExcSyscallError{Err: inner})}
	}

	require.Equal(t, "missing required signature for instruction",
		agaveInstrErrDisplay(wrap(InstrErrMissingRequiredSignature)))
	require.Equal(t, "custom program error: 0x1",
		agaveInstrErrDisplay(wrap(SystemProgErrResultWithNegativeLamports)))
	require.Equal(t, "custom program error: 0x2a",
		agaveInstrErrDisplay(wrap(InstrErrCustomCode{Code: 42})))
}
