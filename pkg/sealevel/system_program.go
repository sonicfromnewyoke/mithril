package sealevel

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"unicode/utf8"

	a "github.com/Overclock-Validator/mithril/pkg/addresses"
	"github.com/Overclock-Validator/mithril/pkg/cu"
	"github.com/Overclock-Validator/mithril/pkg/features"
	"github.com/Overclock-Validator/mithril/pkg/safemath"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

const SystemProgMaxPermittedDataLen = 10 * 1024 * 1024

const (
	SystemProgramInstrTypeCreateAccount = iota
	SystemProgramInstrTypeAssign
	SystemProgramInstrTypeTransfer
	SystemProgramInstrTypeCreateAccountWithSeed
	SystemProgramInstrTypeAdvanceNonceAccount
	SystemProgramInstrTypeWithdrawNonceAccount
	SystemProgramInstrTypeInitializeNonceAccount
	SystemProgramInstrTypeAuthorizeNonceAccount
	SystemProgramInstrTypeAllocate
	SystemProgramInstrTypeAllocateWithSeed
	SystemProgramInstrTypeAssignWithSeed
	SystemProgramInstrTypeTransferWithSeed
	SystemProgramInstrTypeUpgradeNonceAccount
	SystemProgramInstrTypeCreateAccountAllowPrefund
)

var (
	SystemProgErrAccountAlreadyInUse        = errors.New("SystemProgErrAccountAlreadyInUse")
	SystemProgErrInvalidAccountDataLength   = errors.New("SystemProgErrInvalidAccountDataLength")
	SystemProgErrResultWithNegativeLamports = errors.New("SystemProgErrResultWithNegativeLamports")
	SystemProgErrAddressWithSeedMismatch    = errors.New("SystemProgErrAddressWithSeedMismatch")
	SystemProgErrNonceNoRecentBlockhashes   = errors.New("SystemProgErrNonceNoRecentBlockhashes")
	SystemProgErrNonceBlockhashNotExpired   = errors.New("SystemProgErrNonceBlockhashNotExpired")
)

type SystemInstrCreateAccount struct {
	Lamports uint64
	Space    uint64
	Owner    solana.PublicKey
}

type SystemInstrAssign struct {
	Owner solana.PublicKey
}

type SystemInstrTransfer struct {
	Lamports uint64
}

type SystemInstrCreateAccountWithSeed struct {
	Base     solana.PublicKey
	Seed     string
	Lamports uint64
	Space    uint64
	Owner    solana.PublicKey
}

type SystemInstrWithdrawNonceAccount struct {
	Lamports uint64
}

type SystemInstrInitializeNonceAccount struct {
	Pubkey solana.PublicKey
}

type SystemInstrAuthorizeNonceAccount struct {
	Pubkey solana.PublicKey
}

type SystemInstrAllocate struct {
	Space uint64
}

type SystemInstrAllocateWithSeed struct {
	Base  solana.PublicKey
	Seed  string
	Space uint64
	Owner solana.PublicKey
}

type SystemInstrAssignWithSeed struct {
	Base  solana.PublicKey
	Seed  string
	Owner solana.PublicKey
}

type SystemInstrTransferWithSeed struct {
	Lamports  uint64
	FromSeed  string
	FromOwner solana.PublicKey
}

type SystemInstrCreateAccountAllowPrefund struct {
	Lamports uint64
	Space    uint64
	Owner    solana.PublicKey
}

type fromAndLamports struct {
	fromIdx  uint64
	lamports uint64
}

const (
	NonceVersionLegacy  = 0
	NonceVersionCurrent = 1
)

type NonceStateVersions struct {
	Type    uint32
	Legacy  NonceData
	Current NonceData
}

type NonceData struct {
	IsInitialized bool
	Authority     solana.PublicKey
	DurableNonce  [32]byte
	FeeCalculator FeeCalculator
}

func checkWithinDeserializationLimit(decoder *bin.Decoder) error {
	if decoder.Position() > 1232 {
		return InstrErrInvalidInstructionData
	} else {
		return nil
	}
}

