package sbpf

import (
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf/sbpfver"
	"github.com/stretchr/testify/require"
)

func testSlot(op uint8, dst uint8, src uint8, off int16, imm uint32) Slot {
	return Slot(op) | Slot(dst)<<8 | Slot(src)<<12 | Slot(uint16(off))<<16 | Slot(imm)<<32
}

func testSlotsToBytes(slots []Slot) []byte {
	out := make([]byte, len(slots)*SlotSize)
	for i, slot := range slots {
		binary.LittleEndian.PutUint64(out[i*SlotSize:], uint64(slot))
	}
	return out
}

func testV3Program(text []Slot, funcs map[uint32]int64) *Program {
	return &Program{
		TextBytes:   testSlotsToBytes(text),
		Text:        text,
		TextVA:      VaddrProgram,
		Entrypoint:  0,
		Funcs:       funcs,
		SbpfVersion: sbpfver.SbpfVersion{Version: sbpfver.SbpfVersionV3},
	}
}

func TestInterpreterV3StaticSyscallAndReturn(t *testing.T) {
	hash := SymbolHash("test_syscall")
	text := []Slot{
		testSlot(OpCall, 0, 0, 0, hash),
		testSlot(OpExit, 0, 0, 0, 0),
	}
	program := testV3Program(text, nil)
	require.NoError(t, program.Verify())

	called := false
	syscalls := SyscallRegistry(func(got uint32) (Syscall, bool) {
		if got != hash {
			return nil, false
		}
		return SyscallFunc0(func(VM) (uint64, error) {
			called = true
			return 7, nil
		}), true
	})
	computeMeter := cu.NewComputeMeter(100)
	interpreter := NewInterpreter(program, &VMOpts{
		HeapMax:      1024,
		Syscalls:     syscalls,
		ComputeMeter: &computeMeter,
	})
	defer interpreter.Finish()

	ret, _, err := interpreter.Run()
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, uint64(7), ret)
}

func TestInterpreterV3RelativeCall(t *testing.T) {
	text := []Slot{
		testSlot(OpCall, 0, 1, 0, 1),
		testSlot(OpExit, 0, 0, 0, 0),
		testSlot(OpMov64Imm, 0, 0, 0, 42),
		testSlot(OpExit, 0, 0, 0, 0),
	}
	program := testV3Program(text, nil)
	require.NoError(t, program.Verify())

	computeMeter := cu.NewComputeMeter(100)
	interpreter := NewInterpreter(program, &VMOpts{
		HeapMax:      1024,
		Syscalls:     func(uint32) (Syscall, bool) { return nil, false },
		ComputeMeter: &computeMeter,
	})
	defer interpreter.Finish()

	ret, _, err := interpreter.Run()
	require.NoError(t, err)
	require.Equal(t, uint64(42), ret)
}

func TestInterpreterV3RodataRegionZero(t *testing.T) {
	text := []Slot{
		testSlot(OpExit, 0, 0, 0, 0),
	}
	program := testV3Program(text, nil)
	program.RO = []byte{1, 2, 3, 4}
	computeMeter := cu.NewComputeMeter(100)
	interpreter := NewInterpreter(program, &VMOpts{
		HeapMax:      1024,
		Syscalls:     func(uint32) (Syscall, bool) { return nil, false },
		ComputeMeter: &computeMeter,
	})
	defer interpreter.Finish()

	bytes, err := interpreter.Translate(0, uint64(len(program.RO)), false)
	require.NoError(t, err)
	require.Equal(t, program.RO, bytes)

	_, err = interpreter.translateInternal(VaddrProgram, SlotSize, false)
	require.Error(t, err)
}
