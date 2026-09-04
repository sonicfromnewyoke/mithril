package forkchoice

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/epochstakes"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedWaitWhenNoWinnerBeforeTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	service := NewForkChoiceService(0, epochStakes, 100, epochAuth)

	// No blocks ingested yet, no winner, within timeout → NeedWait.
	result := service.IsBankhashCorrect(10, solana.Hash{1})
	assert.Equal(t, BankhashNeedWait, result.Status)
	assert.Equal(t, uint64(100), result.TotalEpochStake)
}

func TestHasSupermajorityAfterEnoughVotes(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	hash := solana.Hash{0xAA}

	// Manually populate state to test query path
	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	for i := 0; i < 67; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		pk[1] = byte((i + 1) >> 8)
		acc.addVote(hash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + VoteConfirmationTimeoutSlots
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(slot, hash)
	assert.Equal(t, BankhashHasSupermajority, result.Status)
	assert.Equal(t, hash, result.WinningHash)
	assert.Equal(t, uint64(67), result.StakeForHash)
	assert.Equal(t, totalStake, result.TotalEpochStake)
	assert.Equal(t, computeThresholdStake(totalStake), result.ThresholdStake)
}

func TestNoSupermajorityAfterLandingWindow(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	hash := solana.Hash{0xAA}

	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	// Only 30 stake — not enough for threshold of 66
	for i := 0; i < 30; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		acc.addVote(hash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + VoteConfirmationTimeoutSlots
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(slot, hash)
	assert.Equal(t, BankhashNoSupermajority, result.Status)
	assert.Equal(t, uint64(30), result.StakeForHash)
	assert.Equal(t, totalStake, result.TotalEpochStake)
	assert.Equal(t, computeThresholdStake(totalStake), result.ThresholdStake)
}

func TestNoVotesSeenAfterLandingWindow(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	// Landing window passed but no accumulator for this slot
	service.state.mu.Lock()
	service.state.latestObservedSlot = 50 + VoteConfirmationTimeoutSlots
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(50, solana.Hash{0xAA})
	assert.Equal(t, BankhashNoSupermajority, result.Status)
}

// TestEarlyConfirmationBeforeTimeout verifies that a slot with observed
// supermajority is confirmed immediately, even before the timeout window.
func TestEarlyConfirmationBeforeTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	hash := solana.Hash{0xAA}

	// Inject supermajority but keep latestObservedSlot BELOW timeout.
	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	for i := 0; i < 67; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		pk[1] = byte((i + 1) >> 8)
		acc.addVote(hash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + 5 // Well below slot + 32
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(slot, hash)
	assert.Equal(t, BankhashHasSupermajority, result.Status, "should confirm immediately when winner exists")
	assert.Equal(t, hash, result.WinningHash)
	assert.Equal(t, uint64(67), result.StakeForHash)
	assert.Equal(t, totalStake, result.TotalEpochStake)
	assert.Equal(t, computeThresholdStake(totalStake), result.ThresholdStake)
}

// TestNeedWaitPartialVotesBeforeTimeout verifies NeedWait when there are
// partial votes (below threshold) and the timeout hasn't expired.
func TestNeedWaitPartialVotesBeforeTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	hash := solana.Hash{0xAA}

	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	// Only 30 stake — not enough for threshold of 66
	for i := 0; i < 30; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		acc.addVote(hash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + 10 // Below timeout
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(slot, hash)
	assert.Equal(t, BankhashNeedWait, result.Status, "no winner + before timeout = NeedWait")
}

// TestNoSupermajorityPartialVotesAfterTimeout verifies NoSupermajority when
// partial votes exist but the timeout has expired without a winner.
func TestNoSupermajorityPartialVotesAfterTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	hash := solana.Hash{0xAA}

	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	for i := 0; i < 30; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		acc.addVote(hash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + VoteConfirmationTimeoutSlots
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(slot, hash)
	assert.Equal(t, BankhashNoSupermajority, result.Status, "no winner + after timeout = NoSupermajority")
	assert.Equal(t, uint64(30), result.StakeForHash)
	assert.Equal(t, computeThresholdStake(totalStake), result.ThresholdStake)
}

// TestEarlyMismatchBeforeTimeout verifies that a bankhash mismatch is surfaced
// immediately when a different hash wins supermajority, even before timeout.
func TestEarlyMismatchBeforeTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	winnerHash := solana.Hash{0xBB}
	ourHash := solana.Hash{0xAA}

	// Inject supermajority for winnerHash, also add some stake for ourHash.
	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	for i := 0; i < 67; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		pk[1] = byte((i + 1) >> 8)
		acc.addVote(winnerHash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	// Add 10 stake for our hash (below threshold).
	for i := 0; i < 10; i++ {
		var pk [32]byte
		pk[0] = byte(i + 100)
		acc.addVote(ourHash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + 5 // Well below timeout
	service.state.mu.Unlock()

	result := service.IsBankhashCorrect(slot, ourHash)
	assert.Equal(t, BankhashNoSupermajority, result.Status, "mismatch surfaced immediately")
	assert.Equal(t, winnerHash, result.WinningHash, "winning hash should be the other hash")
	assert.Equal(t, uint64(10), result.StakeForHash, "our hash stake")
	assert.Equal(t, uint64(67), result.WinningStake, "winner stake")
}

// TestGetSupermajorityHashEarlyConfirmation verifies that GetSupermajorityHash
// returns the winner immediately, before the timeout window.
func TestGetSupermajorityHashEarlyConfirmation(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)
	winnerHash := solana.Hash{0xAA}

	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	for i := 0; i < 67; i++ {
		var pk [32]byte
		pk[0] = byte(i + 1)
		pk[1] = byte((i + 1) >> 8)
		acc.addVote(winnerHash, solana.PublicKeyFromBytes(pk[:]), 1)
	}
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + 5 // Below timeout
	service.state.mu.Unlock()

	hash, status := service.GetSupermajorityHash(slot)
	assert.Equal(t, BankhashHasSupermajority, status, "should return winner immediately")
	assert.Equal(t, winnerHash, hash)
}

// TestGetSupermajorityHashNeedWaitBeforeTimeout verifies that GetSupermajorityHash
// returns NeedWait when no winner exists and the timeout hasn't expired.
func TestGetSupermajorityHashNeedWaitBeforeTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)

	// Partial votes, no winner.
	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	var pk [32]byte
	pk[0] = 1
	acc.addVote(solana.Hash{0xAA}, solana.PublicKeyFromBytes(pk[:]), 30)
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + 10 // Below timeout
	service.state.mu.Unlock()

	hash, status := service.GetSupermajorityHash(slot)
	assert.Equal(t, BankhashNeedWait, status, "no winner + before timeout = NeedWait")
	assert.Equal(t, solana.Hash{}, hash)
}

// TestGetSupermajorityHashNoSupermajorityAfterTimeout verifies that
// GetSupermajorityHash returns NoSupermajority when partial votes exist
// but the timeout has expired without a winner.
func TestGetSupermajorityHashNoSupermajorityAfterTimeout(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochStakes := map[solana.PublicKey]uint64{}
	totalStake := uint64(100)
	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)

	slot := uint64(10)

	// Partial votes, no winner, timeout expired.
	service.state.mu.Lock()
	acc := newSlotVoteAccumulator(totalStake, slot)
	var pk [32]byte
	pk[0] = 1
	acc.addVote(solana.Hash{0xAA}, solana.PublicKeyFromBytes(pk[:]), 30)
	service.state.voteStakeTotals[slot] = acc
	service.state.latestObservedSlot = slot + VoteConfirmationTimeoutSlots
	service.state.mu.Unlock()

	hash, status := service.GetSupermajorityHash(slot)
	assert.Equal(t, BankhashNoSupermajority, status, "no winner + after timeout = NoSupermajority")
	assert.Equal(t, solana.Hash{}, hash)
}

