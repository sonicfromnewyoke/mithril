package accountsdb

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/util"
	"github.com/gagliardetto/solana-go"
)

type AppendVecAccount struct {
	WriteVersion uint64
	DataLen      uint64
	Pubkey       solana.PublicKey
	Lamports     uint64
	RentEpoch    uint64
	Owner        solana.PublicKey
	Executable   bool
	Padding      [7]byte
	Hash         [32]byte
	Data         []byte
}

const (
	hdrLen        = 136
	dataLenOffset = 8
	pubkeyOffset  = 16
	ownerOffset   = 64
)

type appendVecParser struct {
	Buf      []byte
	FileSize uint64
	Offset   uint64

	FileId uint64
	Slot   uint64
}

func (parser *appendVecParser) ParseNextAcct(pk *solana.PublicKey, a *AccountIndexEntry) error {
	if parser.Offset+hdrLen > parser.FileSize {
		return fmt.Errorf("overflow")
	}

	dataLen := binary.LittleEndian.Uint64(parser.Buf[parser.Offset+dataLenOffset : parser.Offset+dataLenOffset+8])

	*pk = solana.PublicKeyFromBytes(parser.Buf[parser.Offset+pubkeyOffset : parser.Offset+pubkeyOffset+32])
	a.Slot = parser.Slot
	a.FileId = parser.FileId
	a.Offset = parser.Offset

	parser.Offset += hdrLen

	if parser.Offset+dataLen > parser.FileSize {
		return fmt.Errorf("overflow")
	}

	parser.Offset += util.AlignUp(dataLen, 8)

	return nil
}

// ParseNextAcctWithOwner parses the next account from the appendvec, extracting
// pubkey, index entry, and owner. Used for building stake pubkey index during
// snapshot processing.
func (parser *appendVecParser) ParseNextAcctWithOwner(pk *solana.PublicKey, a *AccountIndexEntry, owner *solana.PublicKey) error {
	if parser.Offset+hdrLen > parser.FileSize {
		return fmt.Errorf("overflow")
	}

	dataLen := binary.LittleEndian.Uint64(parser.Buf[parser.Offset+dataLenOffset : parser.Offset+dataLenOffset+8])

	*pk = solana.PublicKeyFromBytes(parser.Buf[parser.Offset+pubkeyOffset : parser.Offset+pubkeyOffset+32])
	*owner = solana.PublicKeyFromBytes(parser.Buf[parser.Offset+ownerOffset : parser.Offset+ownerOffset+32])
	a.Slot = parser.Slot
	a.FileId = parser.FileId
	a.Offset = parser.Offset

	parser.Offset += hdrLen

	if parser.Offset+dataLen > parser.FileSize {
		return fmt.Errorf("overflow")
	}

	parser.Offset += util.AlignUp(dataLen, 8)

	return nil
}

// GetAppendVecDataLen reads only the bytes necessary to determine the length
// of the append vec. Should be kept in sync with
// (*AppendVecAccount).Unmarshal.
func GetAppendVecDataLen(f *os.File, offset uint64) (uint64, error) {
	var hdrBytes [8]byte
	_, err := f.ReadAt(hdrBytes[:], int64(offset+8))
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(hdrBytes[:]), nil
}

func (acct *AppendVecAccount) Unmarshal(buf io.Reader) error {
	var err error
	var hdrBytes [hdrLen]byte
	_, err = buf.Read(hdrBytes[:])
	if err != nil {
		return err
	}

	acct.WriteVersion = binary.LittleEndian.Uint64(hdrBytes[:8])
	acct.DataLen = binary.LittleEndian.Uint64(hdrBytes[8:16])
	copy(acct.Pubkey[:], hdrBytes[16:48])
	acct.Lamports = binary.LittleEndian.Uint64(hdrBytes[48:56])
	acct.RentEpoch = binary.LittleEndian.Uint64(hdrBytes[56:64])
	copy(acct.Owner[:], hdrBytes[64:96])
	acct.Executable = hdrBytes[96] != 0
	copy(acct.Padding[:], hdrBytes[97:104])
	copy(acct.Hash[:], hdrBytes[104:136])

	acct.Data = make([]byte, acct.DataLen)
	_, err = buf.Read(acct.Data)

	return err
}

var padding [2048]byte

func (acct *AppendVecAccount) Marshal(buf io.Writer) error {
	_, err := acct.MarshalReturningLength(buf)
	return err
}

func (acct *AppendVecAccount) MarshalReturningLength(buf io.Writer) (int, error) {
	l := 0
	var err error
	var hdrBytes [hdrLen]byte

	binary.LittleEndian.PutUint64(hdrBytes[:8], acct.WriteVersion)
	binary.LittleEndian.PutUint64(hdrBytes[8:16], acct.DataLen)
	copy(hdrBytes[16:48], acct.Pubkey[:])
	binary.LittleEndian.PutUint64(hdrBytes[48:56], acct.Lamports)
	binary.LittleEndian.PutUint64(hdrBytes[56:64], acct.RentEpoch)
	copy(hdrBytes[64:96], acct.Owner[:])

	if acct.Executable {
		hdrBytes[96] = 1
	} else {
		hdrBytes[96] = 0
	}

	copy(hdrBytes[97:104], acct.Padding[:])
	copy(hdrBytes[104:136], acct.Hash[:])

	n, err := buf.Write(hdrBytes[:])
	l += n
	if err != nil {
		return l, err
	}

	n, err = buf.Write(acct.Data)
	l += n
	if err != nil {
		return l, err
	}

	numPaddingBytes := util.AlignUp(acct.DataLen, 8) - acct.DataLen
	n, err = buf.Write(padding[:numPaddingBytes])
	l += n
	if err != nil {
		return l, err
	} else if n != int(numPaddingBytes) {
		return l, fmt.Errorf("number of padding bytes written was %d rather than %d", n, numPaddingBytes)
	}

	return l, nil
}

func (appendVecAcct *AppendVecAccount) ToAccount() *accounts.Account {
	acct := &accounts.Account{Key: appendVecAcct.Pubkey, Lamports: appendVecAcct.Lamports,
		RentEpoch: appendVecAcct.RentEpoch, Owner: appendVecAcct.Owner, Executable: appendVecAcct.Executable,
		Data: appendVecAcct.Data}

	return acct
}

func unmarshalAcctFromAppendVecAcctHeader(buf io.Reader) (*accounts.Account, error) {
	var appendVecAcct AppendVecAccount
	err := appendVecAcct.Unmarshal(buf)
	if err != nil {
		return nil, err
	}

	return appendVecAcct.ToAccount(), nil
}
