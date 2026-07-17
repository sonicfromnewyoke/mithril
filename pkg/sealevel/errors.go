package sealevel

import (
	"errors"
	"fmt"
)

type InstrErrCustomCode struct {
	Code uint32
}

func (err InstrErrCustomCode) Error() string {
	return fmt.Sprintf("InstrErrCustom(%d)", err.Code)
}

const programErrorBuiltinBitShift = 32

func instrErrFromProgramStatus(status uint64) error {
	const customZero = uint64(1) << programErrorBuiltinBitShift

	switch status {
	case customZero:
		return InstrErrCustomCode{Code: 0}
	case uint64(2) << programErrorBuiltinBitShift:
		return InstrErrInvalidArgument
	case uint64(3) << programErrorBuiltinBitShift:
		return InstrErrInvalidInstructionData
	case uint64(4) << programErrorBuiltinBitShift:
		return InstrErrInvalidAccountData
	case uint64(5) << programErrorBuiltinBitShift:
		return InstrErrAccountDataTooSmall
	case uint64(6) << programErrorBuiltinBitShift:
		return InstrErrInsufficientFunds
	case uint64(7) << programErrorBuiltinBitShift:
		return InstrErrIncorrectProgramId
	case uint64(8) << programErrorBuiltinBitShift:
		return InstrErrMissingRequiredSignature
	case uint64(9) << programErrorBuiltinBitShift:
		return InstrErrAccountAlreadyInitialized
	case uint64(10) << programErrorBuiltinBitShift:
		return InstrErrUninitializedAccount
	case uint64(11) << programErrorBuiltinBitShift:
		return InstrErrNotEnoughAccountKeys
	case uint64(12) << programErrorBuiltinBitShift:
		return InstrErrAccountBorrowFailed
	case uint64(13) << programErrorBuiltinBitShift:
		return InstrErrMaxSeedLengthExceeded
	case uint64(14) << programErrorBuiltinBitShift:
		return InstrErrInvalidSeeds
	case uint64(15) << programErrorBuiltinBitShift:
		return InstrErrBorshIoError
	case uint64(16) << programErrorBuiltinBitShift:
		return InstrErrAccountNotRentExempt
	case uint64(17) << programErrorBuiltinBitShift:
		return InstrErrUnsupportedSysvar
	case uint64(18) << programErrorBuiltinBitShift:
		return InstrErrIllegalOwner
	case uint64(19) << programErrorBuiltinBitShift:
		return InstrErrMaxAccountsDataAllocationsExceeded
	case uint64(20) << programErrorBuiltinBitShift:
		return InstrErrInvalidRealloc
	case uint64(21) << programErrorBuiltinBitShift:
		return InstrErrMaxInstructionTraceLengthExceeded
	case uint64(22) << programErrorBuiltinBitShift:
		return InstrErrBuiltinProgramsMustConsumeComputeUnits
	case uint64(23) << programErrorBuiltinBitShift:
		return InstrErrInvalidAccountOwner
	case uint64(24) << programErrorBuiltinBitShift:
		return InstrErrArithmeticOverflow
	case uint64(25) << programErrorBuiltinBitShift:
		return InstrErrImmutable
	case uint64(26) << programErrorBuiltinBitShift:
		return InstrErrIncorrectAuthority
	default:
		if status>>programErrorBuiltinBitShift == 0 {
			return InstrErrCustomCode{Code: uint32(status)}
		}
		return InstrErrInvalidError
	}
}

