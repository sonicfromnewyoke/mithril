package bankhash

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/metrics"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/zeebo/blake3"
)

func CalculateBankHash(slotCtx *sealevel.SlotCtx, writableAccts []*accounts.Account, modifiedAccts []*accounts.Account, parentBankHash [32]byte, numSigs uint64, blockHash [32]byte) []byte {
	adhEnabled := !slotCtx.Features.IsActive(features.RemoveAccountsDeltaHash)
	ltHashEnabled := slotCtx.Features.IsActive(features.AccountsLtHash)

	var acctDeltaHash []byte
	if adhEnabled {
		start := time.Now()
		acctDeltaHash = calculateAcctsDeltaHash(writableAccts)
		metrics.GlobalBlockReplay.AccountsDeltaHash.AddTimingSince(start)
	}

	if ltHashEnabled {
		updateAcctsLtHash(slotCtx, writableAccts)
	}

	hasher := sha256.New()

	// lt accts hash enabled
	if len(acctDeltaHash) == 0 {
		hasher.Write(parentBankHash[:])

		var numSigsBytes [8]byte
		binary.LittleEndian.PutUint64(numSigsBytes[:], numSigs)
		hasher.Write(numSigsBytes[:])

		hasher.Write(blockHash[:])
	} else { // use ADH; now obsolete on all three clusters
		mlog.Log.Infof("including ADH in bankhash")
		hasher.Write(parentBankHash[:])
		hasher.Write(acctDeltaHash)

		var numSigsBytes [8]byte
		binary.LittleEndian.PutUint64(numSigsBytes[:], numSigs)
		hasher.Write(numSigsBytes[:])

		hasher.Write(blockHash[:])
	}

	var bankHash []byte

	if ltHashEnabled {
		currentHashedBytes := hasher.Sum(nil)
		acctsLtHashBytes := slotCtx.AcctsLtHash.Hash()

		finalHasher := sha256.New()
		finalHasher.Write(currentHashedBytes)
		finalHasher.Write(acctsLtHashBytes)
		bankHash = finalHasher.Sum(nil)
	} else {
		bankHash = hasher.Sum(nil)
	}

	return bankHash
}

type acctHash struct {
	Pubkey solana.PublicKey
	Hash   [32]byte
}

func newAcctHash(pubkey solana.PublicKey, hash []byte) acctHash {
	pair := acctHash{Pubkey: pubkey}
	copy(pair.Hash[:], hash)
	return pair
}

func calculateSingleAcctHash(acct accounts.Account) acctHash {
	hasher := blake3.New()

	var lamportBytes [8]byte
	binary.LittleEndian.PutUint64(lamportBytes[:], acct.Lamports)
	_, _ = hasher.Write(lamportBytes[:])

	var rentEpochBytes [8]byte
	binary.LittleEndian.PutUint64(rentEpochBytes[:], acct.RentEpoch)
	_, _ = hasher.Write(rentEpochBytes[:])

	_, _ = hasher.Write(acct.Data)

	if acct.Executable {
		_, _ = hasher.Write([]byte{1})
	} else {
		_, _ = hasher.Write([]byte{0})
	}

	_, _ = hasher.Write(acct.Owner[:])
	_, _ = hasher.Write(acct.Key[:])

	return newAcctHash(acct.Key, hasher.Sum(nil))
}

func calculateAccountHashes(accts []*accounts.Account) []acctHash {
	pairs := make([]acctHash, 0, len(accts))
	for _, acct := range accts {
		if acct.Lamports == 0 {
			pairs = append(pairs, newAcctHash(acct.Key, nil))
		} else {
			pair := calculateSingleAcctHash(*acct)
			pairs = append(pairs, pair)
		}
	}

	return pairs
}

func pubkeyCmp(a solana.PublicKey, b solana.PublicKey) bool {
	for i := uint64(0); i < 4; i++ {
		a1 := binary.BigEndian.Uint64(a[8*i:])
		b1 := binary.BigEndian.Uint64(b[8*i:])
		if a1 != b1 {
			return a1 < b1
		}
	}
	return false
}

func calculateAcctsDeltaHash(accts []*accounts.Account) []byte {
	acctHashes := calculateAccountHashes(accts)

	// sort by pubkey
	sort.SliceStable(acctHashes, func(i, j int) bool {
		return pubkeyCmp(acctHashes[i].Pubkey, acctHashes[j].Pubkey)
	})

	hashes := make([][]byte, len(acctHashes))
	for idx, ah := range acctHashes {
		hashes[idx] = make([]byte, 32)
		copy(hashes[idx], ah.Hash[:])
	}

	return computeMerkleRootLoop(hashes)
}