func (instr *SystemInstrCreateAccount) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error

	instr.Lamports, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	instr.Space, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	pk, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Owner[:], pk)

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrCreateAccount) MarshalWithEncoder(encoder *bin.Encoder) error {
	err := encoder.WriteUint32(SystemProgramInstrTypeCreateAccount, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteUint64(instr.Lamports, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteUint64(instr.Space, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteBytes(instr.Owner[:], false)
	return err
}

func newCreateAccountInstruction(from solana.PublicKey, to solana.PublicKey, lamports uint64, space uint64, owner solana.PublicKey) *Instruction {
	var accountMetas []AccountMeta
	accountMetas = append(accountMetas, AccountMeta{Pubkey: from, IsSigner: true, IsWritable: true})
	accountMetas = append(accountMetas, AccountMeta{Pubkey: to, IsSigner: true, IsWritable: true})

	buf := new(bytes.Buffer)
	encoder := bin.NewBinEncoder(buf)

	createAcctInstr := SystemInstrCreateAccount{Lamports: lamports, Space: space, Owner: owner}
	err := createAcctInstr.MarshalWithEncoder(encoder)
	if err != nil {
		panic("shouldn't fail")
	}

	instr := &Instruction{Accounts: accountMetas, Data: buf.Bytes(), ProgramId: a.SystemProgramAddr}
	return instr
}

func newTransferInstruction(from solana.PublicKey, to solana.PublicKey, lamports uint64) *Instruction {
	var accountMetas []AccountMeta
	accountMetas = append(accountMetas, AccountMeta{Pubkey: from, IsSigner: true, IsWritable: true})
	accountMetas = append(accountMetas, AccountMeta{Pubkey: to, IsSigner: false, IsWritable: true})

	buf := new(bytes.Buffer)
	encoder := bin.NewBinEncoder(buf)

	txInstr := SystemInstrTransfer{Lamports: lamports}
	err := txInstr.MarshalWithEncoder(encoder)
	if err != nil {
		panic("shouldn't fail")
	}

	instr := &Instruction{Accounts: accountMetas, Data: buf.Bytes(), ProgramId: a.SystemProgramAddr}
	return instr
}

func newAllocateInstruction(pubkey solana.PublicKey, space uint64) *Instruction {
	var accountMetas []AccountMeta
	accountMetas = append(accountMetas, AccountMeta{Pubkey: pubkey, IsSigner: true, IsWritable: true})

	buf := new(bytes.Buffer)
	encoder := bin.NewBinEncoder(buf)

	allocInstr := SystemInstrAllocate{Space: space}
	err := allocInstr.MarshalWithEncoder(encoder)
	if err != nil {
		panic("shouldn't fail")
	}

	instr := &Instruction{Accounts: accountMetas, Data: buf.Bytes(), ProgramId: a.SystemProgramAddr}
	return instr
}

func newAssignInstruction(pubkey solana.PublicKey, owner solana.PublicKey) *Instruction {
	var accountMetas []AccountMeta
	accountMetas = append(accountMetas, AccountMeta{Pubkey: pubkey, IsSigner: true, IsWritable: true})

	buf := new(bytes.Buffer)
	encoder := bin.NewBinEncoder(buf)

	assignInstr := SystemInstrAssign{Owner: owner}
	err := assignInstr.MarshalWithEncoder(encoder)
	if err != nil {
		panic("shouldn't fail")
	}

	instr := &Instruction{Accounts: accountMetas, Data: buf.Bytes(), ProgramId: a.SystemProgramAddr}
	return instr
}

func (instr *SystemInstrAssign) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error

	pk, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Owner[:], pk)

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrAssign) MarshalWithEncoder(encoder *bin.Encoder) error {
	err := encoder.WriteUint32(SystemProgramInstrTypeAssign, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteBytes(instr.Owner[:], false)
	return err
}

func (instr *SystemInstrTransfer) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error

	instr.Lamports, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrTransfer) MarshalWithEncoder(encoder *bin.Encoder) error {
	err := encoder.WriteUint32(SystemProgramInstrTypeTransfer, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteUint64(instr.Lamports, bin.LE)
	return err
}

func (instr *SystemInstrCreateAccountWithSeed) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	base, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Base[:], base)

	instr.Seed, err = decoder.ReadRustString()
	if err != nil {
		return err
	}
	if !utf8.ValidString(instr.Seed) {
		return InstrErrInvalidInstructionData
	}

	instr.Lamports, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	instr.Space, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	owner, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Owner[:], owner)

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrWithdrawNonceAccount) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	instr.Lamports, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrInitializeNonceAccount) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	owner, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Pubkey[:], owner)
	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrAuthorizeNonceAccount) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	owner, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Pubkey[:], owner)
	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrAllocate) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	instr.Space, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrAllocate) MarshalWithEncoder(encoder *bin.Encoder) error {
	err := encoder.WriteUint32(SystemProgramInstrTypeAllocate, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteUint64(instr.Space, bin.LE)
	return err
}

func (instr *SystemInstrAllocateWithSeed) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	base, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Base[:], base)

	instr.Seed, err = decoder.ReadRustString()
	if err != nil {
		return err
	}
	if !utf8.ValidString(instr.Seed) {
		return InstrErrInvalidInstructionData
	}

	instr.Space, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	var owner []byte
	owner, err = decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Owner[:], owner)

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrAssignWithSeed) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	base, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Base[:], base)

	instr.Seed, err = decoder.ReadRustString()
	if err != nil {
		return err
	}
	if !utf8.ValidString(instr.Seed) {
		return InstrErrInvalidInstructionData
	}

	owner, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.Owner[:], owner)
	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrAssignWithSeed) MarshalWithEncoder(encoder *bin.Encoder) error {
	var err error

	err = encoder.WriteUint32(SystemProgramInstrTypeAssignWithSeed, bin.LE)
	if err != nil {
		return err
	}

	err = encoder.WriteBytes(instr.Base[:], false)
	if err != nil {
		return err
	}

	err = encoder.WriteRustString(instr.Seed)
	if err != nil {
		return err
	}

	err = encoder.WriteBytes(instr.Owner[:], false)
	return err
}

func (instr *SystemInstrTransferWithSeed) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	instr.Lamports, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	instr.FromSeed, err = decoder.ReadRustString()
	if err != nil {
		return err
	}
	if !utf8.ValidString(instr.FromSeed) {
		return InstrErrInvalidInstructionData
	}

	fromOwner, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}
	copy(instr.FromOwner[:], fromOwner)

	return checkWithinDeserializationLimit(decoder)
}

func (instr *SystemInstrCreateAccountAllowPrefund) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	instr.Lamports, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	instr.Space, err = decoder.ReadUint64(bin.LE)
	if err != nil {
		return err
	}

	owner, err := decoder.ReadBytes(solana.PublicKeyLength)
	if err != nil {
		return err
	}

	copy(instr.Owner[:], owner)
	return checkWithinDeserializationLimit(decoder)
}

