package replay

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/base58"
	b "github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/blockstream"
	"github.com/sonicfromnewyoke/mithril/pkg/forkchoice"
	"github.com/sonicfromnewyoke/mithril/pkg/lthash"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type consensusTxDiagnostic struct {
	Index                  int      `json:"index"`
	Signatures             []string `json:"signatures"`
	IsVote                 bool     `json:"is_vote"`
	Version                string   `json:"version"`
	RecentBlockhash        string   `json:"recent_blockhash"`
	AccountKeyCount        int      `json:"account_key_count"`
	WritableAccountCount   int      `json:"writable_account_count,omitempty"`
	ReadonlyAccountCount   int      `json:"readonly_account_count,omitempty"`
	AddressTableCount      int      `json:"address_table_count,omitempty"`
	AddressLookupCount     int      `json:"address_lookup_count,omitempty"`
	AddressTableLookupKeys []string `json:"address_table_lookup_keys,omitempty"`
	InstructionCount       int      `json:"instruction_count"`
	ProgramIDs             []string `json:"program_ids,omitempty"`
	Meta                   any      `json:"meta"`
}

type consensusEntryDiagnostic struct {
	Index        int      `json:"index"`
	NumHashes    uint64   `json:"num_hashes"`
	Hash         string   `json:"hash,omitempty"`
	TxCount      int      `json:"tx_count"`
	FirstTxIndex uint64   `json:"first_tx_index,omitempty"`
	LastTxIndex  uint64   `json:"last_tx_index,omitempty"`
	TxIndices    []uint64 `json:"tx_indices,omitempty"`
}

func consensusHashString(hash [32]byte) string {
	if hash == ([32]byte{}) {
		return ""
	}
	return base58.Encode(hash[:])
}

func consensusByteHashString(hash []byte) string {
	if len(hash) == 0 {
		return ""
	}
	if len(hash) == 32 {
		return base58.Encode(hash)
	}
	return hex.EncodeToString(hash)
}

func consensusLtHashChecksum(ltHash *lthash.LtHash) string {
	if ltHash == nil {
		return ""
	}
	return consensusByteHashString(ltHash.Checksum())
}

func consensusSignatureStrings(tx *solana.Transaction) []string {
	sigs := make([]string, 0, len(tx.Signatures))
	for _, sig := range tx.Signatures {
		sigs = append(sigs, sig.String())
	}
	return sigs
}

func consensusTxVersion(tx *solana.Transaction) string {
	if tx.Message.IsVersioned() {
		return fmt.Sprintf("v%d", tx.Message.GetVersion())
	}
	return "legacy"
}

func consensusProgramIDs(tx *solana.Transaction) []string {
	out := make([]string, 0, len(tx.Message.Instructions))
	seen := make(map[solana.PublicKey]struct{}, len(tx.Message.Instructions))
	for _, instr := range tx.Message.Instructions {
		idx := int(instr.ProgramIDIndex)
		if idx < 0 || idx >= len(tx.Message.AccountKeys) {
			out = append(out, fmt.Sprintf("invalid_program_index:%d", idx))
			continue
		}
		programID := tx.Message.AccountKeys[idx]
		if _, ok := seen[programID]; ok {
			continue
		}
		seen[programID] = struct{}{}
		out = append(out, programID.String())
	}
	return out
}

func consensusLookupTableKeys(tx *solana.Transaction) []string {
	if !tx.Message.IsVersioned() || tx.Message.AddressTableLookups.NumLookups() == 0 {
		return nil
	}
	tableIDs := tx.Message.GetAddressTableLookups().GetTableIDs()
	out := make([]string, 0, len(tableIDs))
	for _, tableID := range tableIDs {
		out = append(out, tableID.String())
	}
	return out
}

func consensusTxMetaDiagnostic(txMeta *rpc.TransactionMeta) any {
	if txMeta == nil {
		return map[string]any{"present": false}
	}
	out := map[string]any{
		"present":                       true,
		"fee":                           txMeta.Fee,
		"pre_balance_count":             len(txMeta.PreBalances),
		"post_balance_count":            len(txMeta.PostBalances),
		"loaded_writable_address_count": len(txMeta.LoadedAddresses.Writable),
		"loaded_readonly_address_count": len(txMeta.LoadedAddresses.ReadOnly),
	}
	if txMeta.Err != nil {
		out["err"] = fmt.Sprintf("%v", txMeta.Err)
	}
	if txMeta.ComputeUnitsConsumed != nil {
		out["compute_units_consumed"] = *txMeta.ComputeUnitsConsumed
	}
	return out
}