// instruction errors
var (
	InstrErrInvalidInstructionData                 = errors.New("InstrErrInvalidInstructionData")
	InstrErrNotEnoughAccountKeys                   = errors.New("InstrErrNotEnoughAccountKeys")
	InstrErrComputationalBudgetExceeded            = errors.New("InstrErrComputationalBudgetExceeded")
	InstrErrMissingAccount                         = errors.New("InstrErrMissingAccount")
	InstrErrInvalidAccountOwner                    = errors.New("InstrErrInvalidAccountOwner")
	InstrErrInvalidAccountData                     = errors.New("InstrErrInvalidAccountData")
	InstrErrMissingRequiredSignature               = errors.New("InstrErrMissingRequiredSignature")
	InstrErrInvalidArgument                        = errors.New("InstrErrInvalidArgument")
	InstrErrExecutableDataModified                 = errors.New("InstrErrExecutableDataModified")
	InstrErrReadonlyDataModified                   = errors.New("InstrErrReadonlyDataModified")
	InstrErrExternalAccountDataModified            = errors.New("InstrErrExternalAccountDataModified")
	InstrErrPrivilegeEscalation                    = errors.New("InstrErrPrivilegeEscalation")
	InstrErrAccountNotExecutable                   = errors.New("InstrErrAccountNotExecutable")
	InstrErrAccountDataSizeChanged                 = errors.New("InstrErrAccountDataSizeChanged")
	InstrErrInvalidRealloc                         = errors.New("InstrErrInvalidRealloc")
	InstrErrModifiedProgramId                      = errors.New("InstrErrModifiedProgramId")
	InstrErrCallDepth                              = errors.New("InstrErrCallDepth")
	InstrErrUnsupportedProgramId                   = errors.New("InstrErrUnsupportedProgramId")
	InstrErrReentrancyNotAllowed                   = errors.New("InstrErrReentrancyNotAllowed")
	InstrErrArithmeticOverflow                     = errors.New("InstrErrArithmeticOverflow")
	InstrErrUnbalancedInstruction                  = errors.New("InstrErrUnbalancedInstruction")
	InstrErrAccountDataTooSmall                    = errors.New("InstrErrAccountDataTooSmall")
	InstrErrAccountBorrowOutstanding               = errors.New("InstrErrAccountBorrowOutstanding")
	InstrErrExternalAccountLamportSpend            = errors.New("InstrErrExternalAccountLamportSpend")
	InstrErrReadonlyLamportChange                  = errors.New("InstrErrReadonlyLamportChange")
	InstrErrExecutableLamportChange                = errors.New("InstrErrExecutableLamportChange")
	InstrErrInsufficientFunds                      = errors.New("InstrErrInsufficientFunds")
	InstrErrAccountAlreadyInitialized              = errors.New("InstrErrAccountAlreadyInitialized")
	InstrErrUninitializedAccount                   = errors.New("InstrErrUninitializedAccount")
	InstrErrIncorrectProgramId                     = errors.New("InstrErrIncorrectProgramId")
	InstrErrImmutable                              = errors.New("InstrErrImmutable")
	InstrErrIncorrectAuthority                     = errors.New("InstrErrIncorrectAuthority")
	InstrErrExecutableAccountNotRentExempt         = errors.New("InstrErrExecutableAccountNotRentExempt")
	InstrErrExecutableModified                     = errors.New("InstrErrExecutableModified")
	InstrErrMaxAccountsExceeded                    = errors.New("InstrErrMaxAccountsExceeded")
	InstrErrAccountBorrowFailed                    = errors.New("InstrErrAccountBorrowFailed")
	InstrErrDuplicateAccountIndex                  = errors.New("InstrErrDuplicateAccountIndex")
	InstrErrRentEpochModified                      = errors.New("InstrErrRentEpochModified")
	InstrErrDuplicateAccountOutOfSync              = errors.New("InstrErrDuplicateAccountOutOfSync")
	InstrErrCustom                                 = errors.New("InstrErrCustom")
	InstrErrInvalidError                           = errors.New("InstrErrInvalidError")
	InstrErrGenericError                           = errors.New("InstrErrGenericError")
	InstrErrMaxSeedLengthExceeded                  = errors.New("InstrErrMaxSeedLengthExceeded")
	InstrErrInvalidSeeds                           = errors.New("InstrErrInvalidSeeds")
	InstrErrProgramEnvironmentSetupFailure         = errors.New("InstrErrProgramEnvironmentSetupFailure")
	InstrErrProgramFailedToComplete                = errors.New("InstrErrProgramFailedToComplete")
	InstrErrProgramFailedToCompile                 = errors.New("InstrErrProgramFailedToCompile")
	InstrErrBorshIoError                           = errors.New("InstrErrBorshIoError")
	InstrErrAccountNotRentExempt                   = errors.New("InstrErrAccountNotRentExempt")
	InstrErrUnsupportedSysvar                      = errors.New("InstrErrUnsupportedSysvar")
	InstrErrIllegalOwner                           = errors.New("InstrErrIllegalOwner")
	InstrErrMaxAccountsDataAllocationsExceeded     = errors.New("InstrErrMaxAccountsDataAllocationsExceeded")
	InstrErrMaxInstructionTraceLengthExceeded      = errors.New("InstrErrMaxInstructionTraceLengthExceeded")
	InstrErrBuiltinProgramsMustConsumeComputeUnits = errors.New("InstrErrBuiltinProgramsMustConsumeComputeUnits")
)