// TestSubmitBlockCapturesEpochDataBeforeUpdateEpoch verifies that a block
// submitted before UpdateEpoch is processed with the epoch data that was
// current at submission time, not the post-update data. This prevents
// pre-boundary blocks queued before an epoch transition from being parsed
// and weighted with post-boundary stakes/voters.
func TestSubmitBlockCapturesEpochDataBeforeUpdateEpoch(t *testing.T) {
	voterKey := solana.PublicKey{1}
	voteAcct := solana.PublicKey{2}

	// Epoch 0: voterKey authorized for voteAcct, stake=50, total=100.
	epoch0Auth := epochstakes.NewEpochAuthorizedVotersCache()
	epoch0Auth.PutEntry(voteAcct, voterKey)
	epoch0Stakes := map[solana.PublicKey]uint64{voteAcct: 50}
	epoch0Total := uint64(100)

	// Epoch 1: voterKey NOT authorized, different total.
	epoch1Auth := epochstakes.NewEpochAuthorizedVotersCache()
	epoch1Stakes := map[solana.PublicKey]uint64{voteAcct: 75}
	epoch1Total := uint64(200)

	service := NewForkChoiceService(0, epoch0Stakes, epoch0Total, epoch0Auth)

	// Build a vote tx: voterKey votes for slot 50 with hash {0xBB}.
	votedSlot := uint64(50)
	votedHash := solana.Hash{0xBB}
	voteTx := buildTestVoteTx(voteAcct, voterKey, votedSlot, votedHash)

	// Submit the block — captures epoch 0 data in the job.
	// Service is NOT started, so the job sits in the channel.
	service.SubmitBlock(100, []*solana.Transaction{voteTx})

	// Update to epoch 1 BEFORE the job is processed.
	service.UpdateEpoch(1, epoch1Stakes, epoch1Total, epoch1Auth)

	// Drain the job and verify it carries epoch 0 data.
	job := <-service.jobChan
	assert.Equal(t, epoch0Total, job.totalEpochStake, "job should carry epoch 0 total stake")

	// Process the job — should use epoch 0 data from the job.
	service.processBlock(job)

	// The vote should have been accepted (voterKey authorized in epoch 0),
	// and the accumulator should use epoch 0's total stake for threshold.
	service.state.mu.Lock()
	acc, exists := service.state.voteStakeTotals[votedSlot]
	service.state.mu.Unlock()

	assert.True(t, exists, "accumulator should exist — vote authorized in epoch 0")
	assert.Equal(t, epoch0Total, acc.totalEpochStake, "accumulator threshold should use epoch 0 total")
	assert.Equal(t, computeThresholdStake(epoch0Total), acc.thresholdStake)
	assert.Equal(t, uint64(50), acc.stakeForHash(votedHash), "vote weighted with epoch 0 stake")

	// Verify the converse: if the job had used epoch 1 data, the vote would
	// have been rejected (voterKey not in epoch1Auth) and no accumulator created.
	// The existence of the accumulator with epoch 0 weights proves the snapshot.
}