func consensusTxDiagnostics(block *b.Block) []consensusTxDiagnostic {
	out := make([]consensusTxDiagnostic, 0, len(block.Transactions))
	for idx, tx := range block.Transactions {
		var txMeta *rpc.TransactionMeta
		if idx < len(block.TxMetas) {
			txMeta = block.TxMetas[idx]
		}

		txDiag := consensusTxDiagnostic{
			Index:              idx,
			Signatures:         consensusSignatureStrings(tx),
			IsVote:             tx.IsVote(),
			Version:            consensusTxVersion(tx),
			RecentBlockhash:    tx.Message.RecentBlockhash.String(),
			AccountKeyCount:    len(tx.Message.AccountKeys),
			AddressTableCount:  len(tx.Message.AddressTableLookups),
			AddressLookupCount: tx.Message.AddressTableLookups.NumLookups(),
			InstructionCount:   len(tx.Message.Instructions),
			ProgramIDs:         consensusProgramIDs(tx),
			Meta:               consensusTxMetaDiagnostic(txMeta),
		}
		if canDeriveAccountsFromMessage(tx) {
			txDiag.WritableAccountCount = len(messageWritableAccounts(&tx.Message))
			txDiag.ReadonlyAccountCount = len(messageReadonlyAccounts(&tx.Message))
		}
		txDiag.AddressTableLookupKeys = consensusLookupTableKeys(tx)
		out = append(out, txDiag)
	}
	return out
}

func consensusEntryDiagnostics(block *b.Block) []consensusEntryDiagnostic {
	out := make([]consensusEntryDiagnostic, 0, len(block.Entries))
	for idx, entry := range block.Entries {
		entryDiag := consensusEntryDiagnostic{
			Index:     idx,
			NumHashes: entry.NumHashes,
			Hash:      consensusByteHashString(entry.Hash),
			TxCount:   len(entry.Indices),
			TxIndices: append([]uint64(nil), entry.Indices...),
		}
		if len(entry.Indices) > 0 {
			entryDiag.FirstTxIndex = entry.Indices[0]
			entryDiag.LastTxIndex = entry.Indices[len(entry.Indices)-1]
		}
		out = append(out, entryDiag)
	}
	return out
}

func consensusBlockDiagnostic(block *b.Block) map[string]any {
	voteTxCount := 0
	for _, tx := range block.Transactions {
		if tx.IsVote() {
			voteTxCount++
		}
	}

	return map[string]any{
		"slot":                        block.Slot,
		"parent_slot":                 block.ParentSlot,
		"source_parent_slot":          block.SourceParentSlot,
		"block_height":                block.BlockHeight,
		"epoch":                       block.Epoch,
		"from_lightbringer":           block.FromLightbringer,
		"is_skipped":                  block.IsSkipped,
		"leader":                      block.Leader.String(),
		"blockhash":                   consensusHashString(block.Blockhash),
		"last_blockhash":              consensusHashString(block.LastBlockhash),
		"parent_bankhash":             consensusHashString(block.ParentBankhash),
		"expected_bankhash":           consensusHashString(block.ExpectedBankhash),
		"accts_lthash_checksum":       consensusLtHashChecksum(block.AcctsLtHash),
		"num_signatures":              block.NumSignatures,
		"prev_num_signatures":         block.PrevNumSignatures,
		"initial_lamports_per_sig":    block.InitialPreviousLamportsPerSignature,
		"tx_count":                    len(block.Transactions),
		"vote_tx_count":               voteTxCount,
		"non_vote_tx_count":           len(block.Transactions) - voteTxCount,
		"tx_meta_count":               len(block.TxMetas),
		"entry_count":                 len(block.Entries),
		"entries":                     consensusEntryDiagnostics(block),
		"transactions":                consensusTxDiagnostics(block),
		"latest_evicted_blockhash":    consensusHashString(block.LatestEvictedBlockhash),
		"has_eah_workaround":          block.HasEahWorkaround,
		"eah_workaround_bankhash":     consensusByteHashString(block.EahWorkaroundBankhash),
		"num_reward_partitions":       block.NumRewardPartitions,
		"reward_count":                len(block.Rewards),
		"updated_account_count":       len(block.UpdatedAccts),
		"epoch_updated_account_count": len(block.EpochUpdatedAccts),
	}
}

func consensusObservedBlocksDiagnostic(blocks map[uint64]*b.Block) []map[string]any {
	slots := make([]uint64, 0, len(blocks))
	for slot := range blocks {
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })

	out := make([]map[string]any, 0, len(slots))
	for _, slot := range slots {
		block := blocks[slot]
		out = append(out, map[string]any{
			"slot":               block.Slot,
			"source_parent_slot": block.SourceParentSlot,
			"from_lightbringer":  block.FromLightbringer,
			"is_skipped":         block.IsSkipped,
			"blockhash":          consensusHashString(block.Blockhash),
			"last_blockhash":     consensusHashString(block.LastBlockhash),
			"tx_count":           len(block.Transactions),
			"entry_count":        len(block.Entries),
		})
	}
	return out
}

func consensusDecisionDiagnostics(decisions []forkchoice.SlotDecision) []map[string]any {
	out := make([]map[string]any, 0, len(decisions))
	for _, decision := range decisions {
		out = append(out, map[string]any{
			"slot":      decision.Slot,
			"use_block": decision.UseBlock,
		})
	}
	return out
}