// agaveInstrErrDisplayStrings transcribes the Display impl of Agave's
// InstructionError (solana-instruction-error, impl fmt::Display for
// InstructionError) for every unit variant. Custom is handled separately
// because it carries the program-defined error code.
var agaveInstrErrDisplayStrings = map[error]string{
	InstrErrGenericError:                           "generic instruction error",
	InstrErrInvalidArgument:                        "invalid program argument",
	InstrErrInvalidInstructionData:                 "invalid instruction data",
	InstrErrInvalidAccountData:                     "invalid account data for instruction",
	InstrErrAccountDataTooSmall:                    "account data too small for instruction",
	InstrErrInsufficientFunds:                      "insufficient funds for instruction",
	InstrErrIncorrectProgramId:                     "incorrect program id for instruction",
	InstrErrMissingRequiredSignature:               "missing required signature for instruction",
	InstrErrAccountAlreadyInitialized:              "instruction requires an uninitialized account",
	InstrErrUninitializedAccount:                   "instruction requires an initialized account",
	InstrErrUnbalancedInstruction:                  "sum of account balances before and after instruction do not match",
	InstrErrModifiedProgramId:                      "instruction illegally modified the program id of an account",
	InstrErrExternalAccountLamportSpend:            "instruction spent from the balance of an account it does not own",
	InstrErrExternalAccountDataModified:            "instruction modified data of an account it does not own",
	InstrErrReadonlyLamportChange:                  "instruction changed the balance of a read-only account",
	InstrErrReadonlyDataModified:                   "instruction modified data of a read-only account",
	InstrErrDuplicateAccountIndex:                  "instruction contains duplicate accounts",
	InstrErrExecutableModified:                     "instruction changed executable bit of an account",
	InstrErrRentEpochModified:                      "instruction modified rent epoch of an account",
	InstrErrNotEnoughAccountKeys:                   "insufficient account keys for instruction",
	InstrErrAccountDataSizeChanged:                 "program other than the account's owner changed the size of the account data",
	InstrErrAccountNotExecutable:                   "instruction expected an executable account",
	InstrErrAccountBorrowFailed:                    "instruction tries to borrow reference for an account which is already borrowed",
	InstrErrAccountBorrowOutstanding:               "instruction left account with an outstanding borrowed reference",
	InstrErrDuplicateAccountOutOfSync:              "instruction modifications of multiply-passed account differ",
	InstrErrInvalidError:                           "program returned invalid error code",
	InstrErrExecutableDataModified:                 "instruction changed executable accounts data",
	InstrErrExecutableLamportChange:                "instruction changed the balance of an executable account",
	InstrErrExecutableAccountNotRentExempt:         "executable accounts must be rent exempt",
	InstrErrUnsupportedProgramId:                   "Unsupported program id",
	InstrErrCallDepth:                              "Cross-program invocation call depth too deep",
	InstrErrMissingAccount:                         "An account required by the instruction is missing",
	InstrErrReentrancyNotAllowed:                   "Cross-program invocation reentrancy not allowed for this instruction",
	InstrErrMaxSeedLengthExceeded:                  "Length of the seed is too long for address generation",
	InstrErrInvalidSeeds:                           "Provided seeds do not result in a valid address",
	InstrErrInvalidRealloc:                         "Failed to reallocate account data",
	InstrErrComputationalBudgetExceeded:            "Computational budget exceeded",
	InstrErrPrivilegeEscalation:                    "Cross-program invocation with unauthorized signer or writable account",
	InstrErrProgramEnvironmentSetupFailure:         "Failed to create program execution environment",
	InstrErrProgramFailedToComplete:                "Program failed to complete",
	InstrErrProgramFailedToCompile:                 "Program failed to compile",
	InstrErrImmutable:                              "Account is immutable",
	InstrErrIncorrectAuthority:                     "Incorrect authority provided",
	InstrErrBorshIoError:                           "Failed to serialize or deserialize account data",
	InstrErrAccountNotRentExempt:                   "An account does not have enough lamports to be rent-exempt",
	InstrErrInvalidAccountOwner:                    "Invalid account owner",
	InstrErrArithmeticOverflow:                     "Program arithmetic overflowed",
	InstrErrUnsupportedSysvar:                      "Unsupported sysvar",
	InstrErrIllegalOwner:                           "Provided owner is not allowed",
	InstrErrMaxAccountsDataAllocationsExceeded:     "Accounts data allocations exceeded the maximum allowed per transaction",
	InstrErrMaxAccountsExceeded:                    "Max accounts exceeded",
	InstrErrMaxInstructionTraceLengthExceeded:      "Max instruction trace length exceeded",
	InstrErrBuiltinProgramsMustConsumeComputeUnits: "Builtin programs must consume compute units",
}