// TestSequentialProcessingDeterminesWinner verifies that block processing order
// determines winner selection when two competing hashes can both independently
// cross the 2/3 threshold. This is a regression test for the ordering fix that
// replaced the concurrent ants pool with sequential inline processing.
//
// The test exercises the full service path: Start() → SubmitBlock() → run() →
// processBlock() → Stop(). Channel FIFO ordering guarantees block 100 is
// processed before block 101, so hashX always crosses the threshold first.
//
// With the old ants pool, thread scheduling determined which block's votes
// applied first, making the winner non-deterministic.
func TestSequentialProcessingDeterminesWinner(t *testing.T) {
	// Two voters, each with enough stake to independently cross the threshold.
	voterA := solana.PublicKey{1}
	voteAcctA := solana.PublicKey{2}
	voterB := solana.PublicKey{3}
	voteAcctB := solana.PublicKey{4}

	totalStake := uint64(100) // threshold = uint64(100 * 2/3) = 66

	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochAuth.PutEntry(voteAcctA, voterA)
	epochAuth.PutEntry(voteAcctB, voterB)

	epochStakes := map[solana.PublicKey]uint64{
		voteAcctA: 67, // > threshold of 66
		voteAcctB: 67,
	}

	service := NewForkChoiceService(0, epochStakes, totalStake, epochAuth)
	service.Start()

	// Both voters vote for the same slot but different hashes.
	votedSlot := uint64(50)
	hashX := solana.Hash{0xAA}
	hashY := solana.Hash{0xBB}

	// Block at slot 100: voterA votes for (slot 50, hashX) — submitted first.
	txA := buildTestVoteTx(voteAcctA, voterA, votedSlot, hashX)
	service.SubmitBlock(100, []*solana.Transaction{txA})

	// Block at slot 101: voterB votes for (slot 50, hashY) — submitted second.
	txB := buildTestVoteTx(voteAcctB, voterB, votedSlot, hashY)
	service.SubmitBlock(101, []*solana.Transaction{txB})

	// Stop drains all queued jobs before returning.
	service.Stop()

	// latestObservedSlot should have been advanced by processBlock (not manual).
	service.state.mu.Lock()
	latestIngested := service.state.latestObservedSlot
	service.state.mu.Unlock()
	assert.Equal(t, uint64(101), latestIngested, "watermark should advance to 101")

	// hashX must win because block 100 was processed first (channel FIFO).
	resultX := service.IsBankhashCorrect(votedSlot, hashX)
	assert.Equal(t, BankhashHasSupermajority, resultX.Status, "hashX should have supermajority")
	assert.Equal(t, hashX, resultX.WinningHash)

	// hashY loses — a different hash already won supermajority.
	resultY := service.IsBankhashCorrect(votedSlot, hashY)
	assert.Equal(t, BankhashNoSupermajority, resultY.Status, "hashY should be NoSupermajority (mismatch)")
	assert.Equal(t, hashX, resultY.WinningHash, "winning hash should still be hashX")
}