func (nonceStateVersions *NonceStateVersions) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	nonceStateVersions.Type, err = decoder.ReadUint32(bin.LE)
	if err != nil {
		return err
	}

	switch nonceStateVersions.Type {
	case NonceVersionLegacy:
		{
			err = nonceStateVersions.Legacy.UnmarshalWithDecoder(decoder)
		}
	case NonceVersionCurrent:
		{
			err = nonceStateVersions.Current.UnmarshalWithDecoder(decoder)
		}
	default:
		err = InstrErrInvalidAccountData
	}

	return err
}

func (nonceStateVersions *NonceStateVersions) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)
	encoder := bin.NewBinEncoder(buf)

	err := encoder.WriteUint32(nonceStateVersions.Type, bin.LE)
	if err != nil {
		return nil, err
	}

	var nonceDataBytes []byte
	if nonceStateVersions.Type == NonceVersionLegacy {
		nonceDataBytes, err = nonceStateVersions.Legacy.Marshal()
		if err != nil {
			return nil, err
		}
	} else if nonceStateVersions.Type == NonceVersionCurrent {
		nonceDataBytes, err = nonceStateVersions.Current.Marshal()
		if err != nil {
			return nil, err
		}
	} else {
		panic("NonceStateVersions in an invalid state - programming error")
	}

	buf.Write(nonceDataBytes)

	return buf.Bytes(), nil
}

func UnmarshalNonceStateVersions(data []byte) (*NonceStateVersions, error) {
	decoder := bin.NewBinDecoder(data)

	nonceStateVersions := new(NonceStateVersions)
	err := nonceStateVersions.UnmarshalWithDecoder(decoder)
	if err != nil {
		return nil, InstrErrInvalidAccountData
	}

	return nonceStateVersions, nil
}

func (nonceStateVersions *NonceStateVersions) State() *NonceData {
	if nonceStateVersions.Type == NonceVersionLegacy {
		return &nonceStateVersions.Legacy
	} else if nonceStateVersions.Type == NonceVersionCurrent {
		return &nonceStateVersions.Current
	} else {
		panic("NonceStateVersions in an invalid state - programming error")
	}
}

func (nonceStateVersions *NonceStateVersions) IsUpgradeable() bool {
	if nonceStateVersions.Type == NonceVersionCurrent || !nonceStateVersions.State().IsInitialized {
		return false
	} else {
		return true
	}
}

func (nonceStateVersions *NonceStateVersions) Upgrade() bool {
	if nonceStateVersions.Type == NonceVersionCurrent {
		return false
	} else if nonceStateVersions.Type == NonceVersionLegacy {
		if !nonceStateVersions.Legacy.IsInitialized {
			return false
		}

		nonceStateVersions.Current = nonceStateVersions.Legacy
		nonceStateVersions.Type = NonceVersionCurrent
		nonceStateVersions.Current.DurableNonce = durableNonce(nonceStateVersions.Current.DurableNonce)
		nonceStateVersions.Legacy = NonceData{}

		return true
	} else {
		panic("invalid nonce state version - should be impossible")
	}
}

func (nonceStateVersions *NonceStateVersions) Deinitialize() {
	nonceStateVersions.Type = NonceVersionCurrent
	nonceStateVersions.Current = NonceData{}
	nonceStateVersions.Legacy = NonceData{}
}

func (nonceData *NonceData) UnmarshalWithDecoder(decoder *bin.Decoder) error {
	var err error
	isInitialized, err := decoder.ReadUint32(bin.LE)
	if err != nil {
		return err
	}

	if isInitialized != 0 && isInitialized != 1 {
		return InstrErrInvalidAccountData
	}

	nonceData.IsInitialized = isInitialized == 1

	if nonceData.IsInitialized {
		authority, err := decoder.ReadBytes(solana.PublicKeyLength)
		if err != nil {
			return err
		}
		nonceData.Authority = solana.PublicKeyFromBytes(authority)

		durableNonce, err := decoder.ReadBytes(32)
		if err != nil {
			return err
		}
		copy(nonceData.DurableNonce[:], durableNonce)

		lamportsPerSig, err := decoder.ReadUint64(bin.LE)
		if err != nil {
			return err
		}
		nonceData.FeeCalculator.LamportsPerSignature = lamportsPerSig
	}
	return nil
}