// agaveInstrErrDisplay renders err the way Agave's InstructionError Display
// impl would, so stable_log program-failure lines match the Rust runtime
// byte for byte. Program-defined custom errors (system, stake, vote, pubkey
// and precompile sentinels as well as InstrErrCustomCode) render as
// "custom program error: 0x<code-hex>"; anything unmapped falls back to the
// sentinel name.
func agaveInstrErrDisplay(err error) string {
	if err == nil {
		return ""
	}
	var custom InstrErrCustomCode
	if errors.As(err, &custom) {
		return fmt.Sprintf("custom program error: %#x", custom.Code)
	}
	if err == InstrErrCustom {
		// Legacy sentinel without a code: only reachable from paths that
		// never carried the program-defined u32.
		return "custom program error: 0x0"
	}
	if customErrs[err] {
		return fmt.Sprintf("custom program error: %#x", uint32(solanaNumericalErrCodes[err]))
	}
	if s, ok := agaveInstrErrDisplayStrings[err]; ok {
		return s
	}
	// Errors that propagate out of the sbpf interpreter (e.g. an
	// InstructionError returned by a CPI) arrive wrapped in exception chains,
	// so the identity lookups above miss them. Resolve the underlying
	// sentinel through the wrap chain; a chain contains at most one sentinel,
	// so map iteration order does not matter.
	for target, s := range agaveInstrErrDisplayStrings {
		if errors.Is(err, target) {
			return s
		}
	}
	for customErr := range customErrs {
		if errors.Is(err, customErr) {
			return fmt.Sprintf("custom program error: %#x", uint32(solanaNumericalErrCodes[customErr]))
		}
	}
	if errors.Is(err, InstrErrCustom) {
		return "custom program error: 0x0"
	}
	return err.Error()
}

// programRunErrDisplay renders a raw (pre-normalization) VM run error the way
// Agave's stable_log program-failure line would. Ground truth:
// solana-program-runtime-4.0.0 invoke_context.rs process_executable_chain -
// when the failure is an EbpfError::SyscallError whose inner error downcasts
// to an InstructionError, the InstructionError Display is logged; otherwise
// the ORIGINAL error's Display (the inner syscall error, e.g. "SBF program
// panicked", or the EbpfError itself, e.g. "exceeded CUs meter at BPF
// instruction") is logged and ProgramFailedToComplete is returned. Mithril's
// interpreter wraps every fault as *sbpf.Exception -> fmt wrapper ->
// (ExcSyscallError ->) sentinel, so the deepest error in the chain is the
// one whose Display Agave would render.
func programRunErrDisplay(err error) string {
	if err == nil {
		return ""
	}
	deepest := err
	for {
		unwrapped := errors.Unwrap(deepest)
		if unwrapped == nil {
			break
		}
		deepest = unwrapped
	}
	return agaveInstrErrDisplay(deepest)
}

// syscall errors
var (
	// SyscallErrAbort carries Agave's SyscallError::Abort Display text
	// ("SBF program panicked", agave-syscalls-4.0.0) because it is rendered
	// verbatim in the "Program <id> failed: <err>" stable_log line.
	SyscallErrAbort = errors.New("SBF program panicked")

	SyscallErrCopyOverlapping                    = errors.New("SyscallErrCopyOverlapping")
	SyscallErrTooManySlices                      = errors.New("SyscallErrTooManySlices")
	SyscallErrInvalidLength                      = errors.New("SyscallErrInvalidLength")
	SyscallErrInvalidString                      = errors.New("SyscallErrInvalidString")
	SyscallErrMaxSeedLengthExceeded              = errors.New("SyscallErrMaxSeedLengthExceeded")
	SyscallErrReturnDataTooLarge                 = errors.New("SyscallErrReturnDataTooLarge")
	SyscallErrInvalidArgument                    = errors.New("SyscallErrInvalidArgument")
	SyscallErrNotEnoughAccountKeys               = errors.New("SyscallErrNotEnoughAccountKeys")
	SyscallErrTooManySigners                     = errors.New("SyscallErrTooManySigners")
	SyscallErrTooManyBytesConsumed               = errors.New("SyscallErrTooManyBytesConsumed")
	SyscallErrMalformedBool                      = errors.New("SyscallErrMalformedBool")
	SyscallErrProgramNotSupported                = errors.New("SyscallErrProgramNotSupported")
	SyscallErrMaxInstructionDataLenExceeded      = errors.New("SyscallErrMaxInstructionDataLenExceeded")
	SyscallErrMaxInstructionAccountsExceeded     = errors.New("SyscallErrMaxInstructionAccountsExceeded")
	SyscallErrInstructionTooLarge                = errors.New("SyscallErrInstructionTooLarge")
	SyscallErrMaxInstructionAccountInfosExceeded = errors.New("SyscallErrMaxInstructionAccountInfosExceeded")
	SyscallErrTooManyAccounts                    = errors.New("SyscallErrTooManyAccounts")
	SyscallErrInvalidPointer                     = errors.New("SyscallError::InvalidPointer")
)

