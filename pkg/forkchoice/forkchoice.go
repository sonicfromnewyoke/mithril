package forkchoice

import (
	"fmt"
	"sort"
	"sync"

	"github.com/sonicfromnewyoke/mithril/pkg/base58"
	"github.com/sonicfromnewyoke/mithril/pkg/epochstakes"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/gagliardetto/solana-go"
)

// BankhashStatus represents the confirmation status of a slot's bankhash.
type BankhashStatus int

const (
	BankhashHasSupermajority BankhashStatus = iota
	BankhashNoSupermajority
	BankhashNeedWait
)

func (s BankhashStatus) String() string {
	switch s {
	case BankhashHasSupermajority:
		return "has_supermajority"
	case BankhashNoSupermajority:
		return "no_supermajority"
	case BankhashNeedWait:
		return "need_wait"
	default:
		return "unknown"
	}
}

// BankhashResult provides detailed fork choice query results.
type BankhashResult struct {
	Status          BankhashStatus
	WinningHash     solana.Hash
	StakeForHash    uint64 // stake accumulated for the queried hash
	WinningStake    uint64 // stake accumulated for the winning hash (may differ from StakeForHash)
	TotalEpochStake uint64
	ThresholdStake  uint64
}

// VoteHashDiagnostic is a JSON-friendly snapshot of votes accumulated for one
// bankhash within a target slot.
type VoteHashDiagnostic struct {
	Bankhash   string `json:"bankhash"`
	Stake      uint64 `json:"stake"`
	VoterCount int    `json:"voter_count"`
	Confirmed  bool   `json:"confirmed"`
}

// SlotVoteDiagnostic is a JSON-friendly snapshot of forkchoice's accumulated
// vote state for a target slot.
type SlotVoteDiagnostic struct {
	Slot               uint64               `json:"slot"`
	Status             string               `json:"status"`
	WinningHash        string               `json:"winning_hash,omitempty"`
	LatestObservedSlot uint64               `json:"latest_observed_slot"`
	TotalEpochStake    uint64               `json:"total_epoch_stake"`
	ThresholdStake     uint64               `json:"threshold_stake"`
	Hashes             []VoteHashDiagnostic `json:"hashes,omitempty"`
}

// ConfirmedLeaf is a vote-confirmed bankhash winner paired with the observed
// block slot it belongs to.
type ConfirmedLeaf struct {
	Slot     uint64
	Bankhash solana.Hash
}

type blockJob struct {
	slot uint64
	txs  []*solana.Transaction

	// Epoch data captured at submission time so that each block is processed
	// with the epoch view that was current when it was submitted, not when
	// it happens to be dequeued. This prevents post-boundary epoch data from
	// being applied to pre-boundary blocks sitting in the queue.
	epochStakes           map[solana.PublicKey]uint64
	epochAuthorizedVoters *epochstakes.EpochAuthorizedVotersCache
	totalEpochStake       uint64
}

type voteUpdate struct {
	voteInfo *voteInfo
	stake    uint64
}

type forkChoiceState struct {
	voteStakeTotals       map[uint64]*slotVoteAccumulator
	observedBlocks        map[uint64]*ObservedBlockMeta
	blockhashToSlot       map[solana.Hash]uint64
	pendingParentByHash   map[solana.Hash][]uint64
	equivocatedSlots      map[uint64]struct{}
	epoch                 uint64
	epochStakes           map[solana.PublicKey]uint64
	epochAuthorizedVoters *epochstakes.EpochAuthorizedVotersCache
	totalEpochStake       uint64
	latestObservedSlot    uint64
	mu                    sync.Mutex
}

type ForkChoiceService struct {
	state    *forkChoiceState
	jobChan  chan *blockJob
	wg       sync.WaitGroup
	shutdown chan struct{}
}

func NewForkChoiceService(
	epoch uint64,
	epochStakes map[solana.PublicKey]uint64,
	totalEpochStake uint64,
	epochAuthorizedVoters *epochstakes.EpochAuthorizedVotersCache,
) *ForkChoiceService {

	state := &forkChoiceState{
		voteStakeTotals:       make(map[uint64]*slotVoteAccumulator),
		observedBlocks:        make(map[uint64]*ObservedBlockMeta),
		blockhashToSlot:       make(map[solana.Hash]uint64),
		pendingParentByHash:   make(map[solana.Hash][]uint64),
		equivocatedSlots:      make(map[uint64]struct{}),
		epoch:                 epoch,
		epochStakes:           epochStakes,
		epochAuthorizedVoters: epochAuthorizedVoters,
		totalEpochStake:       totalEpochStake,
	}

	return &ForkChoiceService{
		state:    state,
		jobChan:  make(chan *blockJob, 32),
		shutdown: make(chan struct{}),
	}
}