func (nonceData *NonceData) Marshal() ([]byte, error) {
	var err error

	buf := new(bytes.Buffer)
	encoder := bin.NewBinEncoder(buf)

	if !nonceData.IsInitialized {
		err = encoder.WriteUint32(0, bin.LE)
		if err != nil {
			return nil, err
		}
	} else {
		err = encoder.WriteUint32(1, bin.LE)
		if err != nil {
			return nil, err
		}
		err = encoder.WriteBytes(nonceData.Authority[:], false)
		if err != nil {
			return nil, err
		}
		err = encoder.WriteBytes(nonceData.DurableNonce[:], false)
		if err != nil {
			return nil, err
		}
		err = encoder.WriteUint64(nonceData.FeeCalculator.LamportsPerSignature, bin.LE)
		if err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func (nonceData *NonceData) IsSignerAuthority(signers []solana.PublicKey) bool {
	for _, signer := range signers {
		if nonceData.Authority == signer {
			return true
		}
	}
	return false
}

// systemAddress mirrors Agave system_processor's Address struct: an address
// that may or may not have been generated from a base key and a seed. The
// signer check runs against the base when one is present, and the struct's
// Rust derived-Debug rendering appears verbatim in Agave's ic_msg log lines.
type systemAddress struct {
	address solana.PublicKey
	base    *solana.PublicKey
}

// debugString renders the address byte-exactly like Rust's derived Debug for
// Agave's Address struct (`{:?}` in the ic_msg call sites), e.g.
// "Address { address: <base58>, base: None }".
func (addr systemAddress) debugString() string {
	if addr.base != nil {
		return fmt.Sprintf("Address { address: %s, base: Some(%s) }", addr.address, *addr.base)
	}
	return fmt.Sprintf("Address { address: %s, base: None }", addr.address)
}

func (addr systemAddress) isSigner(signers []solana.PublicKey) bool {
	key := addr.address
	if addr.base != nil {
		key = *addr.base
	}
	for _, signer := range signers {
		if key == signer {
			return true
		}
	}
	return false
}

func extractAddress(txCtx *TransactionCtx, instrCtx *InstructionCtx, instrAcctIdx uint64) (solana.PublicKey, error) {
	var addr solana.PublicKey
	var err error

	idx, err := instrCtx.IndexOfInstructionAccountInTransaction(instrAcctIdx)
	if err != nil {
		return addr, err
	}

	addr, err = txCtx.KeyOfAccountAtIndex(idx)
	return addr, err
}

func extractAddressWithSeed(execCtx *ExecutionCtx, instrCtx *InstructionCtx, instrAcctIdx uint64, base solana.PublicKey, seed string, owner solana.PublicKey) (systemAddress, error) {
	txCtx := execCtx.TransactionContext

	idx, err := instrCtx.IndexOfInstructionAccountInTransaction(instrAcctIdx)
	if err != nil {
		return systemAddress{}, err
	}

	addr, err := txCtx.KeyOfAccountAtIndex(idx)
	if err != nil {
		return systemAddress{}, err
	}

	addrWithSeed, err := ValidateAndCreateWithSeed(base, seed, owner)
	if err != nil {
		return systemAddress{}, err
	}

	if addr != addrWithSeed {
		execCtx.stableLog(fmt.Sprintf("Create: address %s does not match derived address %s", addr, addrWithSeed))
		return systemAddress{}, SystemProgErrAddressWithSeedMismatch
	}

	return systemAddress{address: addr, base: &base}, nil
}

func SystemProgramExecute(execCtx *ExecutionCtx) error {
	err := execCtx.ComputeMeter.Consume(cu.CUSystemProgramDefaultComputeUnits)
	if err != nil {
		return InstrErrComputationalBudgetExceeded
	}

	txCtx := execCtx.TransactionContext
	instrCtx, err := txCtx.CurrentInstructionCtx()
	if err != nil {
		return err
	}

	decoder := bin.NewBinDecoder(instrCtx.Data)

	instructionType, err := decoder.ReadUint32(bin.LE)
	if err != nil {
		return InstrErrInvalidInstructionData
	}

	signers, err := instrCtx.Signers(txCtx)
	if err != nil {
		return err
	}

	switch instructionType {

	case SystemProgramInstrTypeCreateAccount:
		{
			var createAccount SystemInstrCreateAccount
			err = createAccount.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(2)
			if err != nil {
				return err
			}

			var toAddr solana.PublicKey
			toAddr, err = extractAddress(txCtx, instrCtx, 1)
			if err != nil {
				return err
			}

			err = SystemProgramCreateAccount(execCtx, systemAddress{address: toAddr}, createAccount.Lamports, createAccount.Space, createAccount.Owner, signers)
		}

	case SystemProgramInstrTypeAssign:
		{
			var assign SystemInstrAssign
			err = assign.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			var addr solana.PublicKey
			addr, err = extractAddress(txCtx, instrCtx, 0)
			if err != nil {
				return err
			}

			err = SystemProgramAssign(execCtx, acct, systemAddress{address: addr}, assign.Owner, signers)
		}

	case SystemProgramInstrTypeTransfer:
		{
			var transfer SystemInstrTransfer
			err = transfer.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}
			err = instrCtx.CheckNumOfInstructionAccounts(2)
			if err != nil {
				return err
			}

			err = SystemProgramTransfer(execCtx, 0, 1, transfer.Lamports)
		}

	case SystemProgramInstrTypeCreateAccountWithSeed:
		{
			var createAcctWithSeed SystemInstrCreateAccountWithSeed
			err = createAcctWithSeed.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(2)
			if err != nil {
				return err
			}

			var toAddr systemAddress
			toAddr, err = extractAddressWithSeed(execCtx, instrCtx, 1, createAcctWithSeed.Base, createAcctWithSeed.Seed, createAcctWithSeed.Owner)
			if err != nil {
				return err
			}

			err = SystemProgramCreateAccount(execCtx, toAddr, createAcctWithSeed.Lamports, createAcctWithSeed.Space, createAcctWithSeed.Owner, signers)
		}

	case SystemProgramInstrTypeAdvanceNonceAccount:
		{
			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			err = CheckAcctForRecentBlockHashesSysvar(execCtx.TransactionContext, instrCtx, 1)
			if err != nil {
				return err
			}

			var recentBlockHashes SysvarRecentBlockhashes
			recentBlockHashes, err = ReadRecentBlockHashesSysvar(execCtx)
			if err != nil {
				return err
			}
			if len(recentBlockHashes) == 0 {
				execCtx.stableLog("Advance nonce account: recent blockhash list is empty")
				return SystemProgErrNonceNoRecentBlockhashes
			}

			err = SystemProgramAdvanceNonceAccount(execCtx, acct, signers, &recentBlockHashes)
		}

	case SystemProgramInstrTypeWithdrawNonceAccount:
		{
			var withdrawNonceAcct SystemInstrWithdrawNonceAccount
			err = withdrawNonceAcct.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(2)
			if err != nil {
				return err
			}

			err = CheckAcctForRecentBlockHashesSysvar(execCtx.TransactionContext, instrCtx, 2)
			if err != nil {
				return err
			}

			var recentBlockhashes SysvarRecentBlockhashes
			recentBlockhashes, err = ReadRecentBlockHashesSysvar(execCtx)
			if err != nil {
				return err
			}

			err = checkAcctForRentSysvar(txCtx, instrCtx, 3)
			if err != nil {
				return err
			}

			var rent SysvarRent
			rent, err = ReadRentSysvar(execCtx)
			if err != nil {
				return err
			}

			err = SystemProgramWithdrawNonceAccount(execCtx, instrCtx, 0, withdrawNonceAcct.Lamports, 1, &rent, signers, &recentBlockhashes)
		}

	case SystemProgramInstrTypeInitializeNonceAccount:
		{
			var initNonceAcct SystemInstrInitializeNonceAccount
			err = initNonceAcct.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			err = CheckAcctForRecentBlockHashesSysvar(execCtx.TransactionContext, instrCtx, 1)
			if err != nil {
				return err
			}

			var recentBlockHashes SysvarRecentBlockhashes
			recentBlockHashes, err = ReadRecentBlockHashesSysvar(execCtx)
			if err != nil {
				return err
			}
			if len(recentBlockHashes) == 0 {
				execCtx.stableLog("Initialize nonce account: recent blockhash list is empty")
				return SystemProgErrNonceNoRecentBlockhashes
			}

			err = checkAcctForRentSysvar(txCtx, instrCtx, 2)
			if err != nil {
				return err
			}

			var rent SysvarRent
			rent, err = ReadRentSysvar(execCtx)
			if err != nil {
				return err
			}

			err = SystemProgramInitializeNonceAccount(execCtx, acct, initNonceAcct.Pubkey, &rent, &recentBlockHashes)
		}

	case SystemProgramInstrTypeAuthorizeNonceAccount:
		{
			var authNonceAcct SystemInstrAuthorizeNonceAccount
			err = authNonceAcct.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			err = SystemProgramAuthorizeNonceAccount(execCtx, acct, authNonceAcct.Pubkey, signers)
		}

	case SystemProgramInstrTypeAllocate:
		{
			var allocate SystemInstrAllocate
			err = allocate.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}
			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			var addr solana.PublicKey
			addr, err = extractAddress(txCtx, instrCtx, 0)
			if err != nil {
				return err
			}
			err = SystemProgramAllocate(execCtx, acct, systemAddress{address: addr}, allocate.Space, signers)
		}

	case SystemProgramInstrTypeAllocateWithSeed:
		{
			var allocateWithSeed SystemInstrAllocateWithSeed
			err = allocateWithSeed.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			var addr systemAddress
			addr, err = extractAddressWithSeed(execCtx, instrCtx, 0, allocateWithSeed.Base, allocateWithSeed.Seed, allocateWithSeed.Owner)
			if err != nil {
				return err
			}
			err = SystemProgramAllocateAndAssign(execCtx, acct, addr, allocateWithSeed.Space, allocateWithSeed.Owner, signers)
		}

	case SystemProgramInstrTypeAssignWithSeed:
		{
			var assignWithSeed SystemInstrAssignWithSeed
			err = assignWithSeed.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			var addr systemAddress
			addr, err = extractAddressWithSeed(execCtx, instrCtx, 0, assignWithSeed.Base, assignWithSeed.Seed, assignWithSeed.Owner)
			if err != nil {
				return err
			}

			err = SystemProgramAssign(execCtx, acct, addr, assignWithSeed.Owner, signers)
		}

	case SystemProgramInstrTypeTransferWithSeed:
		{
			var transferWithSeed SystemInstrTransferWithSeed
			err = transferWithSeed.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			err = instrCtx.CheckNumOfInstructionAccounts(3)
			if err != nil {
				return err
			}

			err = SystemProgramTransferWithSeed(execCtx, 0, 1, transferWithSeed.FromSeed, transferWithSeed.FromOwner, 2, transferWithSeed.Lamports)

		}

	case SystemProgramInstrTypeUpgradeNonceAccount:
		{
			err = instrCtx.CheckNumOfInstructionAccounts(1)
			if err != nil {
				return err
			}

			var acct *BorrowedAccount
			acct, err = instrCtx.BorrowInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}
			defer acct.Drop()

			err = SystemProgramUpgradeNonceAccount(execCtx, acct)
		}

	case SystemProgramInstrTypeCreateAccountAllowPrefund:
		{
			var createAccountAllowPrefund SystemInstrCreateAccountAllowPrefund
			err = createAccountAllowPrefund.UnmarshalWithDecoder(decoder)
			if err != nil {
				return InstrErrInvalidInstructionData
			}

			if !execCtx.Features.IsActive(features.CreateAccountAllowPrefund) {
				return InstrErrInvalidInstructionData
			}

			var fromIdx *uint64
			var lamports *uint64

			if createAccountAllowPrefund.Lamports > 0 {
				err = instrCtx.CheckNumOfInstructionAccounts(2)
				if err != nil {
					return err
				}
				from := uint64(1)
				fromIdx = &from
				lamports = &createAccountAllowPrefund.Lamports
			} else {
				err = instrCtx.CheckNumOfInstructionAccounts(1)
				if err != nil {
					return err
				}
			}

			var toAddr solana.PublicKey
			toAddr, err = instrCtx.KeyOfInstructionAccount(txCtx, 0)
			if err != nil {
				return err
			}

			err = SystemProgramCreateAccountAllowPrefund(execCtx, instrCtx, 0, systemAddress{address: toAddr}, fromIdx, lamports, createAccountAllowPrefund.Space, createAccountAllowPrefund.Owner, signers)
		}

	default:
		{
			err = InstrErrInvalidInstructionData
		}
	}

	return err
}