var (
	PubkeyErrIllegalOwner          = errors.New("PubkeyErrIllegalOwner")
	PubkeyErrInvalidSeeds          = errors.New("PubkeyErrInvalidSeeds")
	PubkeyErrMaxSeedLengthExceeded = errors.New("PubkeyErrMaxSeedLengthExceeded")
)

// precompile errors
var (
	PrecompileErrPublicKey     = errors.New("PrecompileErrPublicKey")
	PrecompileErrRecoveryId    = errors.New("PrecompileErrRecoveryId")
	PrecompileErrSignature     = errors.New("PrecompileErrSignature")
	PrecompileErrDataOffset    = errors.New("PrecompileErrDataOffset")
	PrecompileErrInstrDataSize = errors.New("PrecompileErrInstrDataSize")
)

// instruction errors - Solana numerical error codes
const (
	InstrErrCodeSuccess                                = 0
	InstrErrCodeGenericError                           = 0
	InstrErrCodeInvalidArgument                        = 1
	InstrErrCodeInvalidInstructionData                 = 2
	InstrErrCodeInvalidAccountData                     = 3
	InstrErrCodeAccountDataTooSmall                    = 4
	InstrErrCodeInsufficientFunds                      = 5
	InstrErrCodeIncorrectProgramId                     = 6
	InstrErrCodeMissingRequiredSignature               = 7
	InstrErrCodeAccountAlreadyInitialized              = 8
	InstrErrCodeUninitializedAccount                   = 9
	InstrErrCodeUnbalancedInstruction                  = 10
	InstrErrCodeModifiedProgramId                      = 11
	InstrErrCodeExternalAccountLamportSpend            = 12
	InstrErrCodeExternalAccountDataModified            = 13
	InstrErrCodeReadonlyLamportChange                  = 14
	InstrErrCodeReadonlyDataModified                   = 15
	InstrErrCodeDuplicateAccountIndex                  = 16
	InstrErrCodeExecutableModified                     = 17
	InstrErrCodeRentEpochModified                      = 18
	InstrErrCodeNotEnoughAccountKeys                   = 19
	InstrErrCodeAccountDataSizeChanged                 = 20
	InstrErrCodeAccountNotExecutable                   = 21
	InstrErrCodeAccountBorrowFailed                    = 22
	InstrErrCodeAccountBorrowOutstanding               = 23
	InstrErrCodeDuplicateAccountOutOfSync              = 24
	InstrErrCodeCustom                                 = 25
	InstrErrCodeInvalidError                           = 26
	InstrErrCodeExecutableDataModified                 = 27
	InstrErrCodeExecutableLamportChange                = 28
	InstrErrCodeExecutableAccountNotRentExempt         = 29
	InstrErrCodeUnsupportedProgramId                   = 30
	InstrErrCodeCallDepth                              = 31
	InstrErrCodeMissingAccount                         = 32
	InstrErrCodeReentrancyNotAllowed                   = 33
	InstrErrCodeMaxSeedLengthExceeded                  = 34
	InstrErrCodeInvalidSeeds                           = 35
	InstrErrCodeInvalidRealloc                         = 36
	InstrErrCodeComputationalBudgetExceeded            = 37
	InstrErrCodePrivilegeEscalation                    = 38
	InstrErrCodeProgramEnvironmentSetupFailure         = 39
	InstrErrCodeProgramFailedToComplete                = 40
	InstrErrCodeProgramFailedToCompile                 = 41
	InstrErrCodeImmutable                              = 42
	InstrErrCodeIncorrectAuthority                     = 43
	InstrErrCodeBorshIoError                           = 44
	InstrErrCodeAccountNotRentExempt                   = 45
	InstrErrCodeInvalidAccountOwner                    = 46
	InstrErrCodeArithmeticOverflow                     = 47
	InstrErrCodeUnsupportedSysvar                      = 48
	InstrErrCodeIllegalOwner                           = 49
	InstrErrCodeMaxAccountsDataAllocationsExceeded     = 50
	InstrErrCodeMaxAccountsExceeded                    = 51
	InstrErrCodeMaxInstructionTraceLengthExceeded      = 52
	InstrErrCodeBuiltinProgramsMustConsumeComputeUnits = 53
)

