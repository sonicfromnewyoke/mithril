package forkchoice

import (
	"github.com/sonicfromnewyoke/mithril/pkg/epochstakes"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gammazero/deque"
)

type voteInfo struct {
	slot       uint64
	bankHash   [32]byte
	votePubkey solana.PublicKey
}

// parseAndValidateVoteTx validates a vote transaction against the given authorized
// voters cache. Accepts the cache as a parameter to avoid racing with epoch updates.
func parseAndValidateVoteTx(tx *solana.Transaction, authorizedVoters *epochstakes.EpochAuthorizedVotersCache) (*voteInfo, bool) {
	if len(tx.Message.Instructions) == 0 {
		return nil, false
	}

	for _, instr := range tx.Message.Instructions {
		programID, err := tx.ResolveProgramIDIndex(instr.ProgramIDIndex)
		if err != nil || !programID.Equals(solana.VoteProgramID) {
			continue
		}
		return parseAndValidateVoteInstruction(tx, instr, authorizedVoters)
	}

	return nil, false
}

func parseAndValidateVoteInstruction(tx *solana.Transaction, instr solana.CompiledInstruction, authorizedVoters *epochstakes.EpochAuthorizedVotersCache) (info *voteInfo, ok bool) {
	defer func() {
		if recover() != nil {
			info = nil
			ok = false
		}
	}()

	if len(instr.Accounts) < 1 {
		return nil, false
	}
	votePubkey, err := tx.Message.Account(instr.Accounts[0])
	if err != nil {
		return nil, false
	}

	if !hasAuthorizedVoteSigner(tx, instr, votePubkey, authorizedVoters) {
		return nil, false
	}

	instrData := instr.Data
	decoder := bin.NewBinDecoder(instrData)
	instructionType, err := decoder.ReadUint32(bin.LE)
	if err != nil {
		return nil, false
	}

	switch instructionType {
	case sealevel.VoteProgramInstrTypeTowerSync:
		{
			var vote sealevel.VoteInstrTowerSync
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}
			lockout, ok := getLastLockout(&vote.Lockouts)
			if !ok {
				return nil, false
			}

			return &voteInfo{slot: lockout.Slot,
				bankHash:   vote.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeTowerSyncSwitch:
		{
			var vote sealevel.VoteInstrTowerSyncSwitch
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}
			lockout, ok := getLastLockout(&vote.TowerSync.Lockouts)
			if !ok {
				return nil, false
			}
			return &voteInfo{slot: lockout.Slot,
				bankHash:   vote.TowerSync.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeVote:
		{
			if !hasLegacyVoteSysvarAccounts(tx, instr) {
				return nil, false
			}
			var vote sealevel.VoteInstrVote
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}

			slot, ok := getSlot(vote.Slots)
			if !ok {
				return nil, false
			}

			return &voteInfo{slot: slot,
				bankHash:   vote.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeVoteSwitch:
		{
			if !hasLegacyVoteSysvarAccounts(tx, instr) {
				return nil, false
			}
			var vote sealevel.VoteInstrVoteSwitch
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}

			slot, ok := getSlot(vote.Vote.Slots)
			if !ok {
				return nil, false
			}

			return &voteInfo{slot: slot,
				bankHash:   vote.Vote.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeUpdateVoteState:
		{
			var vote sealevel.VoteInstrUpdateVoteState
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}

			lockout, ok := getLastLockout(&vote.Lockouts)
			if !ok {
				return nil, false
			}
			return &voteInfo{slot: lockout.Slot,
				bankHash:   vote.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeUpdateVoteStateSwitch:
		{
			var vote sealevel.VoteInstrUpdateVoteStateSwitch
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}

			lockout, ok := getLastLockout(&vote.UpdateVoteState.Lockouts)
			if !ok {
				return nil, false
			}
			return &voteInfo{slot: lockout.Slot,
				bankHash:   vote.UpdateVoteState.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeCompactUpdateVoteState:
		{
			var vote sealevel.VoteInstrCompactUpdateVoteState
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}

			lockout, ok := getLastLockout(&vote.UpdateVoteState.Lockouts)
			if !ok {
				return nil, false
			}
			return &voteInfo{slot: lockout.Slot,
				bankHash:   vote.UpdateVoteState.Hash,
				votePubkey: votePubkey}, true
		}

	case sealevel.VoteProgramInstrTypeCompactUpdateVoteStateSwitch:
		{
			var vote sealevel.VoteInstrCompactUpdateVoteStateSwitch
			err = vote.UnmarshalWithDecoder(decoder)
			if err != nil {
				return nil, false
			}

			lockout, ok := getLastLockout(&vote.UpdateVoteState.Lockouts)
			if !ok {
				return nil, false
			}
			return &voteInfo{slot: lockout.Slot,
				bankHash:   vote.Hash,
				votePubkey: votePubkey}, true
		}

	default:
		{
			return nil, false
		}
	}
}

func hasAuthorizedVoteSigner(tx *solana.Transaction, instr solana.CompiledInstruction, votePubkey solana.PublicKey, authorizedVoters *epochstakes.EpochAuthorizedVotersCache) bool {
	if authorizedVoters == nil {
		return false
	}

	// The Vote program validates authority from the instruction's signer set;
	// for common vote instructions, account 1 is a sysvar rather than the voter.
	numSigners := voteTransactionSignerCount(tx)
	if numSigners > len(tx.Message.AccountKeys) {
		numSigners = len(tx.Message.AccountKeys)
	}
	for _, accountIndex := range instr.Accounts {
		if int(accountIndex) >= numSigners {
			continue
		}
		if authorizedVoters.IsAuthorizedVoter(votePubkey, tx.Message.AccountKeys[accountIndex]) {
			return true
		}
	}
	return false
}

func voteTransactionSignerCount(tx *solana.Transaction) int {
	numSigners := int(tx.Message.Header.NumRequiredSignatures)
	if numSigners == 0 && len(tx.Signatures) > 0 {
		numSigners = len(tx.Signatures)
	}
	return numSigners
}

func hasLegacyVoteSysvarAccounts(tx *solana.Transaction, instr solana.CompiledInstruction) bool {
	if len(instr.Accounts) < 3 {
		return false
	}
	slotHashes, err := tx.Message.Account(instr.Accounts[1])
	if err != nil || slotHashes != solana.SysVarSlotHashesPubkey {
		return false
	}
	clock, err := tx.Message.Account(instr.Accounts[2])
	return err == nil && clock == solana.SysVarClockPubkey
}

func getLastLockout(lockouts *deque.Deque[sealevel.VoteLockout]) (*sealevel.VoteLockout, bool) {
	lockoutsLen := lockouts.Len()
	if lockoutsLen == 0 {
		return nil, false
	}

	lockout := lockouts.PopBack()
	return &lockout, true
}

func getSlot(slots []uint64) (uint64, bool) {
	if len(slots) == 0 {
		return 0, false
	}
	maxSlot := slots[0]
	for _, slot := range slots {
		if slot > maxSlot {
			maxSlot = slot
		}
	}
	return maxSlot, true
}
