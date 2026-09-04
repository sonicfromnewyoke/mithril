package turbine

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sonicfromnewyoke/mithril/fixtures"
	"github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/txverify"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

func TestParseMerkleDataShred(t *testing.T) {
	raw := fixtures.DataShreds(t, "mainnet", 102815960)[0]

	shred, err := ParseShred(raw)
	if err != nil {
		t.Fatalf("ParseShred returned error: %v", err)
	}

	if shred.Slot != 102815960 {
		t.Fatalf("slot = %d, want 102815960", shred.Slot)
	}
	if shred.Index != 0 {
		t.Fatalf("index = %d, want 0", shred.Index)
	}
	if shred.ParentSlot() != 102815959 {
		t.Fatalf("parent slot = %d, want 102815959", shred.ParentSlot())
	}
	if len(shred.Data) == 0 {
		t.Fatalf("expected parsed data payload")
	}
}

func TestClassifyCurrentMerkleVariants(t *testing.T) {
	for _, variant := range []byte{0x80, 0x8f, 0x90, 0x9f, 0xb0, 0xbf} {
		shredType, err := classifyVariant(variant)
		if err != nil {
			t.Fatalf("classifyVariant(0x%02x) returned error: %v", variant, err)
		}
		if shredType != ShredTypeData {
			t.Fatalf("classifyVariant(0x%02x) = %v, want data", variant, shredType)
		}
	}
	for _, variant := range []byte{0x40, 0x4f, 0x60, 0x6f, 0x70, 0x7f} {
		shredType, err := classifyVariant(variant)
		if err != nil {
			t.Fatalf("classifyVariant(0x%02x) returned error: %v", variant, err)
		}
		if shredType != ShredTypeCode {
			t.Fatalf("classifyVariant(0x%02x) = %v, want code", variant, shredType)
		}
	}
	for _, variant := range []byte{0x00, 0x50, 0xa0, 0xc0} {
		if _, err := classifyVariant(variant); !errors.Is(err, ErrUnsupportedShred) {
			t.Fatalf("classifyVariant(0x%02x) error = %v, want ErrUnsupportedShred", variant, err)
		}
	}
}

func TestMerkleShredSignatureVerification(t *testing.T) {
	packet := make([]byte, dataPayloadSize)
	packet[shredVariantOffset] = merkleDataVariant
	packet[dataFlagsOffset] = shredFlagLastShredInSlot
	binary.LittleEndian.PutUint64(packet[shredSlotOffset:shredSlotOffset+8], 10)
	binary.LittleEndian.PutUint32(packet[shredIndexOffset:shredIndexOffset+4], 0)
	binary.LittleEndian.PutUint16(packet[shredVersionOffset:shredVersionOffset+2], 1)
	binary.LittleEndian.PutUint32(packet[shredFECSetIndexOffset:shredFECSetIndexOffset+4], 0)
	binary.LittleEndian.PutUint16(packet[dataParentOffsetOffset:dataParentOffsetOffset+2], 1)
	binary.LittleEndian.PutUint16(packet[dataSizeOffset:dataSizeOffset+2], dataHeaderSize+4)
	copy(packet[dataHeaderSize:], []byte("test"))

	shred, err := ParseShred(packet)
	if err != nil {
		t.Fatalf("ParseShred returned error: %v", err)
	}
	root, err := shred.MerkleRoot()
	if err != nil {
		t.Fatalf("MerkleRoot returned error: %v", err)
	}

	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	signature := ed25519.Sign(privateKey, root[:])
	copy(packet[shredSignatureOffset:shredSignatureSize], signature)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	var leader solana.PublicKey
	copy(leader[:], publicKey)

	shred, err = ParseShred(packet)
	if err != nil {
		t.Fatalf("ParseShred signed packet returned error: %v", err)
	}
	if err := shred.VerifySignature(leader); err != nil {
		t.Fatalf("VerifySignature returned error: %v", err)
	}

	packet[dataHeaderSize] ^= 0xff
	corrupt, err := ParseShred(packet)
	if err != nil {
		t.Fatalf("ParseShred corrupt packet returned error: %v", err)
	}
	if err := corrupt.VerifySignature(leader); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifySignature corrupt error = %v, want ErrInvalidSignature", err)
	}
}

