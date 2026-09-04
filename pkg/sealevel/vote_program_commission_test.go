package sealevel

import (
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	a "github.com/sonicfromnewyoke/mithril/pkg/addresses"
	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func voteCommissionTestPubkey(seed byte) solana.PublicKey {
	var raw [32]byte
	raw[0] = seed
	return solana.PublicKeyFromBytes(raw[:])
}

func voteCommissionTestAccountData(t *testing.T, withdrawer solana.PublicKey, commission byte, f *features.Features) []byte {
	t.Helper()

	voteState := &VoteStateVersions{
		Type: VoteStateVersionCurrent,
		Current: VoteState{
			AuthorizedWithdrawer: withdrawer,
			Commission:           commission,
		},
	}

	stateBytes, err := marshalVersionedVoteState(voteState)
	require.NoError(t, err)

	data := make([]byte, sizeOfVersionedVoteState(*f))
	copy(data, stateBytes)
	return data
}

func executeVoteUpdateCommission(
	t *testing.T,
	f *features.Features,
	slot uint64,
	currentCommission byte,
	newCommission byte,
) (error, *accounts.Account) {
	t.Helper()

	oldSysvarCache := SysvarCache
	SysvarCache.Clock.Sysvar = &SysvarClock{Slot: slot}
	SysvarCache.EpochSchedule.Sysvar = &SysvarEpochSchedule{
		SlotsPerEpoch:            32,
		LeaderScheduleSlotOffset: 32,
		Warmup:                   false,
		FirstNormalEpoch:         0,
		FirstNormalSlot:          0,
	}
	defer func() {
		SysvarCache = oldSysvarCache
	}()

	withdrawer := voteCommissionTestPubkey(42)
	votePubkey := voteCommissionTestPubkey(43)

	programAcct := accounts.Account{
		Key:        a.VoteProgramAddr,
		Lamports:   1,
		Data:       make([]byte, 0),
		Owner:      a.NativeLoaderAddr,
		Executable: true,
		RentEpoch:  100,
	}
	voteAcct := accounts.Account{
		Key:       votePubkey,
		Lamports:  1,
		Data:      voteCommissionTestAccountData(t, withdrawer, currentCommission, f),
		Owner:     a.VoteProgramAddr,
		RentEpoch: 100,
	}
	withdrawerAcct := accounts.Account{
		Key:       withdrawer,
		Lamports:  1,
		Data:      make([]byte, 0),
		Owner:     a.SystemProgramAddr,
		RentEpoch: 100,
	}

	transactionAccts := NewTransactionAccounts([]accounts.Account{programAcct, voteAcct, withdrawerAcct})
	acctMetas := []AccountMeta{
		{Pubkey: voteAcct.Key, IsWritable: true},
		{Pubkey: withdrawerAcct.Key, IsSigner: true},
	}
	instructionAccts := InstructionAcctsFromAccountMetas(acctMetas, *transactionAccts)
	txCtx := NewTransactionCtx(*transactionAccts, 5, 64)

	execCtx := ExecutionCtx{
		TransactionContext: txCtx,
		ComputeMeter:       cu.NewComputeMeterDefault(),
		Features:           *f,
		ModifiedVoteStates: make(map[solana.PublicKey]*VoteStateVersions),
	}

	instrData := make([]byte, 0, 5)
	instrData = binary.LittleEndian.AppendUint32(instrData, VoteProgramInstrTypeUpdateCommission)
	instrData = append(instrData, newCommission)

	err := execCtx.ProcessInstruction(instrData, instructionAccts, []uint64{0})
	updatedVoteAcct, getErr := txCtx.Accounts.GetAccount(1)
	require.NoError(t, getErr)

	return err, updatedVoteAcct
}

func TestVoteProgramUpdateCommission_LateIncreaseRejectedBeforeDelayFeature(t *testing.T) {
	ft := features.NewFeaturesDefault()

	err, updatedVoteAcct := executeVoteUpdateCommission(t, ft, 31, 10, 20)
	require.ErrorIs(t, err, VoteErrCommissionUpdateTooLate)

	updatedVoteState, unmarshalErr := UnmarshalVersionedVoteState(updatedVoteAcct.Data)
	require.NoError(t, unmarshalErr)
	assert.Equal(t, byte(10), updatedVoteState.ConvertToCurrent().Commission)
}

func TestVoteProgramUpdateCommission_LateDecreaseAllowedBeforeDelayFeature(t *testing.T) {
	ft := features.NewFeaturesDefault()

	err, updatedVoteAcct := executeVoteUpdateCommission(t, ft, 31, 20, 10)
	require.NoError(t, err)

	updatedVoteState, unmarshalErr := UnmarshalVersionedVoteState(updatedVoteAcct.Data)
	require.NoError(t, unmarshalErr)
	assert.Equal(t, byte(10), updatedVoteState.ConvertToCurrent().Commission)
}

func TestVoteProgramUpdateCommission_LateIncreaseAllowedWithDelayFeature(t *testing.T) {
	ft := features.NewFeaturesDefault()
	ft.EnableFeature(features.DelayCommissionUpdates, 0)

	err, updatedVoteAcct := executeVoteUpdateCommission(t, ft, 31, 10, 20)
	require.NoError(t, err)

	updatedVoteState, unmarshalErr := UnmarshalVersionedVoteState(updatedVoteAcct.Data)
	require.NoError(t, unmarshalErr)
	assert.Equal(t, byte(20), updatedVoteState.ConvertToCurrent().Commission)
}