func (s *ForkChoiceService) Start() {
	s.wg.Add(1)
	go s.run()
}

func (s *ForkChoiceService) Stop() {
	close(s.shutdown)
	s.wg.Wait()
	close(s.jobChan)
}

func (s *ForkChoiceService) run() {
	defer s.wg.Done()
	for {
		select {
		case job, ok := <-s.jobChan:
			if !ok {
				return
			}
			s.processBlock(job)
		case <-s.shutdown:
			for {
				select {
				case job, ok := <-s.jobChan:
					if !ok {
						return
					}
					s.processBlock(job)
				default:
					return
				}
			}
		}
	}
}

func (s *ForkChoiceService) SubmitBlock(slot uint64, txs []*solana.Transaction) {
	s.state.mu.Lock()
	job := &blockJob{
		slot:                  slot,
		txs:                   txs,
		epochStakes:           s.state.epochStakes,
		epochAuthorizedVoters: s.state.epochAuthorizedVoters,
		totalEpochStake:       s.state.totalEpochStake,
	}
	s.state.mu.Unlock()

	select {
	case s.jobChan <- job:
	case <-s.shutdown:
		fmt.Printf("fork choice service shutting down, discarding job for slot %d\n", slot)
	}
}

// ObserveExecutionAnchor seeds the blockhash->slot index with the last known
// confirmed slot. This lets RPC-fetched children that only carry a parent
// blockhash recover their parent slot without extra RPC lookups.
func (s *ForkChoiceService) ObserveExecutionAnchor(slot uint64, blockhash solana.Hash) {
	if blockhash == (solana.Hash{}) {
		return
	}

	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	s.state.blockhashToSlot[blockhash] = slot
	s.resolvePendingParentsLocked(blockhash, slot)
	s.pruneBeforeSlotLocked(slot)
}

// PruneBeforeSlot drops forkchoice state older than the given slot. This is
// useful for post-execution verification paths that need to retain a small
// trailing window of recent slots without advancing the full execution anchor.
func (s *ForkChoiceService) PruneBeforeSlot(slot uint64) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	s.pruneBeforeSlotLocked(slot)
}

// ObserveSkippedSlot advances the observed watermark when the source tells us a
// slot was skipped. This keeps confirmed-leaf search moving even when no block
// exists for the slot.
func (s *ForkChoiceService) ObserveSkippedSlot(slot uint64) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if s.state.latestObservedSlot < slot {
		s.state.latestObservedSlot = slot
	}
}

// ObserveBlock ingests pre-execution block metadata and vote transactions.
func (s *ForkChoiceService) ObserveBlock(meta ObservedBlockMeta, txs []*solana.Transaction) error {
	s.state.mu.Lock()
	epochStakes := s.state.epochStakes
	epochAuthorizedVoters := s.state.epochAuthorizedVoters
	totalEpochStake := s.state.totalEpochStake
	s.state.mu.Unlock()

	updatesToApply := collectVoteUpdates(txs, epochStakes, epochAuthorizedVoters)

	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if err := s.ingestObservedBlockLocked(meta); err != nil {
		return err
	}

	s.applyVoteUpdatesLocked(updatesToApply, totalEpochStake)

	if s.state.latestObservedSlot < meta.Slot {
		s.state.latestObservedSlot = meta.Slot
	}

	return nil
}

func (s *ForkChoiceService) processBlock(job *blockJob) {
	updatesToApply := collectVoteUpdates(job.txs, job.epochStakes, job.epochAuthorizedVoters)

	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	s.applyVoteUpdatesLocked(updatesToApply, job.totalEpochStake)

	if s.state.latestObservedSlot < job.slot {
		s.state.latestObservedSlot = job.slot
	}
}