func TestSlotAssemblerBuildsBlockFromCompleteDataShreds(t *testing.T) {
	rawShreds := fixtures.DataShreds(t, "mainnet", 102815960)
	assembler := NewSlotAssembler()

	var blockSeen bool
	for idx, raw := range rawShreds {
		blk, err := assembler.AddPacket(raw)
		if err != nil {
			t.Fatalf("AddPacket(%d) returned error: %v", idx, err)
		}
		if blk == nil {
			continue
		}
		blockSeen = true
		if idx != len(rawShreds)-1 {
			t.Fatalf("assembled block before final shred: idx=%d total=%d", idx, len(rawShreds))
		}
		if blk.Slot != 102815960 {
			t.Fatalf("block slot = %d, want 102815960", blk.Slot)
		}
		if blk.SourceParentSlot != 102815959 {
			t.Fatalf("source parent slot = %d, want 102815959", blk.SourceParentSlot)
		}
		if !blk.FromLightbringer {
			t.Fatalf("expected block to be marked as live shred-stream sourced")
		}
		if len(blk.Transactions) != 3177 {
			t.Fatalf("transactions = %d, want 3177", len(blk.Transactions))
		}
		if len(blk.Transactions[0].Signatures) == 0 {
			t.Fatalf("first transaction has no signatures")
		}
	}

	if !blockSeen {
		t.Fatalf("assembler did not emit a completed block")
	}
}

func TestValidateBlockTransactionsRejectsInvalidSignature(t *testing.T) {
	rawShreds := fixtures.DataShreds(t, "mainnet", 102815960)
	assembler := NewSlotAssembler()

	var blk *block.Block
	for _, raw := range rawShreds {
		var err error
		blk, err = assembler.AddPacket(raw)
		if err != nil {
			t.Fatalf("AddPacket returned error: %v", err)
		}
	}
	if blk == nil || len(blk.Transactions) == 0 {
		t.Fatalf("assembler did not emit a block with transactions")
	}

	blk.Transactions[0].Message.RecentBlockhash[0] ^= 0xff
	if err := validateBlockTransactions(blk); err == nil {
		t.Fatalf("validateBlockTransactions accepted a mutated transaction")
	}
}

