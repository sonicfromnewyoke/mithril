package block

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/overcast"
	"github.com/gagliardetto/solana-go"
)

func TestFromLightbringerStreamMsgLeavesBlockHeightUnset(t *testing.T) {
	var entryHash [32]byte
	entryHash[0] = 0xAB

	resp := &overcast.SlotResponse{
		Slot:       123,
		ParentSlot: 122,
		Entries: []*overcast.Entry{
			{
				NumHashes: 1,
				Hash:      entryHash[:],
			},
		},
	}

	block := FromLightbringerStreamMsg(resp)

	if block.BlockHeight != 0 {
		t.Fatalf("expected block height to be unset, got %d", block.BlockHeight)
	}
	if block.SourceParentSlot != 122 {
		t.Fatalf("expected parent slot 122, got %d", block.SourceParentSlot)
	}
	if block.Blockhash != solana.HashFromBytes(entryHash[:]) {
		t.Fatalf("expected blockhash %s, got %s", solana.HashFromBytes(entryHash[:]), block.Blockhash)
	}
}
