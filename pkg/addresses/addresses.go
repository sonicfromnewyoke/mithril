package addresses

import "github.com/sonicfromnewyoke/mithril/pkg/base58"

const BpfLoaderUpgradeableAddrStr = "BPFLoaderUpgradeab1e11111111111111111111111"

var BpfLoaderUpgradeableAddr = base58.MustDecodeFromString(BpfLoaderUpgradeableAddrStr)

const BpfLoader2AddrStr = "BPFLoader2111111111111111111111111111111111"

var BpfLoader2Addr = base58.MustDecodeFromString(BpfLoader2AddrStr)

const BpfLoaderDeprecatedAddrStr = "BPFLoader1111111111111111111111111111111111"

var BpfLoaderDeprecatedAddr = base58.MustDecodeFromString(BpfLoaderDeprecatedAddrStr)

const LoaderV4AddrStr = "LoaderV411111111111111111111111111111111111"

var LoaderV4Addr = base58.MustDecodeFromString(LoaderV4AddrStr)

const NativeLoaderAddrStr = "NativeLoader1111111111111111111111111111111"

var NativeLoaderAddr = base58.MustDecodeFromString(NativeLoaderAddrStr)

const ConfigProgramAddrStr = "Config1111111111111111111111111111111111111"

var ConfigProgramAddr = base58.MustDecodeFromString(ConfigProgramAddrStr)

const Secp256kPrecompileAddrStr = "KeccakSecp256k11111111111111111111111111111"

var Secp256kPrecompileAddr = base58.MustDecodeFromString(Secp256kPrecompileAddrStr)

const Secp256r1PrecompileAddrStr = "Secp256r1SigVerify1111111111111111111111111"

var Secp256r1PrecompileAddr = base58.MustDecodeFromString(Secp256r1PrecompileAddrStr)

const Ed25519PrecompileAddrStr = "Ed25519SigVerify111111111111111111111111111"

var Ed25519PrecompileAddr = base58.MustDecodeFromString(Ed25519PrecompileAddrStr)

const StakeProgramAddrStr = "Stake11111111111111111111111111111111111111"

var StakeProgramAddr = base58.MustDecodeFromString(StakeProgramAddrStr)

const StakeProgramConfigAddrStr = "StakeConfig11111111111111111111111111111111"

var StakeProgramConfigAddr = base58.MustDecodeFromString(StakeProgramConfigAddrStr)

const VoteProgramAddrStr = "Vote111111111111111111111111111111111111111"

var VoteProgramAddr = base58.MustDecodeFromString(VoteProgramAddrStr)

const SystemProgramAddrStr = "11111111111111111111111111111111"

var SystemProgramAddr = base58.MustDecodeFromString(SystemProgramAddrStr)

const AddressLookupTableProgramAddrStr = "AddressLookupTab1e1111111111111111111111111"

var AddressLookupTableAddr = base58.MustDecodeFromString(AddressLookupTableProgramAddrStr)

const ComputeBudgetProgramAddrStr = "ComputeBudget111111111111111111111111111111"

var ComputeBudgetProgramAddr = base58.MustDecodeFromString(ComputeBudgetProgramAddrStr)

const IncineratorAddrStr = "1nc1nerator11111111111111111111111111111111"

var IncineratorAddr = base58.MustDecodeFromString(IncineratorAddrStr)

const ZkTokenProofProgramAddrStr = "ZkTokenProof1111111111111111111111111111111"

var ZkTokenProofProgramAddr = base58.MustDecodeFromString(ZkTokenProofProgramAddrStr)

const ZkElgamalProofProgramAddrStr = "ZkE1Gama1Proof11111111111111111111111111111"

var ZkElgamalProofProgramAddr = base58.MustDecodeFromString(ZkElgamalProofProgramAddrStr)

const SysvarOwnerStr = "Sysvar1111111111111111111111111111111111111"

var SysvarOwnerAddr = base58.MustDecodeFromString(SysvarOwnerStr)

const SysvarRewardsAddrStr = "SysvarRewards111111111111111111111111111111"

var SysvarRewardsAddr = base58.MustDecodeFromString(SysvarRewardsAddrStr)

const FeatureAddrStr = "Feature111111111111111111111111111111111111"

var FeatureAddr = base58.MustDecodeFromString(FeatureAddrStr)