func TestFixtureRawTransactionSignatureBytes(t *testing.T) {
	rawShreds := fixtures.DataShreds(t, "mainnet", 102815960)
	var batchBytes []byte
	var tx solana.Transaction
	var rawTx []byte
	var txIndex uint64
	for _, raw := range rawShreds {
		shred, err := ParseShred(raw)
		if err != nil {
			t.Fatalf("ParseShred returned error: %v", err)
		}
		batchBytes = append(batchBytes, shred.Data...)
		if !shred.DataComplete() {
			continue
		}

		var decoder bin.Decoder
		decoder.SetEncoding(bin.EncodingBin)
		decoder.Reset(batchBytes)
		numEntries, err := decoder.ReadUint64(bin.LE)
		if err != nil {
			t.Fatalf("read entry count: %v", err)
		}
		for entryIdx := uint64(0); entryIdx < numEntries; entryIdx++ {
			if _, err := decoder.ReadUint64(bin.LE); err != nil {
				t.Fatalf("read num hashes: %v", err)
			}
			if _, err := decoder.ReadBytes(32); err != nil {
				t.Fatalf("read hash: %v", err)
			}
			numTxns, err := decoder.ReadUint64(bin.LE)
			if err != nil {
				t.Fatalf("read tx count: %v", err)
			}
			for i := uint64(0); i < numTxns; i++ {
				start := decoder.Position()
				var current solana.Transaction
				if err := current.UnmarshalWithDecoder(&decoder); err != nil {
					t.Fatalf("read tx: %v", err)
				}
				if txIndex == 1 {
					tx = current
					rawTx = append([]byte(nil), batchBytes[start:decoder.Position()]...)
					break
				}
				txIndex++
			}
			if rawTx != nil {
				break
			}
		}
		if rawTx != nil {
			break
		}
		batchBytes = batchBytes[:0]
	}
	if rawTx == nil {
		t.Fatalf("did not find transaction 1")
	}

	rawDecoder := bin.NewBinDecoder(rawTx)
	numSigs, err := rawDecoder.ReadCompactU16()
	if err != nil {
		t.Fatalf("read raw signature count: %v", err)
	}
	if numSigs != len(tx.Signatures) {
		t.Fatalf("raw signature count = %d, decoded signatures = %d", numSigs, len(tx.Signatures))
	}
	for i := 0; i < numSigs; i++ {
		if _, err := rawDecoder.ReadBytes(64); err != nil {
			t.Fatalf("read raw signature %d: %v", i, err)
		}
	}
	rawMessage := rawTx[rawDecoder.Position():]
	remarshaled, err := tx.Message.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary returned error: %v", err)
	}
	if string(remarshaled) != string(rawMessage) {
		limit := len(remarshaled)
		if len(rawMessage) < limit {
			limit = len(rawMessage)
		}
		diff := -1
		for i := 0; i < limit; i++ {
			if remarshaled[i] != rawMessage[i] {
				diff = i
				break
			}
		}
		t.Fatalf("remarshaled message differs from raw at %d raw_len=%d remarshaled_len=%d raw_prefix=%x remarshaled_prefix=%x", diff, len(rawMessage), len(remarshaled), rawMessage[:min(24, len(rawMessage))], remarshaled[:min(24, len(remarshaled))])
	}
	signers := tx.Message.Signers()
	if len(signers) != len(tx.Signatures) {
		t.Fatalf("signers=%d signatures=%d", len(signers), len(tx.Signatures))
	}
	for i, sig := range tx.Signatures {
		if !sig.Verify(signers[i], rawMessage) {
			t.Fatalf("raw message signature %d failed for %s", i, signers[i])
		}
	}
	if err := txverify.VerifyTransaction(&tx); err != nil {
		t.Fatalf("txverify failed for raw transaction: %v", err)
	}

	var parsed []*Shred
	for _, raw := range rawShreds {
		shred, err := ParseShred(raw)
		if err != nil {
			t.Fatalf("ParseShred for block decode returned error: %v", err)
		}
		parsed = append(parsed, shred)
	}
	entries, err := DecodeEntriesFromDataShreds(parsed)
	if err != nil {
		t.Fatalf("DecodeEntriesFromDataShreds returned error: %v", err)
	}
	var entryTx *solana.Transaction
	var entryTxIndex uint64
	for entryIdx := range entries {
		for txIdx := range entries[entryIdx].Txns {
			if entryTxIndex == 1 {
				entryTx = &entries[entryIdx].Txns[txIdx]
				break
			}
			entryTxIndex++
		}
		if entryTx != nil {
			break
		}
	}
	if entryTx == nil {
		t.Fatalf("did not find decoded entry transaction 1")
	}
	entryMsg, _ := txverify.MessageBytes(entryTx)
	if string(entryMsg) != string(rawMessage) {
		t.Fatalf("decoded transaction message does not match raw message")
	}
	if err := txverify.VerifyTransaction(entryTx); err != nil {
		t.Fatalf("txverify failed for decoded entry transaction 1: %v", err)
	}
	blk := BlockFromEntries(102815960, 102815959, entries)
	if len(blk.Transactions) < 2 {
		t.Fatalf("block transactions = %d", len(blk.Transactions))
	}
	if err := txverify.VerifyTransaction(blk.Transactions[1]); err != nil {
		t.Fatalf("txverify failed for block transaction 1: %v", err)
	}
}

