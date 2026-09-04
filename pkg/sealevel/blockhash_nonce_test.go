package sealevel

import (
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	a "github.com/sonicfromnewyoke/mithril/pkg/addresses"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func marshalNonceCurrentState(t *testing.T, durableNonce [32]byte, authority solana.PublicKey) []byte {
	t.Helper()
	nsv := &NonceStateVersions{
		Type: NonceVersionCurrent,
		Current: NonceData{
			IsInitialized: true,
			Authority:     authority,
			DurableNonce:  durableNonce,
			FeeCalculator: FeeCalculator{LamportsPerSignature: 5000},
		},
	}
	data, err := nsv.Marshal()
	require.NoError(t, err)
	return data
}

func makeAdvanceNonceAccountInstr(noncePk, authorityPk solana.PublicKey) Instruction {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, SystemProgramInstrTypeAdvanceNonceAccount)
	return Instruction{
		ProgramId: a.SystemProgramAddr,
		Accounts: []AccountMeta{
			{Pubkey: noncePk, IsSigner: false, IsWritable: true},
			{Pubkey: authorityPk, IsSigner: true, IsWritable: false},
		},
		Data: data,
	}
}

// withEmptyRecentBlockhashesSysvar swaps the global cache for an empty
// queue and returns a deferrable restore func. IsBlockhashAgeValid then
// returns false for any input, forcing the durable-nonce path.
func withEmptyRecentBlockhashesSysvar(t *testing.T) func() {
	t.Helper()
	prev := SysvarCache.RecentBlockHashes.Sysvar
	empty := SysvarRecentBlockhashes{}
	SysvarCache.RecentBlockHashes.Sysvar = &empty
	return func() { SysvarCache.RecentBlockHashes.Sysvar = prev }
}

func publicKeyForTest(kind byte, seed byte) solana.PublicKey {
	var pk solana.PublicKey
	pk[0] = kind
	pk[31] = seed
	return pk
}

func TestIsTransactionAgeValid_NonceInMemAccounts_ReturnsTrue(t *testing.T) {
	defer withEmptyRecentBlockhashesSysvar(t)()

	authority := publicKeyForTest('A', 1)
	noncePk := publicKeyForTest('N', 2)
	durable := [32]byte{0xAA}

	tx := &solana.Transaction{Message: solana.Message{RecentBlockhash: durable}}
	instrs := []Instruction{makeAdvanceNonceAccountInstr(noncePk, authority)}

	mem := accounts.NewMemAccounts()
	require.NoError(t, mem.SetAccountWithoutLock(noncePk, &accounts.Account{
		Key:   noncePk,
		Owner: a.SystemProgramAddr,
		Data:  marshalNonceCurrentState(t, durable, authority),
	}))

	slotCtx := &SlotCtx{Accounts: mem, LastBlockhash: [32]byte{0x77}}
	assert.True(t, IsTransactionAgeValid(tx, instrs, slotCtx))
}

func TestIsTransactionAgeValid_NonceMissing_NilAccountsDb_ReturnsFalse(t *testing.T) {
	defer withEmptyRecentBlockhashesSysvar(t)()

	tx := &solana.Transaction{Message: solana.Message{RecentBlockhash: [32]byte{0xAA}}}
	instrs := []Instruction{makeAdvanceNonceAccountInstr(publicKeyForTest('N', 2), publicKeyForTest('A', 1))}

	slotCtx := &SlotCtx{
		Accounts:      accounts.NewMemAccounts(),
		LastBlockhash: [32]byte{0x77},
	}

	require.NotPanics(t, func() {
		assert.False(t, IsTransactionAgeValid(tx, instrs, slotCtx))
	})
}
