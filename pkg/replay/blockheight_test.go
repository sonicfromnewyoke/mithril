package replay

import (
	"testing"

	b "github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/global"
)

func TestAssignBlockHeightUsesNextProducedHeight(t *testing.T) {
	prevBlockHeight := global.BlockHeight()
	t.Cleanup(func() {
		global.SetBlockHeight(prevBlockHeight)
	})

	global.SetBlockHeight(41)

	block := &b.Block{}
	setBlockHeight(block)

	if block.BlockHeight != 42 {
		t.Fatalf("expected block height 42, got %d", block.BlockHeight)
	}
}

func TestAssignBlockHeightOverridesSourceHeight(t *testing.T) {
	prevBlockHeight := global.BlockHeight()
	t.Cleanup(func() {
		global.SetBlockHeight(prevBlockHeight)
	})

	global.SetBlockHeight(41)

	block := &b.Block{BlockHeight: 99}
	setBlockHeight(block)

	if block.BlockHeight != 42 {
		t.Fatalf("expected managed block height 42, got %d", block.BlockHeight)
	}
}