func SystemProgramCreateAccount(execCtx *ExecutionCtx, toAddr systemAddress, lamports uint64, space uint64, owner solana.PublicKey, signers []solana.PublicKey) error {
	txCtx := execCtx.TransactionContext
	instrCtx, err := txCtx.CurrentInstructionCtx()
	if err != nil {
		return err
	}

	toAcct, err := instrCtx.BorrowInstructionAccount(txCtx, 1)
	if err != nil {
		return err
	}
	defer toAcct.Drop()

	if toAcct.Lamports() > 0 {
		execCtx.stableLog(fmt.Sprintf("Create Account: account %s already in use", toAddr.debugString()))
		return SystemProgErrAccountAlreadyInUse
	}

	err = SystemProgramAllocateAndAssign(execCtx, toAcct, toAddr, space, owner, signers)
	if err != nil {
		return err
	}
	toAcct.Drop()

	return SystemProgramTransfer(execCtx, 0, 1, lamports)
}

func SystemProgramAllocateAndAssign(execCtx *ExecutionCtx, toAcct *BorrowedAccount, toAddr systemAddress, space uint64, owner solana.PublicKey, signers []solana.PublicKey) error {
	err := SystemProgramAllocate(execCtx, toAcct, toAddr, space, signers)
	if err != nil {
		return err
	}

	return SystemProgramAssign(execCtx, toAcct, toAddr, owner, signers)
}

