package replay

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

const (
	fuzzMaxAccountKeys  = 16
	fuzzMaxSignatures   = 16
	fuzzMaxInstructions = 8
)

// FuzzLoadAndExecuteTransaction_NeverPanics asserts that LoadAndExecuteTransaction
// surfaces every reachable failure as a TransactionError, never as a panic.
//
// Run extended fuzz with:
//
//	go test ./pkg/replay/ -fuzz FuzzLoadAndExecuteTransaction_NeverPanics -fuzztime 60s
func FuzzLoadAndExecuteTransaction_NeverPanics(f *testing.F) {
	for _, s := range [][]byte{
		{},
		{0},
		{0, 0, 0, 0},
		{1, 1, 1, 0},
		{2, 1, 1, 1, 99, 0, 0},
		{255, 255, 255, 255, 0},
	} {
		f.Add(s)
	}

	feats := features.NewFeaturesDefault()
	feats.EnableFeature(features.FormalizeLoadedTransactionDataSize, 0)

	// Use an empty queue so the fuzzer exercises sanitize/load logic
	// without depending on production sysvar initialization.
	emptyRBH := sealevel.SysvarRecentBlockhashes{}
	prev := sealevel.SysvarCache.RecentBlockHashes.Sysvar
	sealevel.SysvarCache.RecentBlockHashes.Sysvar = &emptyRBH
	defer func() { sealevel.SysvarCache.RecentBlockHashes.Sysvar = prev }()

	f.Fuzz(func(t *testing.T, payload []byte) {
		slotCtx := &sealevel.SlotCtx{
			Features:        feats,
			Accounts:        accounts.NewMemAccounts(),
			FeeRateGovernor: &sealevel.FeeRateGovernor{},
		}

		require.NotPanics(t, func() {
			_ = LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
				SlotCtx:      slotCtx,
				Transaction:  buildFuzzedTx(payload),
				IsSimulation: true,
			})
		})
	})
}

func buildFuzzedTx(payload []byte) *solana.Transaction {
	tx := &solana.Transaction{}
	if len(payload) == 0 {
		return tx
	}

	at := func(i int) byte {
		if i < len(payload) {
			return payload[i]
		}
		return 0
	}

	tx.Message.Header.NumRequiredSignatures = at(0)
	tx.Message.Header.NumReadonlySignedAccounts = at(1)
	tx.Message.Header.NumReadonlyUnsignedAccounts = at(2)

	numKeys := int(at(3) % fuzzMaxAccountKeys)
	numSigs := int(at(4) % fuzzMaxSignatures)
	numInstrs := int(at(5) % fuzzMaxInstructions)

	tx.Message.AccountKeys = make([]solana.PublicKey, numKeys)
	for i := range tx.Message.AccountKeys {
		var pk solana.PublicKey
		pk[0] = byte(i)
		pk[1] = at(6 + i)
		tx.Message.AccountKeys[i] = pk
	}

	tx.Signatures = make([]solana.Signature, numSigs)

	tx.Message.Instructions = make([]solana.CompiledInstruction, numInstrs)
	for i := range tx.Message.Instructions {
		var programIdx, accountIdx uint8
		// Allow indices to land out of range so the loader's bounds check fires.
		if numKeys > 0 {
			programIdx = at(22+i*3) % byte(numKeys+2)
			accountIdx = at(23+i*3) % byte(numKeys+2)
		} else {
			programIdx = at(22+i*3) % 4
		}
		tx.Message.Instructions[i] = solana.CompiledInstruction{
			ProgramIDIndex: uint16(programIdx),
			Accounts:       []uint16{uint16(accountIdx)},
			Data:           nil,
		}
	}

	return tx
}