// UpdateEpoch swaps in new epoch stake data. Called at epoch boundaries so that
// vote stake weights and authorized voter lookups use current data.
func (s *ForkChoiceService) UpdateEpoch(
	epoch uint64,
	epochStakes map[solana.PublicKey]uint64,
	totalEpochStake uint64,
	epochAuthorizedVoters *epochstakes.EpochAuthorizedVotersCache,
) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	s.state.epoch = epoch
	s.state.epochStakes = epochStakes
	s.state.totalEpochStake = totalEpochStake
	s.state.epochAuthorizedVoters = epochAuthorizedVoters

	mlog.Log.Infof("forkchoice: updated epoch stakes for epoch %d (total_stake=%d, validators=%d)",
		epoch, totalEpochStake, len(epochStakes))
}

func collectVoteUpdates(
	txs []*solana.Transaction,
	epochStakes map[solana.PublicKey]uint64,
	epochAuthorizedVoters *epochstakes.EpochAuthorizedVotersCache,
) []voteUpdate {
	var updatesToApply []voteUpdate

	for _, tx := range txs {
		if !tx.IsVote() {
			continue
		}

		voteInfo, ok := parseAndValidateVoteTx(tx, epochAuthorizedVoters)
		if !ok {
			continue
		}

		stakeForVoteAcct, ok := epochStakes[voteInfo.votePubkey]
		if !ok {
			continue
		}

		updatesToApply = append(updatesToApply, voteUpdate{
			voteInfo: voteInfo,
			stake:    stakeForVoteAcct,
		})
	}

	return updatesToApply
}

func (s *ForkChoiceService) applyVoteUpdatesLocked(updatesToApply []voteUpdate, totalEpochStake uint64) {
	for _, update := range updatesToApply {
		accumulator, exists := s.state.voteStakeTotals[update.voteInfo.slot]
		if !exists {
			accumulator = newSlotVoteAccumulator(totalEpochStake, update.voteInfo.slot)
			s.state.voteStakeTotals[update.voteInfo.slot] = accumulator
		}

		_, _ = accumulator.addVote(
			update.voteInfo.bankHash,
			update.voteInfo.votePubkey,
			update.stake,
		)
	}
}

func (s *ForkChoiceService) ingestObservedBlockLocked(meta ObservedBlockMeta) error {
	existing, exists := s.state.observedBlocks[meta.Slot]
	if exists {
		if existing.Blockhash != meta.Blockhash {
			s.state.equivocatedSlots[meta.Slot] = struct{}{}
			return ErrEquivocation
		}
		if !existing.ParentSlotKnown && meta.ParentSlotKnown {
			existing.ParentSlot = meta.ParentSlot
			existing.ParentSlotKnown = true
		}
		if existing.ParentBlockhash == (solana.Hash{}) && meta.ParentBlockhash != (solana.Hash{}) {
			existing.ParentBlockhash = meta.ParentBlockhash
		}
		meta = *existing
	} else {
		copyMeta := meta
		s.state.observedBlocks[meta.Slot] = &copyMeta
		existing = &copyMeta
	}

	s.state.blockhashToSlot[existing.Blockhash] = existing.Slot
	s.resolvePendingParentsLocked(existing.Blockhash, existing.Slot)

	if !existing.ParentSlotKnown && existing.ParentBlockhash != (solana.Hash{}) {
		if parentSlot, hasParent := s.state.blockhashToSlot[existing.ParentBlockhash]; hasParent {
			existing.ParentSlot = parentSlot
			existing.ParentSlotKnown = true
		} else {
			s.state.pendingParentByHash[existing.ParentBlockhash] = append(s.state.pendingParentByHash[existing.ParentBlockhash], existing.Slot)
		}
	}

	return nil
}

func (s *ForkChoiceService) resolvePendingParentsLocked(parentBlockhash solana.Hash, parentSlot uint64) {
	waiting := s.state.pendingParentByHash[parentBlockhash]
	if len(waiting) == 0 {
		return
	}
	for _, childSlot := range waiting {
		if child, exists := s.state.observedBlocks[childSlot]; exists && !child.ParentSlotKnown {
			child.ParentSlot = parentSlot
			child.ParentSlotKnown = true
		}
	}
	delete(s.state.pendingParentByHash, parentBlockhash)
}