// precompile program errors - Solana numerical error codes
const (
	PrecompileErrCodeInvalidDataOffsets         = 100
	PrecompileErrCodeInvalidInstructionDataSize = 101
	PrecompileErrCodeInvalidSignature           = 102
	PrecompileErrCodeInvalidRecoveryId          = 103 // TODO: not sure this is correct
)

var solanaNumericalErrCodes = map[error]int{
	/* instruction errors */
	InstrErrGenericError:                           0,
	InstrErrInvalidArgument:                        1,
	InstrErrInvalidInstructionData:                 2,
	InstrErrInvalidAccountData:                     3,
	InstrErrAccountDataTooSmall:                    4,
	InstrErrInsufficientFunds:                      5,
	InstrErrIncorrectProgramId:                     6,
	InstrErrMissingRequiredSignature:               7,
	InstrErrAccountAlreadyInitialized:              8,
	InstrErrUninitializedAccount:                   9,
	InstrErrUnbalancedInstruction:                  10,
	InstrErrModifiedProgramId:                      11,
	InstrErrExternalAccountLamportSpend:            12,
	InstrErrExternalAccountDataModified:            13,
	InstrErrReadonlyLamportChange:                  14,
	InstrErrReadonlyDataModified:                   15,
	InstrErrDuplicateAccountIndex:                  16,
	InstrErrExecutableModified:                     17,
	InstrErrRentEpochModified:                      18,
	InstrErrNotEnoughAccountKeys:                   19,
	InstrErrAccountDataSizeChanged:                 20,
	InstrErrAccountNotExecutable:                   21,
	InstrErrAccountBorrowFailed:                    22,
	InstrErrAccountBorrowOutstanding:               23,
	InstrErrDuplicateAccountOutOfSync:              24,
	InstrErrCustom:                                 25,
	InstrErrInvalidError:                           26,
	InstrErrExecutableDataModified:                 27,
	InstrErrExecutableLamportChange:                28,
	InstrErrExecutableAccountNotRentExempt:         29,
	InstrErrUnsupportedProgramId:                   30,
	InstrErrCallDepth:                              31,
	InstrErrMissingAccount:                         32,
	InstrErrReentrancyNotAllowed:                   33,
	InstrErrMaxSeedLengthExceeded:                  34,
	InstrErrInvalidSeeds:                           35,
	InstrErrInvalidRealloc:                         36,
	InstrErrComputationalBudgetExceeded:            37,
	InstrErrPrivilegeEscalation:                    38,
	InstrErrProgramEnvironmentSetupFailure:         39,
	InstrErrProgramFailedToComplete:                40,
	InstrErrProgramFailedToCompile:                 41,
	InstrErrImmutable:                              42,
	InstrErrIncorrectAuthority:                     43,
	InstrErrBorshIoError:                           44,
	InstrErrAccountNotRentExempt:                   45,
	InstrErrInvalidAccountOwner:                    46,
	InstrErrArithmeticOverflow:                     47,
	InstrErrUnsupportedSysvar:                      48,
	InstrErrIllegalOwner:                           49,
	InstrErrMaxAccountsDataAllocationsExceeded:     50,
	InstrErrMaxAccountsExceeded:                    51,
	InstrErrMaxInstructionTraceLengthExceeded:      52,
	InstrErrBuiltinProgramsMustConsumeComputeUnits: 53,

	/* system program errors */
	SystemProgErrAccountAlreadyInUse:        0,
	SystemProgErrResultWithNegativeLamports: 1,
	SystemProgErrInvalidAccountDataLength:   3,
	SystemProgErrAddressWithSeedMismatch:    5,
	SystemProgErrNonceNoRecentBlockhashes:   6,
	SystemProgErrNonceBlockhashNotExpired:   7,

	/* pubkey errors */
	PubkeyErrMaxSeedLengthExceeded: 0,
	PubkeyErrInvalidSeeds:          1,
	PubkeyErrIllegalOwner:          2,

	/* stake program errors */
	StakeErrLockupInForce:                                                  1,
	StakeErrAlreadyDeactivated:                                             2,
	StakeErrTooSoonToRedelegate:                                            3,
	StakeErrInsufficientStake:                                              4,
	StakeErrMergeTransientStake:                                            5,
	StakeErrMergeMismatch:                                                  6,
	StakeErrCustodianMissing:                                               7,
	StakeErrCustodianSignatureMissing:                                      8,
	StakeErrInsufficientReferenceVotes:                                     9,
	StakeErrVoteAddressMismatch:                                            10,
	StakeErrMinimumDelinquentEpochsForDeactivationNotMet:                   11,
	StakeErrInsufficientDelegation:                                         12,
	StakeErrRedelegateTransientOrInactiveStake:                             13,
	StakeErrRedelegateToSameVoteAccount:                                    14,
	StakeErrRedelegatedStakeMustFullyActivateBeforeDeactivationIsPermitted: 15,
	StakeErrEpochRewardsActive:                                             16,

	/* vote program errors */
	VoteErrVoteTooOld:                  0,
	VoteErrSlotsMismatch:               1,
	VoteErrSlotHashMismatch:            2,
	VoteErrEmptySlots:                  3,
	VoteErrTimestampTooOld:             4,
	VoteErrTooSoonToReauthorize:        5,
	VoteErrLockoutConflict:             6,
	VoteErrNewVoteStateLockoutMismatch: 7,
	VoteErrSlotsNotOrdered:             8,
	VoteErrConfirmationsNotOrdered:     9,
	VoteErrZeroConfirmations:           10,
	VoteErrConfirmationTooLarge:        11,
	VoteErrRootRollback:                12,
	VoteErrTooManyVotes:                15,
	VoteErrVotesTooOldAllFiltered:      16,
	VoteErrRootOnDifferentFork:         17,
	VoteErrActiveVoteAccountClose:      18,
	VoteErrCommissionUpdateTooLate:     19,

	/* precompile errors*/
	PrecompileErrPublicKey:     0,
	PrecompileErrRecoveryId:    1,
	PrecompileErrSignature:     2,
	PrecompileErrDataOffset:    3,
	PrecompileErrInstrDataSize: 4,
}

