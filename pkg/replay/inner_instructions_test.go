package replay

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/stretchr/testify/assert"
)

func TestAssembleInnerInstructions_EmptyReturnsNil(t *testing.T) {
	execCtx := &sealevel.ExecutionCtx{}
	got := AssembleInnerInstructions(execCtx)
	assert.Nil(t, got)
}

func TestAssembleInnerInstructions_GroupsByTopLevelIndex(t *testing.T) {
	execCtx := &sealevel.ExecutionCtx{
		InnerInstrs: []sealevel.RecordedInnerInstr{
			{TopLevelIdx: 0, StackHeight: 2, ProgramIdIndex: 5, Accounts: []uint8{1, 2}, Data: []byte{0xAA}},
			{TopLevelIdx: 0, StackHeight: 3, ProgramIdIndex: 6, Accounts: []uint8{3}, Data: []byte{0xBB}},
			{TopLevelIdx: 2, StackHeight: 2, ProgramIdIndex: 7, Accounts: []uint8{4, 5}, Data: []byte{0xCC}},
		},
	}

	got := AssembleInnerInstructions(execCtx)
	assert.Len(t, got, 2)

	assert.Equal(t, uint8(0), got[0].Index)
	assert.Len(t, got[0].Instructions, 2)
	assert.Equal(t, uint8(5), got[0].Instructions[0].ProgramIdIndex)
	assert.Equal(t, []uint8{1, 2}, got[0].Instructions[0].Accounts)
	assert.Equal(t, []byte{0xAA}, got[0].Instructions[0].Data)
	assert.Equal(t, uint8(2), got[0].Instructions[0].StackHeight)
	assert.Equal(t, uint8(6), got[0].Instructions[1].ProgramIdIndex)
	assert.Equal(t, uint8(3), got[0].Instructions[1].StackHeight)

	assert.Equal(t, uint8(2), got[1].Index)
	assert.Len(t, got[1].Instructions, 1)
	assert.Equal(t, uint8(7), got[1].Instructions[0].ProgramIdIndex)
	assert.Equal(t, uint8(2), got[1].Instructions[0].StackHeight)
}

func TestAssembleInnerInstructions_PreservesInsertionOrder(t *testing.T) {
	execCtx := &sealevel.ExecutionCtx{
		InnerInstrs: []sealevel.RecordedInnerInstr{
			{TopLevelIdx: 3},
			{TopLevelIdx: 1},
			{TopLevelIdx: 3},
			{TopLevelIdx: 0},
		},
	}
	got := AssembleInnerInstructions(execCtx)
	indices := []uint8{got[0].Index, got[1].Index, got[2].Index}
	assert.Equal(t, []uint8{3, 1, 0}, indices,
		"order must reflect first-seen top-level index, not numeric sort")
}

func TestAssembleInnerInstructions_DefensiveCopySlices(t *testing.T) {
	original := sealevel.RecordedInnerInstr{
		TopLevelIdx:    0,
		ProgramIdIndex: 1,
		Accounts:       []uint8{1, 2},
		Data:           []byte{0x01, 0x02},
	}
	execCtx := &sealevel.ExecutionCtx{
		InnerInstrs: []sealevel.RecordedInnerInstr{original},
	}
	got := AssembleInnerInstructions(execCtx)

	// Mutating the assembled output must not corrupt the source.
	got[0].Instructions[0].Accounts[0] = 99
	got[0].Instructions[0].Data[0] = 0xFF
	assert.Equal(t, uint8(1), execCtx.InnerInstrs[0].Accounts[0])
	assert.Equal(t, byte(0x01), execCtx.InnerInstrs[0].Data[0])
}