func (s *ForkChoiceService) pruneBeforeSlotLocked(anchorSlot uint64) {
	if anchorSlot == 0 {
		return
	}

	for slot := range s.state.voteStakeTotals {
		if slot < anchorSlot {
			delete(s.state.voteStakeTotals, slot)
		}
	}

	for slot := range s.state.observedBlocks {
		if slot < anchorSlot {
			delete(s.state.observedBlocks, slot)
		}
	}

	for slot := range s.state.equivocatedSlots {
		if slot < anchorSlot {
			delete(s.state.equivocatedSlots, slot)
		}
	}

	for blockhash, slot := range s.state.blockhashToSlot {
		if slot < anchorSlot {
			delete(s.state.blockhashToSlot, blockhash)
		}
	}

	for parentHash, waiting := range s.state.pendingParentByHash {
		filtered := waiting[:0]
		for _, childSlot := range waiting {
			if childSlot >= anchorSlot {
				filtered = append(filtered, childSlot)
			}
		}
		if len(filtered) == 0 {
			delete(s.state.pendingParentByHash, parentHash)
			continue
		}
		s.state.pendingParentByHash[parentHash] = filtered
	}
}

func (s *ForkChoiceService) LatestObservedSlot() uint64 {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.latestObservedSlot
}

// FindConfirmedLeaf returns the highest slot above the current anchor that has
// both an observed block and a vote-confirmed bankhash winner.
func (s *ForkChoiceService) FindConfirmedLeaf(anchorSlot uint64, maxDepth int) (ConfirmedLeaf, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if s.state.latestObservedSlot <= anchorSlot {
		return ConfirmedLeaf{}, ErrNeedWait
	}

	latestSearchSlot := s.state.latestObservedSlot
	if maxDepth > 0 {
		maxLeafSlot := anchorSlot + uint64(maxDepth)
		if latestSearchSlot > maxLeafSlot {
			latestSearchSlot = maxLeafSlot
		}
	}

	for slot := latestSearchSlot; slot > anchorSlot; slot-- {
		if _, equivocated := s.state.equivocatedSlots[slot]; equivocated {
			return ConfirmedLeaf{}, ErrEquivocation
		}

		accumulator, exists := s.state.voteStakeTotals[slot]
		if !exists {
			continue
		}

		winningHash, hasWinner := accumulator.winningHash()
		if !hasWinner {
			continue
		}

		if _, observed := s.state.observedBlocks[slot]; !observed {
			continue
		}

		return ConfirmedLeaf{
			Slot:     slot,
			Bankhash: winningHash,
		}, nil
	}

	if maxDepth > 0 && s.state.latestObservedSlot > anchorSlot+uint64(maxDepth) {
		return ConfirmedLeaf{}, ErrDepthExceeded
	}

	return ConfirmedLeaf{}, ErrNeedWait
}

// ResolvePathToLeaf reconstructs the block/skip decisions from anchorSlot to
// leafSlot using the observed pre-execution block metadata.
func (s *ForkChoiceService) ResolvePathToLeaf(anchorSlot uint64, leafSlot uint64, maxDepth int) (*SolveResult, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	observedSnapshot := make(map[uint64]*ObservedBlockMeta, len(s.state.observedBlocks))
	for slot, meta := range s.state.observedBlocks {
		copyMeta := *meta
		observedSnapshot[slot] = &copyMeta
	}

	equivocatedSnapshot := make(map[uint64]struct{}, len(s.state.equivocatedSlots))
	for slot := range s.state.equivocatedSlots {
		equivocatedSnapshot[slot] = struct{}{}
	}

	return ResolvePohPath(anchorSlot, leafSlot, observedSnapshot, equivocatedSnapshot, maxDepth)
}

// VoteConfirmationTimeoutSlots is the grace window (in slots) before an
// unresolved slot (no supermajority winner) transitions from NeedWait to
// NoSupermajority. This is NOT a mandatory delay before confirmation — a slot
// with observed supermajority is confirmed immediately regardless of this window.
// abcd.
const VoteConfirmationTimeoutSlots = 32