func TestParseAndValidateVoteTxFindsVoteInstructionAfterComputeBudget(t *testing.T) {
	voterKey := solana.PublicKey{1}
	voteAcct := solana.PublicKey{2}
	votedSlot := uint64(50)
	votedHash := solana.Hash{0xBB}

	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochAuth.PutEntry(voteAcct, voterKey)

	tx := buildTestVoteTx(voteAcct, voterKey, votedSlot, votedHash)
	voteInstr := tx.Message.Instructions[0]
	tx.Message.AccountKeys = []solana.PublicKey{
		voterKey,
		voteAcct,
		solana.SysVarSlotHashesPubkey,
		solana.SysVarClockPubkey,
		solana.ComputeBudget,
		solana.VoteProgramID,
	}
	tx.Message.Instructions = []solana.CompiledInstruction{
		{
			ProgramIDIndex: 4,
			Data:           solana.Base58([]byte{2, 0, 0, 0, 200, 0, 0, 0, 0}),
		},
		{
			ProgramIDIndex: 5,
			Accounts:       voteInstr.Accounts,
			Data:           voteInstr.Data,
		},
	}

	require.True(t, tx.IsVote(), "fixture should still be recognized as a vote transaction")

	info, ok := parseAndValidateVoteTx(tx, epochAuth)
	require.True(t, ok)
	assert.Equal(t, votedSlot, info.slot)
	assert.Equal(t, votedHash, solana.Hash(info.bankHash))
	assert.Equal(t, voteAcct, info.votePubkey)
}