// writeConsensusArtifact writes a best-effort JSON diagnostic artifact to the
// per-run consensus subdirectory. If the log dir is empty or any step fails,
// it logs a warning and continues; artifact failure must not crash replay.
func writeConsensusArtifact(filename string, data map[string]interface{}) {
	logDir := mlog.GetLogDir()
	if logDir == "" {
		return
	}
	dir := filepath.Join(logDir, "consensus")
	if err := os.MkdirAll(dir, 0755); err != nil {
		mlog.Log.Warnf("consensus artifact: failed to create directory %s: %v", dir, err)
		return
	}
	artifactPath := filepath.Join(dir, filename)
	artifactJSON, jsonErr := json.MarshalIndent(data, "", "  ")
	if jsonErr != nil {
		mlog.Log.Warnf("consensus artifact: failed to marshal JSON for %s: %v", filename, jsonErr)
		return
	}
	if writeErr := os.WriteFile(artifactPath, artifactJSON, 0644); writeErr != nil {
		mlog.Log.Warnf("consensus artifact: failed to write %s: %v", artifactPath, writeErr)
		return
	}
	mlog.Log.FileOnlyf("consensus artifact written: %s", artifactPath)
}

func buildConsensusMismatchArtifact(
	block *b.Block,
	slotCtx *sealevel.SlotCtx,
	path *pendingConsensusPath,
	actualBankhash solana.Hash,
	fetchStats blockstream.FetchStatsSnapshot,
	forkChoice *forkchoice.ForkChoiceService,
	consensusPolicy string,
	observedConsensusBlocks map[uint64]*b.Block,
	executionAnchorAfterReplay uint64,
) map[string]interface{} {
	artifact := map[string]interface{}{
		"type":                          "bankhash_mismatch",
		"checked_slot":                  block.Slot,
		"our_bankhash":                  base58.Encode(actualBankhash[:]),
		"winning_bankhash":              base58.Encode(path.leafBankhash[:]),
		"policy":                        consensusPolicy,
		"run_id":                        CurrentRunID,
		"created_at":                    time.Now().UTC().Format(time.RFC3339Nano),
		"path_anchor_slot":              path.anchorSlot,
		"execution_anchor_after_replay": executionAnchorAfterReplay,
		"source": map[string]interface{}{
			"current_source":       fetchStats.CurrentSource,
			"source_status":        fetchStats.SourceStatus,
			"is_near_tip":          fetchStats.IsNearTip,
			"next_slot":            fetchStats.NextSlot,
			"confirmed_tip":        fetchStats.ConfirmedTip,
			"processed_tip":        fetchStats.ProcessedTip,
			"handoff_slot":         fetchStats.HandoffSlot,
			"waiting_slot_state":   fetchStats.WaitingSlotState,
			"waiting_slot_retries": fetchStats.WaitingSlotRetries,
			"inflight":             fetchStats.InflightCount,
			"retry_queue_len":      fetchStats.RetryQueueLen,
			"stream_buffer_depth":  fetchStats.BufferDepth,
			"reorder_buffer_len":   fetchStats.ReorderBufLen,
		},
		"block":                     consensusBlockDiagnostic(block),
		"forkchoice_vote_summary":   forkChoice.SlotVoteDiagnostics(path.leafSlot),
		"parent_vote_summary":       forkChoice.SlotVoteDiagnostics(block.ParentSlot),
		"observed_consensus_blocks": consensusObservedBlocksDiagnostic(observedConsensusBlocks),
		"resolved_path": map[string]interface{}{
			"anchor_slot":         path.anchorSlot,
			"leaf_slot":           path.leafSlot,
			"leaf_bankhash":       base58.Encode(path.leafBankhash[:]),
			"remaining_decisions": consensusDecisionDiagnostics(path.decisions),
			"original_decisions":  consensusDecisionDiagnostics(path.originalDecisions),
		},
	}
	if slotCtx != nil {
		artifact["slot_context"] = map[string]interface{}{
			"slot":                         slotCtx.Slot,
			"parent_slot":                  slotCtx.ParentSlot,
			"epoch":                        slotCtx.Epoch,
			"blockhash":                    consensusHashString(slotCtx.Blockhash),
			"last_blockhash":               consensusHashString(slotCtx.LastBlockhash),
			"latest_evicted_blockhash":     consensusHashString(slotCtx.LatestEvictedBlockhash),
			"final_bankhash":               consensusByteHashString(slotCtx.FinalBankhash),
			"accts_lthash_checksum":        consensusLtHashChecksum(slotCtx.AcctsLtHash),
			"num_signatures":               slotCtx.NumSignatures,
			"lamports_burnt":               slotCtx.LamportsBurnt,
			"total_compute_units_consumed": slotCtx.TotalComputeUnitsConsumed,
			"modified_account_count":       len(slotCtx.ModifiedAccts),
			"writable_account_count":       len(slotCtx.WritableAccts),
			"total_epoch_stake":            slotCtx.TotalEpochStake,
		}
	}
	return artifact
}