func SystemProgramAllocate(execCtx *ExecutionCtx, acct *BorrowedAccount, address systemAddress, space uint64, signers []solana.PublicKey) error {
	if !address.isSigner(signers) {
		execCtx.stableLog(fmt.Sprintf("Allocate: 'to' account %s must sign", address.debugString()))
		return InstrErrMissingRequiredSignature
	}

	if len(acct.Data()) != 0 || acct.Owner() != a.SystemProgramAddr {
		execCtx.stableLog(fmt.Sprintf("Allocate: account %s already in use", address.debugString()))
		return SystemProgErrAccountAlreadyInUse
	}

	if space > SystemProgMaxPermittedDataLen {
		execCtx.stableLog(fmt.Sprintf("Allocate: requested %d, max allowed %d", space, uint64(SystemProgMaxPermittedDataLen)))
		return SystemProgErrInvalidAccountDataLength
	}

	return acct.SetDataLength(space, execCtx.Features)
}

func SystemProgramCreateAccountAllowPrefund(execCtx *ExecutionCtx, instrCtx *InstructionCtx, toAcctIdx uint64, toAddr systemAddress, fromIdx *uint64, lamports *uint64, space uint64, owner solana.PublicKey, signers []solana.PublicKey) error {
	to, err := instrCtx.BorrowInstructionAccount(execCtx.TransactionContext, toAcctIdx)
	if err != nil {
		return err
	}

	err = SystemProgramAllocateAndAssign(execCtx, to, toAddr, space, owner, signers)
	if err != nil {
		to.Drop()
		return err
	}
	to.Drop()

	if lamports != nil && *lamports > 0 {
		err = SystemProgramTransfer(execCtx, *fromIdx, toAcctIdx, *lamports)
		if err != nil {
			return err
		}
	}

	return nil
}

func SystemProgramAssign(execCtx *ExecutionCtx, acct *BorrowedAccount, address systemAddress, owner solana.PublicKey, signers []solana.PublicKey) error {
	if acct.Owner() == owner {
		return nil
	}

	if !address.isSigner(signers) {
		execCtx.stableLog(fmt.Sprintf("Assign: account %s must sign", address.debugString()))
		return InstrErrMissingRequiredSignature
	}

	return acct.SetOwner(execCtx.Features, owner)
}

func SystemProgramTransfer(execCtx *ExecutionCtx, fromAcctIdx uint64, toAcctIdx uint64, lamports uint64) error {
	instrCtx, err := execCtx.TransactionContext.CurrentInstructionCtx()
	if err != nil {
		return err
	}

	isSigner, err := instrCtx.IsInstructionAccountSigner(fromAcctIdx)
	if err != nil {
		return err
	}

	if !isSigner {
		fromAddr, err := extractAddress(execCtx.TransactionContext, instrCtx, fromAcctIdx)
		if err != nil {
			return err
		}
		execCtx.stableLog(fmt.Sprintf("Transfer: `from` account %s must sign", fromAddr))
		return InstrErrMissingRequiredSignature
	}

	return transferInternal(execCtx, fromAcctIdx, toAcctIdx, lamports)
}

