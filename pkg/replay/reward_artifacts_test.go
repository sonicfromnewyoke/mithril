package replay

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/rewards"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestWriteEpochBoundaryCalculatedRewards(t *testing.T) {
	logDir := t.TempDir()
	spoolDir := t.TempDir()

	slot := uint64(321)
	epoch := uint64(9)

	writers := rewards.NewPartitionedSpoolWriters(spoolDir, slot, 2)
	require.NoError(t, writers.WriteRecord(&rewards.SpoolRecord{
		StakePubkey:     solana.MustPublicKeyFromBase58("CktRuQ4VpYxVgLJYPGn9tD6xLdjvxRKqGZo4PMBVXsfS"),
		VotePubkey:      solana.MustPublicKeyFromBase58("6QWeT6FpJrm8AF1btu6WH2k2Xhq6t5vbheKVfQavmeoZ"),
		StakeLamports:   100,
		CreditsObserved: 42,
		RewardLamports:  7,
		PartitionIndex:  0,
	}))
	require.NoError(t, writers.WriteRecord(&rewards.SpoolRecord{
		StakePubkey:     solana.MustPublicKeyFromBase58("cGfHiC6Kgg3FpFZvgwGcswsCRtp4aBP2fzuXRQPizuN"),
		VotePubkey:      solana.MustPublicKeyFromBase58("6QWeT6FpJrm8AF1btu6WH2k2Xhq6t5vbheKVfQavmeoZ"),
		StakeLamports:   200,
		CreditsObserved: 77,
		RewardLamports:  0,
		PartitionIndex:  1,
	}))
	require.NoError(t, writers.Close())

	nonZeroVotingReward := &atomic.Uint64{}
	nonZeroVotingReward.Store(3)
	zeroVotingReward := &atomic.Uint64{}

	streamResult := &rewards.StreamingRewardsResult{
		SpoolDir:  spoolDir,
		SpoolSlot: slot,
		ValidatorRewards: map[solana.PublicKey]*atomic.Uint64{
			solana.MustPublicKeyFromBase58("6QWeT6FpJrm8AF1btu6WH2k2Xhq6t5vbheKVfQavmeoZ"): nonZeroVotingReward,
			solana.MustPublicKeyFromBase58("8opHzTAnfzRpPEx21XtnrVTX28YQuCpAjcn1PczScKh"):  zeroVotingReward,
		},
		NumStakeRewards: 2,
		NumPartitions:   2,
	}

	artifact, csvPath, err := writeEpochBoundaryCalculatedRewards(logDir, epoch, slot, streamResult)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(logDir, "rewards", "epoch_boundary_calculated_rewards_slot_321.csv"), csvPath)
	require.Equal(t, "rewards/epoch_boundary_calculated_rewards_slot_321.csv", artifact.RewardsCSV)
	require.Equal(t, uint64(2), artifact.NumPartitions)
	require.Equal(t, 2, artifact.Voting.TrackedAccounts)
	require.Equal(t, uint64(1), artifact.Voting.RewardCount)
	require.Equal(t, uint64(3), artifact.Voting.TotalLamports)
	require.Equal(t, uint64(2), artifact.Staking.RecordCount)
	require.Equal(t, uint64(1), artifact.Staking.RewardCount)
	require.Equal(t, uint64(1), artifact.Staking.CreditsOnlyCount)
	require.Equal(t, uint64(7), artifact.Staking.TotalLamports)
	require.Equal(t, uint64(3), artifact.Totals.RecordCount)
	require.Equal(t, uint64(2), artifact.Totals.RewardCount)
	require.Equal(t, uint64(1), artifact.Totals.CreditsOnlyCount)
	require.Equal(t, uint64(10), artifact.Totals.TotalLamports)

	file, err := os.Open(csvPath)
	require.NoError(t, err)
	defer file.Close()

	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"record_type", "reward_type", "recipient_pubkey", "vote_pubkey", "lamports", "stake_lamports", "credits_observed", "partition_index"},
		{"reward", "Voting", "6QWeT6FpJrm8AF1btu6WH2k2Xhq6t5vbheKVfQavmeoZ", "", "3", "", "", ""},
		{"reward", "Staking", "CktRuQ4VpYxVgLJYPGn9tD6xLdjvxRKqGZo4PMBVXsfS", "6QWeT6FpJrm8AF1btu6WH2k2Xhq6t5vbheKVfQavmeoZ", "7", "100", "42", "0"},
		{"credits_update_only", "Staking", "cGfHiC6Kgg3FpFZvgwGcswsCRtp4aBP2fzuXRQPizuN", "6QWeT6FpJrm8AF1btu6WH2k2Xhq6t5vbheKVfQavmeoZ", "0", "200", "77", "1"},
	}, rows)
}