// IsBankhashCorrect queries the confirmation status of a slot's bankhash.
// Returns a BankhashResult with status, winning hash, stake details, and threshold.
//
// A slot is confirmed immediately when a winner is observed — no mandatory delay.
// The timeout window (VoteConfirmationTimeoutSlots) only governs how long an
// unresolved slot (no winner) stays in NeedWait before becoming NoSupermajority.
func (s *ForkChoiceService) IsBankhashCorrect(slot uint64, bankHash solana.Hash) BankhashResult {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	accumulator, exists := s.state.voteStakeTotals[slot]

	if exists {
		winningHash, hasWinner := accumulator.winningHash()
		if hasWinner {
			stakeForHash := accumulator.stakeForHash(bankHash)
			winningStake := accumulator.stakeForHash(winningHash)

			if winningHash == bankHash {
				return BankhashResult{
					Status:          BankhashHasSupermajority,
					WinningHash:     winningHash,
					StakeForHash:    stakeForHash,
					WinningStake:    winningStake,
					TotalEpochStake: accumulator.totalEpochStake,
					ThresholdStake:  accumulator.thresholdStake,
				}
			}

			mlog.Log.Warnf("forkchoice: slot %d bankhash mismatch! our=%s winning=%s (our_stake=%d winning_stake=%d/%d)",
				slot,
				base58.Encode(bankHash[:]),
				base58.Encode(winningHash[:]),
				stakeForHash,
				winningStake,
				accumulator.totalEpochStake,
			)
			return BankhashResult{
				Status:          BankhashNoSupermajority,
				WinningHash:     winningHash,
				StakeForHash:    stakeForHash,
				WinningStake:    winningStake,
				TotalEpochStake: accumulator.totalEpochStake,
				ThresholdStake:  accumulator.thresholdStake,
			}
		}
	}

	if s.state.latestObservedSlot < (slot + VoteConfirmationTimeoutSlots) {
		totalStake := s.state.totalEpochStake
		if exists {
			totalStake = accumulator.totalEpochStake
		}
		return BankhashResult{
			Status:          BankhashNeedWait,
			TotalEpochStake: totalStake,
		}
	}

	if exists {
		stakeForHash := accumulator.stakeForHash(bankHash)
		return BankhashResult{
			Status:          BankhashNoSupermajority,
			StakeForHash:    stakeForHash,
			TotalEpochStake: accumulator.totalEpochStake,
			ThresholdStake:  accumulator.thresholdStake,
		}
	}

	return BankhashResult{
		Status:          BankhashNoSupermajority,
		TotalEpochStake: s.state.totalEpochStake,
	}
}

// GetSupermajorityHash returns the vote-confirmed hash for a slot, if any hash
// has crossed the 2/3 supermajority threshold.
func (s *ForkChoiceService) GetSupermajorityHash(slot uint64) (solana.Hash, BankhashStatus) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if accumulator, exists := s.state.voteStakeTotals[slot]; exists {
		if winningHash, ok := accumulator.winningHash(); ok {
			return winningHash, BankhashHasSupermajority
		}
	}

	if s.state.latestObservedSlot < (slot + VoteConfirmationTimeoutSlots) {
		return solana.Hash{}, BankhashNeedWait
	}

	return solana.Hash{}, BankhashNoSupermajority
}

// SlotVoteDiagnostics returns a compact snapshot of forkchoice vote totals for
// a target slot. It is intended for rare consensus mismatch artifacts rather
// than hot-path logging.
func (s *ForkChoiceService) SlotVoteDiagnostics(slot uint64) SlotVoteDiagnostic {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	out := SlotVoteDiagnostic{
		Slot:               slot,
		Status:             BankhashNoSupermajority.String(),
		LatestObservedSlot: s.state.latestObservedSlot,
		TotalEpochStake:    s.state.totalEpochStake,
	}

	accumulator, exists := s.state.voteStakeTotals[slot]
	if !exists {
		if s.state.latestObservedSlot < slot+VoteConfirmationTimeoutSlots {
			out.Status = BankhashNeedWait.String()
		}
		return out
	}

	out.TotalEpochStake = accumulator.totalEpochStake
	out.ThresholdStake = accumulator.thresholdStake
	if winningHash, ok := accumulator.winningHash(); ok {
		out.Status = BankhashHasSupermajority.String()
		out.WinningHash = base58.Encode(winningHash[:])
	} else if s.state.latestObservedSlot < slot+VoteConfirmationTimeoutSlots {
		out.Status = BankhashNeedWait.String()
	}

	out.Hashes = make([]VoteHashDiagnostic, 0, len(accumulator.trackers))
	for bankhash, tracker := range accumulator.trackers {
		out.Hashes = append(out.Hashes, VoteHashDiagnostic{
			Bankhash:   base58.Encode(bankhash[:]),
			Stake:      tracker.stake,
			VoterCount: len(tracker.voted),
			Confirmed:  accumulator.hashHasSupermajority(bankhash),
		})
	}
	sort.Slice(out.Hashes, func(i, j int) bool {
		if out.Hashes[i].Stake == out.Hashes[j].Stake {
			return out.Hashes[i].Bankhash < out.Hashes[j].Bankhash
		}
		return out.Hashes[i].Stake > out.Hashes[j].Stake
	})
	return out
}