func TestParseAndValidateVoteTxUsesSignerForVoteAuthority(t *testing.T) {
	voterKey := solana.PublicKey{1}
	voteAcct := solana.PublicKey{2}
	votedSlot := uint64(50)
	votedHash := solana.Hash{0xBB}

	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochAuth.PutEntry(voteAcct, voterKey)

	tx := buildTestVoteTx(voteAcct, voterKey, votedSlot, votedHash)
	require.True(t, tx.IsVote(), "fixture should be recognized as a vote transaction")
	require.Equal(t, solana.SysVarSlotHashesPubkey, tx.Message.AccountKeys[tx.Message.Instructions[0].Accounts[1]],
		"legacy vote account 1 is the slot-hashes sysvar, not the vote authority")

	info, ok := parseAndValidateVoteTx(tx, epochAuth)
	require.True(t, ok)
	assert.Equal(t, votedSlot, info.slot)
	assert.Equal(t, votedHash, solana.Hash(info.bankHash))
	assert.Equal(t, voteAcct, info.votePubkey)
}

func TestParseAndValidateVoteTxRejectsAuthorityOutsideInstruction(t *testing.T) {
	voterKey := solana.PublicKey{1}
	voteAcct := solana.PublicKey{2}

	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochAuth.PutEntry(voteAcct, voterKey)

	tx := buildTestVoteTx(voteAcct, voterKey, 50, solana.Hash{0xBB})
	tx.Message.Instructions[0].Accounts = []uint16{1, 2, 3}

	_, ok := parseAndValidateVoteTx(tx, epochAuth)
	require.False(t, ok)
}

func TestParseAndValidateVoteTxAcceptsLiveTowerSyncShape(t *testing.T) {
	voteAuthority := solana.MustPublicKeyFromBase58("DRpbCBMxVnDK7maPM5tGv6MvB3v1sRMC86PZ8okm21hy")
	voteAcct := solana.MustPublicKeyFromBase58("3N7s9zXMZ4QqvHQR15t5GNHyqc89KduzMP7423eWiD5g")
	votedHash := solana.MustHashFromBase58("F4GcS4MtttPknSkbGW3KCXWJd6mWvzaXDnHHyM87Gd2A")

	var data solana.Base58
	err := json.Unmarshal([]byte(`"67MGmzm8yEnRh15X2h4HuP1ZCWg1Ld1zPNgZqhBGEySYPXuCReZ8tSvvhrKA1j7q6ky81hjNVPUp6WvdLbnfVYTmFvK2C2QCSBbGAoibsseTrrczvs6Xk47BPdpcN6PB9bYaFnu8wtykuo4WLhELbCuYYwwUyA6zNqZfDHLePABFKUDJLbyE9DsqoiATDtoznG7Bevvfra"`), &data)
	require.NoError(t, err)

	tx := &solana.Transaction{
		Message: solana.Message{
			Header: solana.MessageHeader{
				NumRequiredSignatures: 1,
			},
			AccountKeys: []solana.PublicKey{
				voteAuthority,
				voteAcct,
				solana.VoteProgramID,
			},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 2,
					Accounts:       []uint16{1, 0},
					Data:           data,
				},
			},
		},
		Signatures: []solana.Signature{{}},
	}

	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochAuth.PutEntry(voteAcct, voteAuthority)

	info, ok := parseAndValidateVoteTx(tx, epochAuth)
	require.True(t, ok)
	assert.Equal(t, uint64(420404777), info.slot)
	assert.Equal(t, votedHash, solana.Hash(info.bankHash))
	assert.Equal(t, voteAcct, info.votePubkey)
}