var customErrs = map[error]bool{
	SystemProgErrAccountAlreadyInUse:                                       true,
	SystemProgErrResultWithNegativeLamports:                                true,
	SystemProgErrInvalidAccountDataLength:                                  true,
	SystemProgErrAddressWithSeedMismatch:                                   true,
	SystemProgErrNonceNoRecentBlockhashes:                                  true,
	SystemProgErrNonceBlockhashNotExpired:                                  true,
	PubkeyErrMaxSeedLengthExceeded:                                         true,
	PubkeyErrInvalidSeeds:                                                  true,
	PubkeyErrIllegalOwner:                                                  true,
	StakeErrRedelegateTransientOrInactiveStake:                             true,
	StakeErrRedelegateToSameVoteAccount:                                    true,
	StakeErrInsufficientDelegation:                                         true,
	StakeErrInsufficientReferenceVotes:                                     true,
	StakeErrLockupInForce:                                                  true,
	StakeErrAlreadyDeactivated:                                             true,
	StakeErrTooSoonToRedelegate:                                            true,
	StakeErrInsufficientStake:                                              true,
	StakeErrMergeTransientStake:                                            true,
	StakeErrMergeMismatch:                                                  true,
	StakeErrCustodianMissing:                                               true,
	StakeErrCustodianSignatureMissing:                                      true,
	StakeErrVoteAddressMismatch:                                            true,
	StakeErrMinimumDelinquentEpochsForDeactivationNotMet:                   true,
	StakeErrRedelegatedStakeMustFullyActivateBeforeDeactivationIsPermitted: true,
	StakeErrEpochRewardsActive:                                             true,
	VoteErrVoteTooOld:                                                      true,
	VoteErrSlotsMismatch:                                                   true,
	VoteErrSlotHashMismatch:                                                true,
	VoteErrEmptySlots:                                                      true,
	VoteErrTimestampTooOld:                                                 true,
	VoteErrTooSoonToReauthorize:                                            true,
	VoteErrLockoutConflict:                                                 true,
	VoteErrNewVoteStateLockoutMismatch:                                     true,
	VoteErrSlotsNotOrdered:                                                 true,
	VoteErrConfirmationsNotOrdered:                                         true,
	VoteErrZeroConfirmations:                                               true,
	VoteErrConfirmationTooLarge:                                            true,
	VoteErrTooManyVotes:                                                    true,
	VoteErrCommissionUpdateTooLate:                                         true,
	VoteErrRootOnDifferentFork:                                             true,
	VoteErrVotesTooOldAllFiltered:                                          true,
	VoteErrActiveVoteAccountClose:                                          true,
	VoteErrRootRollback:                                                    true,
	PrecompileErrPublicKey:                                                 true,
	PrecompileErrRecoveryId:                                                true,
	PrecompileErrSignature:                                                 true,
	PrecompileErrDataOffset:                                                true,
	PrecompileErrInstrDataSize:                                             true,
}