func TestSlotAssemblerIgnoresCodingShreds(t *testing.T) {
	raw := fixtures.CodeShreds(t, "mainnet", 102815960)[0]

	blk, err := NewSlotAssembler().AddPacket(raw)
	if err != nil {
		t.Fatalf("AddPacket returned error: %v", err)
	}
	if blk != nil {
		t.Fatalf("expected coding shred to be ignored")
	}

	if _, err := ParseShred(raw); !errors.Is(err, ErrCodingShredIgnored) {
		t.Fatalf("ParseShred error = %v, want ErrCodingShredIgnored", err)
	}
}

func TestParseMerkleCodingShred(t *testing.T) {
	raw := localnetMerkleShreds(t, "c")[0]

	shred, err := ParseShred(raw)
	if err != nil {
		t.Fatalf("ParseShred returned error: %v", err)
	}
	if shred.Type != ShredTypeCode {
		t.Fatalf("type = %v, want code", shred.Type)
	}
	if shred.NumDataShreds == 0 || shred.NumCodingShreds == 0 {
		t.Fatalf("invalid FEC layout: data=%d coding=%d", shred.NumDataShreds, shred.NumCodingShreds)
	}
	if _, err := shred.erasureShard(); err != nil {
		t.Fatalf("erasureShard returned error: %v", err)
	}
}

func TestSlotAssemblerRecoversMissingMerkleDataShredFromCodingShreds(t *testing.T) {
	dataShreds := localnetMerkleShreds(t, "d")
	codeShreds := localnetMerkleShreds(t, "c")
	if len(dataShreds) < 2 || len(codeShreds) == 0 {
		t.Fatalf("fixture needs data and coding shreds")
	}

	missing, err := ParseShred(dataShreds[1])
	if err != nil {
		t.Fatalf("ParseShred missing data returned error: %v", err)
	}
	assembler := NewSlotAssembler()
	for idx, raw := range dataShreds {
		if idx == 1 {
			continue
		}
		if _, err := assembler.AddPacket(raw); err != nil {
			t.Fatalf("AddPacket data %d returned error: %v", idx, err)
		}
	}
	var blockSeen bool
	for idx, raw := range codeShreds {
		blk, err := assembler.AddPacket(raw)
		if err != nil {
			t.Fatalf("AddPacket code %d returned error: %v", idx, err)
		}
		if blk != nil {
			blockSeen = true
		}
	}
	if blockSeen {
		return
	}

	assembler.mu.Lock()
	defer assembler.mu.Unlock()
	state := assembler.slots[missing.Slot]
	if state == nil {
		t.Fatalf("slot state missing for slot %d", missing.Slot)
	}
	recovered := state.shreds[missing.Index]
	if recovered == nil {
		t.Fatalf("missing data shred index %d was not recovered", missing.Index)
	}
	if !recovered.Recovered {
		t.Fatalf("data shred index %d present but not marked recovered", missing.Index)
	}
	if string(recovered.Data) != string(missing.Data) {
		t.Fatalf("recovered data does not match original data")
	}
}

func TestSlotAssemblerRejectsOversizedShredIndex(t *testing.T) {
	raw := fixtures.DataShreds(t, "mainnet", 102815960)[0]
	shred, err := ParseShred(raw)
	if err != nil {
		t.Fatalf("ParseShred returned error: %v", err)
	}
	shred.Index = maxDataShredsPerSlot

	if _, err := NewSlotAssembler().AddShred(shred); !errors.Is(err, ErrSlotOverflow) {
		t.Fatalf("AddShred error = %v, want ErrSlotOverflow", err)
	}
}