func TestParseAndValidateVoteTxFallsBackToSignatureCountWhenHeaderSignerCountMissing(t *testing.T) {
	voteAuthority := solana.MustPublicKeyFromBase58("GmCxjmjKZoaKN1DKunbYq8RCYib94Nm3sHyncFfofaF5")
	voteAcct := solana.MustPublicKeyFromBase58("7S9dHgoeMYvtShTjEC3x5D3THRDQz123WVGPseZsm3hm")

	var data solana.Base58
	err := json.Unmarshal([]byte(`"67MGn8HzmNzWfjLAq5WGPoC4LktJMeSH2UUTqWmWd2VXRbURnRQM4hTJvxGRcSbKb6CYLd3x42wvAjAyYsY19ajzUtqxDcE4XZP4eHV47zTUEkudvy7R2a7sJAaJtS9nk9D2NtMP3du8S8BFSUhjLPmVW9pmh4CgnBS5Jh7B8XNkQmGLS8sCGSWY9UbZrYipFy7rEVjirv"`), &data)
	require.NoError(t, err)

	tx := &solana.Transaction{
		Message: solana.Message{
			Header: solana.MessageHeader{},
			AccountKeys: []solana.PublicKey{
				voteAuthority,
				voteAcct,
				solana.VoteProgramID,
			},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 2,
					Accounts:       []uint16{1, 0},
					Data:           data,
				},
			},
		},
		Signatures: []solana.Signature{{}},
	}

	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	epochAuth.PutEntry(voteAcct, voteAuthority)

	info, ok := parseAndValidateVoteTx(tx, epochAuth)
	require.True(t, ok)
	assert.Equal(t, uint64(420407984), info.slot)
	assert.Equal(t, voteAcct, info.votePubkey)
}

func TestObserveBlockResolvesParentSlotFromParentBlockhash(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	service := NewForkChoiceService(0, map[solana.PublicKey]uint64{}, 100, epochAuth)

	anchorHash := solana.Hash{0x01}
	childHash := solana.Hash{0x02}

	service.ObserveExecutionAnchor(99, anchorHash)

	err := service.ObserveBlock(ObservedBlockMeta{
		Slot:            101,
		Blockhash:       childHash,
		ParentBlockhash: anchorHash,
	}, nil)
	require.NoError(t, err)

	service.state.mu.Lock()
	defer service.state.mu.Unlock()

	meta := service.state.observedBlocks[101]
	require.NotNil(t, meta)
	assert.True(t, meta.ParentSlotKnown)
	assert.Equal(t, uint64(99), meta.ParentSlot)
}

func TestObserveExecutionAnchorPrunesOldForkchoiceState(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	service := NewForkChoiceService(0, map[solana.PublicKey]uint64{}, 100, epochAuth)

	oldParentHash := hash(0x10)
	newParentHash := hash(0x20)
	oldBlockHash := hash(0x11)
	newBlockHash := hash(0x21)
	anchorHash := hash(0x30)

	oldAcc := newSlotVoteAccumulator(100, 90)
	oldAcc.trackers[hash(0x91)] = &voteStakeTracker{voted: map[solana.PublicKey]struct{}{}, stake: 70}
	newAcc := newSlotVoteAccumulator(100, 101)
	newAcc.trackers[hash(0xA1)] = &voteStakeTracker{voted: map[solana.PublicKey]struct{}{}, stake: 70}

	service.state.mu.Lock()
	service.state.voteStakeTotals[90] = oldAcc
	service.state.voteStakeTotals[101] = newAcc
	service.state.observedBlocks[90] = &ObservedBlockMeta{Slot: 90, Blockhash: oldBlockHash, ParentSlot: 89, ParentSlotKnown: true}
	service.state.observedBlocks[101] = &ObservedBlockMeta{Slot: 101, Blockhash: newBlockHash, ParentSlot: 100, ParentSlotKnown: true}
	service.state.blockhashToSlot[oldParentHash] = 89
	service.state.blockhashToSlot[oldBlockHash] = 90
	service.state.blockhashToSlot[newParentHash] = 100
	service.state.blockhashToSlot[newBlockHash] = 101
	service.state.pendingParentByHash[oldParentHash] = []uint64{90, 95}
	service.state.pendingParentByHash[newParentHash] = []uint64{101, 103}
	service.state.equivocatedSlots[90] = struct{}{}
	service.state.equivocatedSlots[101] = struct{}{}
	service.state.mu.Unlock()

	service.ObserveExecutionAnchor(100, anchorHash)

	service.state.mu.Lock()
	defer service.state.mu.Unlock()

	_, ok := service.state.voteStakeTotals[90]
	assert.False(t, ok)
	_, ok = service.state.voteStakeTotals[101]
	assert.True(t, ok)

	_, ok = service.state.observedBlocks[90]
	assert.False(t, ok)
	_, ok = service.state.observedBlocks[101]
	assert.True(t, ok)

	_, ok = service.state.blockhashToSlot[oldParentHash]
	assert.False(t, ok)
	_, ok = service.state.blockhashToSlot[oldBlockHash]
	assert.False(t, ok)
	_, ok = service.state.blockhashToSlot[newParentHash]
	assert.True(t, ok)
	_, ok = service.state.blockhashToSlot[newBlockHash]
	assert.True(t, ok)
	assert.Equal(t, uint64(100), service.state.blockhashToSlot[anchorHash])

	waitingOld := service.state.pendingParentByHash[oldParentHash]
	assert.Empty(t, waitingOld)
	waitingNew := service.state.pendingParentByHash[newParentHash]
	assert.Equal(t, []uint64{101, 103}, waitingNew)

	_, ok = service.state.equivocatedSlots[90]
	assert.False(t, ok)
	_, ok = service.state.equivocatedSlots[101]
	assert.True(t, ok)
}