func IsCustomErr(err error) bool {
	var custom InstrErrCustomCode
	if errors.As(err, &custom) {
		return true
	}
	return customErrs[err]
}

var instructionErrTargets = []error{
	InstrErrGenericError,
	InstrErrInvalidArgument,
	InstrErrInvalidInstructionData,
	InstrErrInvalidAccountData,
	InstrErrAccountDataTooSmall,
	InstrErrInsufficientFunds,
	InstrErrIncorrectProgramId,
	InstrErrMissingRequiredSignature,
	InstrErrAccountAlreadyInitialized,
	InstrErrUninitializedAccount,
	InstrErrUnbalancedInstruction,
	InstrErrModifiedProgramId,
	InstrErrExternalAccountLamportSpend,
	InstrErrExternalAccountDataModified,
	InstrErrReadonlyLamportChange,
	InstrErrReadonlyDataModified,
	InstrErrDuplicateAccountIndex,
	InstrErrExecutableModified,
	InstrErrRentEpochModified,
	InstrErrNotEnoughAccountKeys,
	InstrErrAccountDataSizeChanged,
	InstrErrAccountNotExecutable,
	InstrErrAccountBorrowFailed,
	InstrErrAccountBorrowOutstanding,
	InstrErrDuplicateAccountOutOfSync,
	InstrErrCustom,
	InstrErrInvalidError,
	InstrErrExecutableDataModified,
	InstrErrExecutableLamportChange,
	InstrErrExecutableAccountNotRentExempt,
	InstrErrUnsupportedProgramId,
	InstrErrCallDepth,
	InstrErrMissingAccount,
	InstrErrReentrancyNotAllowed,
	InstrErrMaxSeedLengthExceeded,
	InstrErrInvalidSeeds,
	InstrErrInvalidRealloc,
	InstrErrComputationalBudgetExceeded,
	InstrErrPrivilegeEscalation,
	InstrErrProgramEnvironmentSetupFailure,
	InstrErrProgramFailedToComplete,
	InstrErrProgramFailedToCompile,
	InstrErrImmutable,
	InstrErrIncorrectAuthority,
	InstrErrBorshIoError,
	InstrErrAccountNotRentExempt,
	InstrErrInvalidAccountOwner,
	InstrErrArithmeticOverflow,
	InstrErrUnsupportedSysvar,
	InstrErrIllegalOwner,
	InstrErrMaxAccountsDataAllocationsExceeded,
	InstrErrMaxAccountsExceeded,
	InstrErrMaxInstructionTraceLengthExceeded,
	InstrErrBuiltinProgramsMustConsumeComputeUnits,
}

func solanaErrCode(err error) (int, bool) {
	if err == nil {
		return InstrErrCodeSuccess, true
	}
	if code, ok := solanaNumericalErrCodes[err]; ok {
		return code, true
	}
	for _, instructionErr := range instructionErrTargets {
		if errors.Is(err, instructionErr) {
			return solanaNumericalErrCodes[instructionErr], true
		}
	}
	return 0, false
}

// TODO: add additional error conversions
func TranslateErrToErrCode(err error) int {
	var custom InstrErrCustomCode
	if errors.As(err, &custom) {
		return int(custom.Code)
	}

	code, _ := solanaErrCode(err)
	return code
}
