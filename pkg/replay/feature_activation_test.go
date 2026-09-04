package replay

import (
	"math"
	"testing"

	"github.com/sonicfromnewyoke/mithril/fixtures"
	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	a "github.com/sonicfromnewyoke/mithril/pkg/addresses"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPubkey(seed byte) solana.PublicKey {
	var raw [32]byte
	raw[0] = seed
	return solana.PublicKeyFromBytes(raw[:])
}

func TestBuildCoreBpfProgramUpgrade(t *testing.T) {
	slot := uint64(1234)
	upgradeAuthority := testPubkey(9)
	rentSysvar := &sealevel.SysvarRent{
		LamportsPerUint8Year: 1,
		ExemptionThreshold:   2,
	}

	programDataAddress, err := deriveUpgradeableLoaderProgramDataAddress(a.StakeProgramAddr)
	require.NoError(t, err)

	targetProgramStateBytes, err := marshalUpgradeableLoaderStateSized(&sealevel.UpgradeableLoaderState{
		Type: sealevel.UpgradeableLoaderStateTypeProgram,
		Program: sealevel.UpgradeableLoaderStateProgram{
			ProgramDataAddress: programDataAddress,
		},
	}, upgradeableLoaderProgramStateSize)
	require.NoError(t, err)

	targetProgram := &accounts.Account{
		Key:        a.StakeProgramAddr,
		Lamports:   rentSysvar.MinimumBalance(upgradeableLoaderProgramStateSize),
		Data:       targetProgramStateBytes,
		Owner:      a.BpfLoaderUpgradeableAddr,
		Executable: true,
		RentEpoch:  math.MaxUint64,
	}

	oldProgramDataStateBytes, err := marshalUpgradeableLoaderStateSized(&sealevel.UpgradeableLoaderState{
		Type: sealevel.UpgradeableLoaderStateTypeProgramData,
		ProgramData: sealevel.UpgradeableLoaderStateProgramData{
			Slot:                    55,
			UpgradeAuthorityAddress: &upgradeAuthority,
		},
	}, upgradeableLoaderProgramDataMetadataSize)
	require.NoError(t, err)

	oldElf := []byte{1, 2, 3, 4}
	targetProgramDataBytes := append(oldProgramDataStateBytes, oldElf...)
	targetProgramData := &accounts.Account{
		Key:       programDataAddress,
		Lamports:  rentSysvar.MinimumBalance(uint64(len(targetProgramDataBytes))),
		Data:      targetProgramDataBytes,
		Owner:     a.BpfLoaderUpgradeableAddr,
		RentEpoch: math.MaxUint64,
	}

	newElf := fixtures.Load(t, "sbpf", "noop_aligned.so")
	sourceBufferStateBytes, err := marshalUpgradeableLoaderStateSized(&sealevel.UpgradeableLoaderState{
		Type: sealevel.UpgradeableLoaderStateTypeBuffer,
		Buffer: sealevel.UpgradeableLoaderStateBuffer{
			AuthorityAddress: &upgradeAuthority,
		},
	}, upgradeableLoaderBufferMetadataSize)
	require.NoError(t, err)

	sourceBufferData := append(sourceBufferStateBytes, newElf...)
	sourceBuffer := &accounts.Account{
		Key:       stakeProgramV5Buffer,
		Lamports:  rentSysvar.MinimumBalance(uint64(len(sourceBufferData))),
		Data:      sourceBufferData,
		Owner:     a.BpfLoaderUpgradeableAddr,
		RentEpoch: math.MaxUint64,
	}

	migration, err := buildCoreBpfProgramUpgrade(slot, rentSysvar, targetProgram, targetProgramData, sourceBuffer, "Stake")
	require.NoError(t, err)

	require.Len(t, migration.modifiedAccts, 2)
	upgradedProgramData := migration.modifiedAccts[0]
	assert.Equal(t, programDataAddress, upgradedProgramData.Key)
	assert.Equal(t, a.BpfLoaderUpgradeableAddr, upgradedProgramData.Owner)
	assert.False(t, upgradedProgramData.Executable)
	assert.Equal(t, rentSysvar.MinimumBalance(uint64(upgradeableLoaderProgramDataMetadataSize+len(newElf))), upgradedProgramData.Lamports)

	upgradedProgramDataState, err := sealevel.UnmarshalUpgradeableLoaderState(upgradedProgramData.Data)
	require.NoError(t, err)
	assert.Equal(t, uint32(sealevel.UpgradeableLoaderStateTypeProgramData), upgradedProgramDataState.Type)
	assert.Equal(t, slot, upgradedProgramDataState.ProgramData.Slot)
	require.NotNil(t, upgradedProgramDataState.ProgramData.UpgradeAuthorityAddress)
	assert.Equal(t, upgradeAuthority, *upgradedProgramDataState.ProgramData.UpgradeAuthorityAddress)
	assert.Equal(t, newElf, upgradedProgramData.Data[upgradeableLoaderProgramDataMetadataSize:])

	clearedBuffer := migration.modifiedAccts[1]
	assert.Equal(t, solana.PublicKeyFromBytes(stakeProgramV5Buffer[:]), clearedBuffer.Key)
	assert.Zero(t, clearedBuffer.Lamports)
	assert.Empty(t, clearedBuffer.Data)
	assert.False(t, clearedBuffer.Executable)

	require.Len(t, migration.parentAccts, 2)
	assert.Equal(t, targetProgramData.Data, migration.parentAccts[0].Data)
	assert.Equal(t, sourceBuffer.Data, migration.parentAccts[1].Data)
	assert.Equal(t, targetProgramData.Lamports+sourceBuffer.Lamports, migration.lamportsToBurn)
	assert.Equal(t, upgradedProgramData.Lamports, migration.lamportsToFund)
}

func TestBuildCoreBpfProgramUpgrade_UpgradeAuthorityMismatch(t *testing.T) {
	slot := uint64(1234)
	rentSysvar := &sealevel.SysvarRent{
		LamportsPerUint8Year: 1,
		ExemptionThreshold:   2,
	}

	programDataAddress, err := deriveUpgradeableLoaderProgramDataAddress(a.StakeProgramAddr)
	require.NoError(t, err)

	targetProgramStateBytes, err := marshalUpgradeableLoaderStateSized(&sealevel.UpgradeableLoaderState{
		Type: sealevel.UpgradeableLoaderStateTypeProgram,
		Program: sealevel.UpgradeableLoaderStateProgram{
			ProgramDataAddress: programDataAddress,
		},
	}, upgradeableLoaderProgramStateSize)
	require.NoError(t, err)

	targetProgram := &accounts.Account{
		Key:        a.StakeProgramAddr,
		Lamports:   rentSysvar.MinimumBalance(upgradeableLoaderProgramStateSize),
		Data:       targetProgramStateBytes,
		Owner:      a.BpfLoaderUpgradeableAddr,
		Executable: true,
		RentEpoch:  math.MaxUint64,
	}

	targetAuthority := testPubkey(1)
	targetProgramDataStateBytes, err := marshalUpgradeableLoaderStateSized(&sealevel.UpgradeableLoaderState{
		Type: sealevel.UpgradeableLoaderStateTypeProgramData,
		ProgramData: sealevel.UpgradeableLoaderStateProgramData{
			Slot:                    55,
			UpgradeAuthorityAddress: &targetAuthority,
		},
	}, upgradeableLoaderProgramDataMetadataSize)
	require.NoError(t, err)

	targetProgramData := &accounts.Account{
		Key:       programDataAddress,
		Lamports:  rentSysvar.MinimumBalance(uint64(len(targetProgramDataStateBytes))),
		Data:      targetProgramDataStateBytes,
		Owner:     a.BpfLoaderUpgradeableAddr,
		RentEpoch: math.MaxUint64,
	}

	bufferAuthority := testPubkey(2)
	sourceBufferStateBytes, err := marshalUpgradeableLoaderStateSized(&sealevel.UpgradeableLoaderState{
		Type: sealevel.UpgradeableLoaderStateTypeBuffer,
		Buffer: sealevel.UpgradeableLoaderStateBuffer{
			AuthorityAddress: &bufferAuthority,
		},
	}, upgradeableLoaderBufferMetadataSize)
	require.NoError(t, err)

	sourceBuffer := &accounts.Account{
		Key:       stakeProgramV5Buffer,
		Lamports:  rentSysvar.MinimumBalance(uint64(len(sourceBufferStateBytes))),
		Data:      sourceBufferStateBytes,
		Owner:     a.BpfLoaderUpgradeableAddr,
		RentEpoch: math.MaxUint64,
	}

	_, err = buildCoreBpfProgramUpgrade(slot, rentSysvar, targetProgram, targetProgramData, sourceBuffer, "Stake")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upgrade authority mismatch")
}