func TestFindConfirmedLeafReturnsHighestObservedWinner(t *testing.T) {
	epochAuth := epochstakes.NewEpochAuthorizedVotersCache()
	service := NewForkChoiceService(0, map[solana.PublicKey]uint64{}, 100, epochAuth)

	injectWinner := func(slot uint64, winningHash solana.Hash) {
		acc := newSlotVoteAccumulator(100, slot)
		tracker := &voteStakeTracker{
			voted: make(map[solana.PublicKey]struct{}),
			stake: 70,
		}
		acc.trackers[winningHash] = tracker
		acc.confirmed = true
		acc.confirmedHash = winningHash
		service.state.voteStakeTotals[slot] = acc
	}

	service.state.mu.Lock()
	service.state.observedBlocks[105] = &ObservedBlockMeta{Slot: 105, ParentSlot: 100, ParentSlotKnown: true, Blockhash: hash(0x05)}
	service.state.observedBlocks[107] = &ObservedBlockMeta{Slot: 107, ParentSlot: 105, ParentSlotKnown: true, Blockhash: hash(0x07)}
	injectWinner(105, hash(0xA5))
	injectWinner(107, hash(0xA7))
	service.state.latestObservedSlot = 107
	service.state.mu.Unlock()

	leaf, err := service.FindConfirmedLeaf(100, 16)
	require.NoError(t, err)
	assert.Equal(t, uint64(107), leaf.Slot)
	assert.Equal(t, hash(0xA7), leaf.Bankhash)
}

// buildTestVoteTx constructs a minimal legacy Vote instruction for testing.
func buildTestVoteTx(voteAcct, voteAuthority solana.PublicKey, slot uint64, hash solana.Hash) *solana.Transaction {
	// Encode VoteProgramInstrTypeVote (type=2):
	//   [type:4][num_slots:8][slot:8][hash:32][timestamp_opt:1] = 53 bytes
	data := make([]byte, 53)
	binary.LittleEndian.PutUint32(data[0:4], 2)  // VoteProgramInstrTypeVote
	binary.LittleEndian.PutUint64(data[4:12], 1) // 1 slot (Rust Vec len is u64)
	binary.LittleEndian.PutUint64(data[12:20], slot)
	copy(data[20:52], hash[:])
	data[52] = 0 // No timestamp

	return &solana.Transaction{
		Message: solana.Message{
			Header: solana.MessageHeader{
				NumRequiredSignatures: 1,
			},
			AccountKeys: []solana.PublicKey{
				voteAuthority,
				voteAcct,
				solana.SysVarSlotHashesPubkey,
				solana.SysVarClockPubkey,
				solana.VoteProgramID,
			},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 4,
					Accounts:       []uint16{1, 2, 3, 0},
					Data:           solana.Base58(data),
				},
			},
		},
		Signatures: []solana.Signature{{}},
	}
}
