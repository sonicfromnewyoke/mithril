package turbine

import (
	"fmt"
	"sort"

	"github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/txverify"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

type Entry struct {
	NumHashes uint64
	Hash      solana.Hash
	Txns      []solana.Transaction
}

func (e *Entry) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	if e.NumHashes, err = decoder.ReadUint64(bin.LE); err != nil {
		return fmt.Errorf("read num_hashes: %w", err)
	}
	if _, err = decoder.Read(e.Hash[:]); err != nil {
		return fmt.Errorf("read hash: %w", err)
	}
	numTxns, err := decoder.ReadUint64(bin.LE)
	if err != nil {
		return fmt.Errorf("read transaction count: %w", err)
	}
	if numTxns > uint64(decoder.Remaining()) {
		return fmt.Errorf("transaction count %d exceeds remaining bytes %d", numTxns, decoder.Remaining())
	}
	e.Txns = make([]solana.Transaction, numTxns)
	for i := uint64(0); i < numTxns; i++ {
		if err = e.Txns[i].UnmarshalWithDecoder(decoder); err != nil {
			return fmt.Errorf("read transaction %d: %w", i, err)
		}
	}
	return nil
}

type entryBatch struct {
	Entries []Entry
}

func (b *entryBatch) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	numEntries, err := decoder.ReadUint64(bin.LE)
	if err != nil {
		return fmt.Errorf("read entry count: %w", err)
	}
	if numEntries > uint64(decoder.Remaining()) {
		return fmt.Errorf("entry count %d exceeds remaining bytes %d", numEntries, decoder.Remaining())
	}
	b.Entries = make([]Entry, numEntries)
	for i := uint64(0); i < numEntries; i++ {
		if err = b.Entries[i].UnmarshalWithDecoder(decoder); err != nil {
			return fmt.Errorf("read entry %d: %w", i, err)
		}
	}
	return nil
}

func DecodeEntriesFromDataShreds(shreds []*Shred) ([]Entry, error) {
	if len(shreds) == 0 {
		return nil, nil
	}
	sort.Slice(shreds, func(i, j int) bool {
		return shreds[i].Index < shreds[j].Index
	})

	var entries []Entry
	var batchBytes []byte
	for _, shred := range shreds {
		if shred == nil || shred.Type != ShredTypeData {
			continue
		}
		batchBytes = append(batchBytes, shred.Data...)
		if !shred.DataComplete() {
			continue
		}
		batchEntries, err := decodeEntryBatch(batchBytes)
		if err != nil {
			return nil, fmt.Errorf("decode entry batch ending at shred %d: %w", shred.Index, err)
		}
		entries = append(entries, batchEntries...)
		// Decoded transactions retain slices into the batch buffer for instruction data.
		// Keep the backing array alive instead of reusing and overwriting it.
		batchBytes = nil
	}
	if len(batchBytes) != 0 {
		return nil, fmt.Errorf("slot ended with %d undecoded entry bytes", len(batchBytes))
	}
	return entries, nil
}

func decodeEntryBatch(data []byte) ([]Entry, error) {
	var decoder bin.Decoder
	decoder.SetEncoding(bin.EncodingBin)
	decoder.Reset(data)
	var batch entryBatch
	if err := batch.UnmarshalWithDecoder(&decoder); err != nil {
		return nil, err
	}
	return batch.Entries, nil
}

func BlockFromEntries(slot uint64, parentSlot uint64, entries []Entry) *block.Block {
	blk := &block.Block{
		Slot:             slot,
		SourceParentSlot: parentSlot,
		Transactions:     make([]*solana.Transaction, 0, len(entries)*4),
		Entries:          make([]*block.TxEntry, len(entries)),
		FromLightbringer: true,
	}

	var txOffset uint64
	for entryIdx, entry := range entries {
		txEntry := &block.TxEntry{
			NumHashes: entry.NumHashes,
			Hash:      append([]byte(nil), entry.Hash[:]...),
			Indices:   make([]uint64, len(entry.Txns)),
		}
		for txIdx := range entry.Txns {
			tx := &entry.Txns[txIdx]
			blk.Transactions = append(blk.Transactions, tx)
			blk.NumSignatures += uint64(tx.Message.Header.NumRequiredSignatures)
			blk.Versions = append(blk.Versions, uint8(tx.Message.GetVersion()))
			txEntry.Indices[txIdx] = txOffset + uint64(txIdx)
		}
		blk.Entries[entryIdx] = txEntry
		txOffset += uint64(len(entry.Txns))
	}
	if len(entries) > 0 {
		blk.Blockhash = entries[len(entries)-1].Hash
	}
	return blk
}

func validateBlockTransactions(blk *block.Block) error {
	if blk == nil {
		return nil
	}
	for txIdx, tx := range blk.Transactions {
		if tx == nil {
			return fmt.Errorf("slot %d transaction %d is nil", blk.Slot, txIdx)
		}
		if err := txverify.VerifyTransaction(tx); err != nil {
			txSig := "<missing>"
			if len(tx.Signatures) > 0 {
				txSig = tx.Signatures[0].String()
			}
			return fmt.Errorf("slot %d transaction %d %s version=%d failed signature verification: %w", blk.Slot, txIdx, txSig, tx.Message.GetVersion(), err)
		}
	}
	return nil
}