func TestSlotAssemblerPrunesOldIncompleteSlots(t *testing.T) {
	assembler := NewSlotAssembler()
	maxSlot := maxRetainedIncompleteSlotLag + 20
	for slot := uint64(1); slot <= maxSlot; slot++ {
		_, err := assembler.AddShred(&Shred{
			Variant:      legacyDataVariant,
			Type:         ShredTypeData,
			Slot:         slot,
			Index:        0,
			Version:      1,
			ParentOffset: 1,
			Data:         []byte{byte(slot)},
		})
		if err != nil {
			t.Fatalf("AddShred(%d) returned error: %v", slot, err)
		}
	}

	if got := assembler.ActiveSlots(); got > int(maxRetainedIncompleteSlotLag)+1 {
		t.Fatalf("active slots = %d, want at most %d", got, maxRetainedIncompleteSlotLag+1)
	}
	if got := assembler.EvictedSlots(); got == 0 {
		t.Fatalf("expected old incomplete slots to be evicted")
	}
	assembler.mu.Lock()
	_, oldExists := assembler.slots[1]
	assembler.mu.Unlock()
	if oldExists {
		t.Fatalf("expected oldest incomplete slot to be evicted")
	}

	_, err := assembler.AddShred(&Shred{
		Variant:      legacyDataVariant,
		Type:         ShredTypeData,
		Slot:         1,
		Index:        1,
		Version:      1,
		ParentOffset: 1,
		Data:         []byte{1},
	})
	if err != nil {
		t.Fatalf("AddShred for stale slot returned error: %v", err)
	}
	assembler.mu.Lock()
	_, oldExists = assembler.slots[1]
	assembler.mu.Unlock()
	if oldExists {
		t.Fatalf("expected stale shred not to recreate evicted slot")
	}
	if got := assembler.IgnoredOldShreds(); got == 0 {
		t.Fatalf("expected stale shred counter to increment")
	}
}

func TestSlotAssemblerRepairRequestsIncludeAbsentAndIncompleteSlots(t *testing.T) {
	assembler := NewSlotAssembler()
	for _, shred := range []*Shred{
		{
			Variant:      legacyDataVariant,
			Type:         ShredTypeData,
			Slot:         10,
			Index:        1,
			Version:      1,
			ParentOffset: 1,
			Data:         []byte{1},
		},
		{
			Variant:      legacyDataVariant,
			Type:         ShredTypeData,
			Slot:         14,
			Index:        0,
			Version:      1,
			ParentOffset: 1,
			Data:         []byte{1},
		},
	} {
		if _, err := assembler.AddShred(shred); err != nil {
			t.Fatalf("AddShred returned error: %v", err)
		}
	}

	requests := assembler.RepairRequests(32, 8)
	bySlot := make(map[uint64]SlotRepairRequest, len(requests))
	for _, req := range requests {
		bySlot[req.Slot] = req
	}

	if req, ok := bySlot[9]; !ok || !req.NeedHighestDataShred || req.HighestDataShredIndex != 0 {
		t.Fatalf("slot 9 absent repair request = %+v, ok=%v", req, ok)
	}
	req, ok := bySlot[10]
	if !ok {
		t.Fatalf("expected repair request for incomplete slot 10")
	}
	if !req.NeedHighestDataShred || req.HighestDataShredIndex != 2 {
		t.Fatalf("slot 10 highest request = need %v index %d, want true index 2", req.NeedHighestDataShred, req.HighestDataShredIndex)
	}
	if len(req.MissingDataShreds) != 1 || req.MissingDataShreds[0] != 0 {
		t.Fatalf("slot 10 missing shreds = %v, want [0]", req.MissingDataShreds)
	}
}

func TestUDPReceiverSignalsReadyAfterBind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	receiver := NewUDPReceiver("127.0.0.1:0")
	done := make(chan error, 1)
	go func() {
		done <- receiver.Run(ctx)
	}()

	select {
	case err := <-receiver.Ready():
		if err != nil {
			t.Fatalf("Ready returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for receiver readiness")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for receiver shutdown")
	}
}

func localnetMerkleShreds(t testing.TB, prefix string) [][]byte {
	t.Helper()
	dir := fixtures.Path(t, "shreds", "localnet", "merkle")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read localnet merkle shreds: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	var shreds [][]byte
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("cannot read shred %s: %v", entry.Name(), err)
		}
		shreds = append(shreds, raw)
	}
	return shreds
}