func SystemProgramTransferWithSeed(execCtx *ExecutionCtx, fromAcctIdx uint64, fromBaseAcctIdx uint64, fromSeed string, fromOwner solana.PublicKey, toAcctIdx uint64, lamports uint64) error {
	txCtx := execCtx.TransactionContext
	instrCtx, err := txCtx.CurrentInstructionCtx()
	if err != nil {
		return err
	}

	isSigner, err := instrCtx.IsInstructionAccountSigner(fromBaseAcctIdx)
	if err != nil {
		return err
	}
	if !isSigner {
		baseAddr, err := extractAddress(txCtx, instrCtx, fromBaseAcctIdx)
		if err != nil {
			return err
		}
		execCtx.stableLog(fmt.Sprintf("Transfer: 'from' account %s must sign", baseAddr))
		return InstrErrMissingRequiredSignature
	}

	baseAcctIdxInTx, err := instrCtx.IndexOfInstructionAccountInTransaction(fromBaseAcctIdx)
	if err != nil {
		return err
	}

	base, err := txCtx.KeyOfAccountAtIndex(baseAcctIdxInTx)
	if err != nil {
		return err
	}

	addrFromSeed, err := ValidateAndCreateWithSeed(base, fromSeed, fromOwner)
	if err != nil {
		return err
	}

	fromAddr, err := extractAddress(txCtx, instrCtx, fromAcctIdx)
	if err != nil {
		return err
	}

	if fromAddr != addrFromSeed {
		execCtx.stableLog(fmt.Sprintf("Transfer: 'from' address %s does not match derived address %s", fromAddr, addrFromSeed))
		return SystemProgErrAddressWithSeedMismatch
	}

	return transferInternal(execCtx, fromAcctIdx, toAcctIdx, lamports)
}

func transferInternal(execCtx *ExecutionCtx, fromAcctIdx uint64, toAcctIdx uint64, lamports uint64) error {
	txCtx := execCtx.TransactionContext
	instrCtx, err := txCtx.CurrentInstructionCtx()
	if err != nil {
		return err
	}

	from, err := instrCtx.BorrowInstructionAccount(txCtx, fromAcctIdx)
	if err != nil {
		return err
	}
	defer from.Drop()

	if len(from.Data()) != 0 {
		execCtx.stableLog("Transfer: `from` must not carry data")
		return InstrErrInvalidArgument
	}

	if lamports > from.Lamports() {
		execCtx.stableLog(fmt.Sprintf("Transfer: insufficient lamports %d, need %d", from.Lamports(), lamports))
		return SystemProgErrResultWithNegativeLamports
	}

	f := execCtx.Features
	err = from.CheckedSubLamports(lamports, f)
	if err != nil {
		return err
	}
	from.Drop()

	to, err := instrCtx.BorrowInstructionAccount(txCtx, toAcctIdx)
	if err != nil {
		return err
	}
	defer to.Drop()

	err = to.CheckedAddLamports(lamports, f)
	return err
}

func durableNonce(hash [32]byte) [32]byte {
	prefix := "DURABLE_NONCE"
	hasher := sha256.New()
	hasher.Write([]byte(prefix))
	hasher.Write(hash[:])
	sum := hasher.Sum(nil)

	var durableNonce [32]byte
	copy(durableNonce[:], sum)
	return durableNonce
}

func SystemProgramInitializeNonceAccount(execCtx *ExecutionCtx, acct *BorrowedAccount, nonceAuthority solana.PublicKey, rent *SysvarRent, recentBlockhashes *SysvarRecentBlockhashes) error {
	if !acct.IsWritable() {
		execCtx.stableLog(fmt.Sprintf("Initialize nonce account: Account %s must be writeable", acct.Key()))
		return InstrErrInvalidArgument
	}

	nonceStateVersions, err := UnmarshalNonceStateVersions(acct.Data())
	if err != nil {
		return err
	}

	if nonceStateVersions.State().IsInitialized {
		execCtx.stableLog(fmt.Sprintf("Initialize nonce account: Account %s state is invalid", acct.Key()))
		return InstrErrInvalidAccountData
	}

	minBalance := rent.MinimumBalance(uint64(len(acct.Data())))
	if acct.Lamports() < minBalance {
		execCtx.stableLog(fmt.Sprintf("Initialize nonce account: insufficient lamports %d, need %d", acct.Lamports(), minBalance))
		return InstrErrInsufficientFunds
	}

	rbh := execCtx.SlotCtx.LastBlockhash
	durableNonce := durableNonce(rbh)

	newNonceStateVersions := NonceStateVersions{Type: NonceVersionCurrent, Current: NonceData{
		IsInitialized: true,
		Authority:     nonceAuthority,
		DurableNonce:  durableNonce,
		FeeCalculator: FeeCalculator{LamportsPerSignature: execCtx.PrevLamportsPerSignature},
	}}

	newStateBytes, err := newNonceStateVersions.Marshal()
	if err != nil {
		return err
	}

	err = acct.SetState(execCtx.Features, newStateBytes)
	return err
}

func SystemProgramAuthorizeNonceAccount(execCtx *ExecutionCtx, acct *BorrowedAccount, nonceAuthority solana.PublicKey, signers []solana.PublicKey) error {
	if !acct.IsWritable() {
		execCtx.stableLog(fmt.Sprintf("Authorize nonce account: Account %s must be writeable", acct.Key()))
		return InstrErrInvalidArgument
	}

	nonceStateVersions, err := UnmarshalNonceStateVersions(acct.Data())
	if err != nil {
		return err
	}

	nonceData := nonceStateVersions.State()
	if !nonceData.IsInitialized {
		execCtx.stableLog(fmt.Sprintf("Authorize nonce account: Account %s state is invalid", acct.Key()))
		return InstrErrInvalidAccountData
	}

	if !nonceData.IsSignerAuthority(signers) {
		execCtx.stableLog(fmt.Sprintf("Authorize nonce account: Account %s must sign", nonceData.Authority))
		return InstrErrMissingRequiredSignature
	}

	nonceData.Authority = nonceAuthority

	newStateData, err := nonceStateVersions.Marshal()
	if err != nil {
		return err
	}
	return acct.SetState(execCtx.Features, newStateData)
}

