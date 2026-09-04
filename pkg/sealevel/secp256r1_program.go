package sealevel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/big"

	"github.com/sonicfromnewyoke/mithril/pkg/safemath"
)

const (
	Secp256r1CompressedPubkeySerializedSize = 33
	Secp256r1SignatureSerializedSize        = 64
	Secp256r1SignatureOffsetsSerializedSize = 14
	Secp256r1SignatureOffsetsStart          = 2
	Secp256r1DataStart                      = Secp256r1SignatureOffsetsSerializedSize + Secp256r1SignatureOffsetsStart
)

type Secp256r1SignatureOffsets struct {
	SignatureOffset           uint16
	SignatureInstructionIndex uint16
	PublicKeyOffset           uint16
	PublicKeyInstructionIndex uint16
	MessageDataOffset         uint16
	MessageDataSize           uint16
	MessageInstructionIndex   uint16
}

func parseSecp256r1SignatureOffsets(data []byte) Secp256r1SignatureOffsets {
	var offsets Secp256r1SignatureOffsets
	offsets.SignatureOffset = binary.LittleEndian.Uint16(data[:2])
	offsets.SignatureInstructionIndex = binary.LittleEndian.Uint16(data[2:4])
	offsets.PublicKeyOffset = binary.LittleEndian.Uint16(data[4:6])
	offsets.PublicKeyInstructionIndex = binary.LittleEndian.Uint16(data[6:8])
	offsets.MessageDataOffset = binary.LittleEndian.Uint16(data[8:10])
	offsets.MessageDataSize = binary.LittleEndian.Uint16(data[10:12])
	offsets.MessageInstructionIndex = binary.LittleEndian.Uint16(data[12:14])

	return offsets
}

func Secp256r1GetDataSlice(txCtx *TransactionCtx, index uint16, offset uint16, size uint64) ([]byte, error) {

	var data []byte
	var dataSize uint64

	if index == math.MaxUint16 {
		instrCtx, err := txCtx.CurrentInstructionCtx()
		if err != nil {
			return nil, PrecompileErrSignature
		}
		data = instrCtx.Data
		dataSize = uint64(len(data))
	} else {
		signatureIdx := uint64(index)
		if signatureIdx >= uint64(len(txCtx.AllInstructions)) {
			return nil, PrecompileErrDataOffset
		}
		data = txCtx.AllInstructions[signatureIdx].Data
		dataSize = uint64(len(data))
	}

	end := safemath.SaturatingAddU64(uint64(offset), size)
	if end > dataSize {
		return nil, PrecompileErrSignature
	}

	return data[offset:end], nil
}

func unmarshalSecp256r1Pubkey(pkBytes []byte) (*ecdsa.PublicKey, error) {
	curve := elliptic.P256()
	x, y := elliptic.UnmarshalCompressed(curve, pkBytes)
	if x == nil {
		return nil, PrecompileErrPublicKey
	}

	pk := &ecdsa.PublicKey{
		Curve: curve,
		X:     x,
		Y:     y,
	}

	return pk, nil
}

var oneBigInt = new(big.Int).SetUint64(1)
var secp256r1OrderMinusOne = new(big.Int).SetBytes([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0xFF, 0xBC, 0xE6, 0xFA, 0xAD, 0xA7, 0x17, 0x9E, 0x84, 0xF3, 0xB9, 0xCA, 0xC2, 0xFC, 0x63,
	0x25, 0x50})

var secp256r1HalfOrder = new(big.Int).SetBytes([]byte{0x7F, 0xFF, 0xFF, 0xFF, 0x80, 0x00, 0x00, 0x00, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0xFF, 0xDE, 0x73, 0x7D, 0x56, 0xD3, 0x8B, 0xCF, 0x42, 0x79, 0xDC, 0xE5, 0x61, 0x7E, 0x31,
	0x92, 0xA8})

func isSecp256r1SignatureWithinRange(r *big.Int, s *big.Int) bool {
	if r.Cmp(oneBigInt) == -1 {
		return false
	}

	if r.Cmp(secp256r1OrderMinusOne) == 1 {
		return false
	}

	if s.Cmp(oneBigInt) == -1 {
		return false
	}

	if s.Cmp(secp256r1HalfOrder) == 1 {
		return false
	}

	return true
}

func verifySecp256r1(pk, msg, sig []byte) error {
	rBigNum := new(big.Int).SetBytes(sig[:32])
	sBigNum := new(big.Int).SetBytes(sig[32:])

	if !isSecp256r1SignatureWithinRange(rBigNum, sBigNum) {
		return PrecompileErrSignature
	}

	pubkey, err := unmarshalSecp256r1Pubkey(pk)
	if err != nil {
		return err
	}

	hasher := sha256.New()
	hasher.Write(msg)
	hash := hasher.Sum(nil)

	if !ecdsa.Verify(pubkey, hash, rBigNum, sBigNum) {
		return PrecompileErrSignature
	}

	return nil
}

func Secp256r1ProgramExecute(execCtx *ExecutionCtx) error {
	txCtx := execCtx.TransactionContext
	instrCtx, err := txCtx.CurrentInstructionCtx()
	if err != nil {
		return err
	}

	data := instrCtx.Data
	dataLen := uint64(len(data))

	if dataLen < Secp256r1SignatureOffsetsStart {
		return PrecompileErrInstrDataSize
	}

	numSignatures := uint64(data[0])
	if numSignatures == 0 {
		return PrecompileErrInstrDataSize
	}
	if numSignatures > 8 {
		return PrecompileErrInstrDataSize
	}

	expectedDataSize := safemath.SaturatingAddU64(safemath.SaturatingMulU64(numSignatures, Secp256r1SignatureOffsetsSerializedSize), Secp256r1SignatureOffsetsStart)
	if dataLen < expectedDataSize {
		return PrecompileErrInstrDataSize
	}

	for i := range numSignatures {
		start := safemath.SaturatingAddU64(safemath.SaturatingMulU64(i, Secp256r1SignatureOffsetsSerializedSize), Secp256r1SignatureOffsetsStart)
		offsets := parseSecp256r1SignatureOffsets(data[start:])

		signature, err := Secp256r1GetDataSlice(txCtx, offsets.SignatureInstructionIndex, offsets.SignatureOffset, Secp256r1SignatureSerializedSize)
		if err != nil {
			return err
		}

		pkBytes, err := Secp256r1GetDataSlice(txCtx, offsets.PublicKeyInstructionIndex, offsets.PublicKeyOffset, Secp256r1CompressedPubkeySerializedSize)
		if err != nil {
			return err
		}

		message, err := Secp256r1GetDataSlice(txCtx, offsets.MessageInstructionIndex, offsets.MessageDataOffset, uint64(offsets.MessageDataSize))
		if err != nil {
			return err
		}

		err = verifySecp256r1(pkBytes, message, signature)
		if err != nil {
			return err
		}
	}

	return nil
}
