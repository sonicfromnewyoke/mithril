package sbpf

import (
	"errors"
	"fmt"

	"github.com/Overclock-Validator/mithril/pkg/cu"
	"github.com/gagliardetto/solana-go"
)

// VM is the virtual machine abstraction, implemented by each executor.
type VM interface {
	VMContext() any

	HeapMax() uint64
	HeapSize() uint64
	UpdateHeapSize(size uint64)

	Translate(addr uint64, size uint64, write bool) ([]byte, error)

	DueInstrCount() uint64
	PrevInstrMeter() uint64
	SetPrevInstrMeter(num uint64)
	ComputeMeter() *cu.ComputeMeter

	Read(addr uint64, p []byte) error
	Read8(addr uint64) (uint8, error)
	Read16(addr uint64) (uint16, error)
	Read32(addr uint64) (uint32, error)
	Read64(addr uint64) (uint64, error)

	Write(addr uint64, p []byte) error
	Write8(addr uint64, x uint8) error
	Write16(addr uint64, x uint16) error
	Write32(addr uint64, x uint32) error
	Write64(addr uint64, x uint64) error
}

// VMOpts specifies virtual machine parameters.
type VMOpts struct {
	// Machine parameters
	HeapMax       int
	Syscalls      SyscallRegistry
	Tracer        TraceSink
	EnableTracing bool

	// Execution parameters
	Context        any // passed to syscalls
	MaxCU          int
	ComputeMeter   *cu.ComputeMeter
	Input          []byte // mapped at VaddrInput
	InputRegions   []InputRegion
	InputDataVaddr uint64 // VM address of instruction data within Input (SIMD-0321)
	// DisableStackFrameGaps is used by SIMD-0460 virtual address space adjustments.
	DisableStackFrameGaps bool

	// Debug
	ProgramId   solana.PublicKey
	TxSignature solana.Signature
}

type InputRegion struct {
	Offset               uint64
	HostOffset           uint64
	RegionSize           uint64
	AddressSpaceReserved uint64
	Writable             bool
	AccountIndex         int
	Data                 []byte
	OnWrite              func(region *InputRegion, requestedLen uint64) error
}

type Exception struct {
	PC     int64
	Detail error
}

func (e *Exception) Error() string {
	return fmt.Sprintf("exception at %d: %s", e.PC, e.Detail)
}

func (e *Exception) Unwrap() error {
	return e.Detail
}

// Exception codes. The sentinel texts mirror the Display impl of the
// corresponding solana-sbpf EbpfError variants (verified against
// solana-sbpf-0.14.4 src/error.rs), because Agave's stable_log renders the
// raw EbpfError Display in "Program <id> failed: <err>" lines when the
// failure does not map to an InstructionError.
var (
	ExcDivideByZero   = errors.New("divide by zero at BPF instruction")         // EbpfError::DivideByZero
	ExcDivideOverflow = errors.New("division overflow at BPF instruction")      // EbpfError::DivideOverflow
	ExcOutOfCU        = errors.New("exceeded CUs meter at BPF instruction")     // EbpfError::ExceededMaxInstructions
	ExcCallDepth      = errors.New("exceeded max BPF to BPF call depth")        // EbpfError::CallDepthExceeded
	ExcInvalidInstr   = errors.New("invalid instruction - feature not enabled") // (mithril-specific diagnostic)

	ExcUnsupportedInstruction = errors.New("unsupported BPF instruction")                                              // EbpfError::UnsupportedInstruction
	ExcExecutionOverrun       = errors.New("attempted to execute past the end of the text segment at BPF instruction") // EbpfError::ExecutionOverrun
)

type ExcBadAccess struct {
	Addr   uint64
	Size   uint64
	Write  bool
	Reason string
}

func NewExcBadAccess(addr uint64, size uint64, write bool, reason string) ExcBadAccess {
	return ExcBadAccess{
		Addr:   addr,
		Size:   size,
		Write:  write,
		Reason: reason,
	}
}

// accessViolationSection derives the section name solana-sbpf reports in
// EbpfError::AccessViolation from the faulting virtual address: the region is
// selected by the address' top 32 bits (memory_region.rs
// generate_access_violation, solana-sbpf-0.14.4).
func accessViolationSection(addr uint64) string {
	switch addr & ^(VaddrProgram - 1) {
	case VaddrProgram:
		return "program"
	case VaddrStack:
		return "stack"
	case VaddrHeap:
		return "heap"
	case VaddrInput:
		return "input"
	default:
		return "unknown"
	}
}

// Error mirrors the Display of solana-sbpf's general
// EbpfError::AccessViolation ("Access violation in {section} section at
// address {addr:#x} of size {size}"), which Agave's stable_log renders in
// "Program <id> failed: <err>" lines. The stack-frame specific
// StackAccessViolation variant is not reproduced; the general form is used
// for all addresses. Reason is kept for diagnostics but not displayed.
func (e ExcBadAccess) Error() string {
	return fmt.Sprintf("Access violation in %s section at address %#x of size %d", accessViolationSection(e.Addr), e.Addr, e.Size)
}

type ExcCallDest struct {
	Imm uint32
}

func (e ExcCallDest) Error() string {
	return fmt.Sprintf("unknown symbol or syscall 0x%08x", e.Imm)
}

type ExcSyscallError struct {
	Err error
}

func (e ExcSyscallError) Error() string {
	return fmt.Sprintf("syscall error: %s", e.Err)
}

func (e ExcSyscallError) Unwrap() error {
	return e.Err
}