func SystemProgramUpgradeNonceAccount(execCtx *ExecutionCtx, acct *BorrowedAccount) error {
	if acct.Owner() != a.SystemProgramAddr {
		return InstrErrInvalidAccountOwner
	}

	if !acct.IsWritable() {
		return InstrErrInvalidArgument
	}

	nonceStateVersions, err := UnmarshalNonceStateVersions(acct.Data())
	if err != nil {
		return err
	}

	upgradeable := nonceStateVersions.Upgrade()
	if !upgradeable {
		return InstrErrInvalidArgument
	}

	newStateData, err := nonceStateVersions.Marshal()
	if err != nil {
		return err
	}

	return acct.SetState(execCtx.Features, newStateData)
}

func SystemProgramWithdrawNonceAccount(execCtx *ExecutionCtx, instrCtx *InstructionCtx, fromAcctIdx uint64, lamports uint64, toAcctIdx uint64, rent *SysvarRent, signers []solana.PublicKey, recentBlockhashes *SysvarRecentBlockhashes) error {
	from, err := instrCtx.BorrowInstructionAccount(execCtx.TransactionContext, fromAcctIdx)
	if err != nil {
		return err
	}
	defer from.Drop()

	if !from.IsWritable() {
		execCtx.stableLog(fmt.Sprintf("Withdraw nonce account: Account %s must be writeable", from.Key()))
		return InstrErrInvalidArgument
	}

	nonceStateVersions, err := UnmarshalNonceStateVersions(from.Data())
	if err != nil {
		return err
	}

	var signer solana.PublicKey
	state := nonceStateVersions.State()

	if state.IsInitialized {
		signer = state.Authority
		if lamports == from.Lamports() {
			durableNonce := durableNonce(execCtx.SlotCtx.LastBlockhash)
			if durableNonce == state.DurableNonce {
				execCtx.stableLog("Withdraw nonce account: nonce can only advance once per slot")
				return SystemProgErrNonceBlockhashNotExpired
			}
			nonceStateVersions.Deinitialize()
			deinitNonceStateVersionsData, err := nonceStateVersions.Marshal()
			if err != nil {
				return err
			}
			err = from.SetState(execCtx.Features, deinitNonceStateVersionsData)
			if err != nil {
				return err
			}
		} else {
			minBalance := rent.MinimumBalance(uint64(len(from.Data())))
			amount, err := safemath.CheckedAddU64(lamports, minBalance)
			if err != nil {
				return InstrErrInsufficientFunds
			}
			if amount > from.Lamports() {
				execCtx.stableLog(fmt.Sprintf("Withdraw nonce account: insufficient lamports %d, need %d", from.Lamports(), amount))
				return InstrErrInsufficientFunds
			}
		}
	} else {
		if lamports > from.Lamports() {
			execCtx.stableLog(fmt.Sprintf("Withdraw nonce account: insufficient lamports %d, need %d", from.Lamports(), lamports))
			return InstrErrInsufficientFunds
		}
		signer = from.Key()
	}

	var isSigner bool
	for _, s := range signers {
		if s == signer {
			isSigner = true
			break
		}
	}

	if !isSigner {
		execCtx.stableLog(fmt.Sprintf("Withdraw nonce account: Account %s must sign", signer))
		return InstrErrMissingRequiredSignature
	}

	err = from.CheckedSubLamports(lamports, execCtx.Features)
	if err != nil {
		return err
	}
	from.Drop()

	to, err := instrCtx.BorrowInstructionAccount(execCtx.TransactionContext, toAcctIdx)
	if err != nil {
		return err
	}
	defer to.Drop()

	err = to.CheckedAddLamports(lamports, execCtx.Features)
	if err != nil {
		return err
	}

	return nil
}

func SystemProgramAdvanceNonceAccount(execCtx *ExecutionCtx, acct *BorrowedAccount, signers []solana.PublicKey, recentBlockhashes *SysvarRecentBlockhashes) error {
	if !acct.IsWritable() {
		execCtx.stableLog(fmt.Sprintf("Advance nonce account: Account %s must be writeable", acct.Key()))
		return InstrErrInvalidArgument
	}

	nonceStateVersions, err := UnmarshalNonceStateVersions(acct.Data())
	if err != nil {
		return err
	}

	state := nonceStateVersions.State()

	if !state.IsInitialized {
		execCtx.stableLog(fmt.Sprintf("Advance nonce account: Account %s state is invalid", acct.Key()))
		return InstrErrInvalidAccountData
	}

	if !state.IsSignerAuthority(signers) {
		execCtx.stableLog(fmt.Sprintf("Advance nonce account: Account %s must be a signer", state.Authority))
		return InstrErrMissingRequiredSignature
	}

	rbh := execCtx.SlotCtx.LastBlockhash
	nextDurableNonce := durableNonce(rbh)
	if state.DurableNonce == nextDurableNonce {
		execCtx.stableLog("Advance nonce account: nonce can only advance once per slot")
		return SystemProgErrNonceBlockhashNotExpired
	}

	if nonceStateVersions.Type == NonceVersionCurrent {
		state.DurableNonce = nextDurableNonce
		state.FeeCalculator.LamportsPerSignature = execCtx.PrevLamportsPerSignature
	} else {
		nonceStateVersions.Upgrade()
		upgradedState := nonceStateVersions.State()
		upgradedState.DurableNonce = nextDurableNonce
		upgradedState.FeeCalculator.LamportsPerSignature = execCtx.PrevLamportsPerSignature
	}

	newData, err := nonceStateVersions.Marshal()
	if err != nil {
		return err
	}

	err = acct.SetState(execCtx.Features, newData)
	if err != nil {
		return err
	}

	execCtx.TransactionContext.NonceAcctAdvanced = true
	return nil
}
