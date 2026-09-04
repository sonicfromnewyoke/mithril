package fees

import (
	"fmt"
	"math"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/accountsdb"
	a "github.com/sonicfromnewyoke/mithril/pkg/addresses"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/safemath"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/Overclock-Validator/wide"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

var microLamportsPerLamport = wide.Uint128FromUint64(1000000)
var microLamportsPerLamportMinus1 = wide.Uint128FromUint64(1000000 - 1)

func calculatePriorityFee(computeBudgetLimits *sealevel.ComputeBudgetLimits) uint64 {
	computeUnitPrice := wide.Uint128FromUint64(computeBudgetLimits.ComputeUnitPrice)
	computeUnitLimit := wide.Uint128FromUint64(uint64(computeBudgetLimits.ComputeUnitLimit))

	microLamportFee := computeUnitPrice.Mul(computeUnitLimit)
	fee := microLamportFee.Add(microLamportsPerLamportMinus1).Div(microLamportsPerLamport)

	var priorityFee uint64
	if fee.IsUint64() {
		priorityFee = fee.Uint64()
	} else {
		priorityFee = math.MaxUint64
	}

	return priorityFee
}

// There are currently two aspects of the tx fee cost model on Solana
// 1) fee per signature (5k lamports/sig)
// 2) prioritization fees set via a SetComputeUnitPrice instruction

const feePayerIdx = 0

type TxFeeInfo struct {
	ExecutionFee uint64
	PriorityFee  uint64
	TotalFee     uint64
}

type TxFeeInfoAccumulator struct {
	ExecutionFees uint64
	PriorityFees  uint64
	TotalFees     uint64
}

func (txFeeAccumulator *TxFeeInfoAccumulator) Add(txFeeInfo *TxFeeInfo) {
	var err error

	txFeeAccumulator.TotalFees, err = safemath.CheckedAddU64(txFeeAccumulator.TotalFees, txFeeInfo.TotalFee)
	if err != nil {
		panic("overflow in accumulating total tx fees - should be impossible")
	}

	txFeeAccumulator.PriorityFees, err = safemath.CheckedAddU64(txFeeAccumulator.PriorityFees, txFeeInfo.PriorityFee)
	if err != nil {
		panic("overflow in accumulating priority fees - should be impossible")
	}

	txFeeAccumulator.ExecutionFees, err = safemath.CheckedAddU64(txFeeAccumulator.ExecutionFees, txFeeInfo.ExecutionFee)
	if err != nil {
		panic("overflow in accumulating execution fees - should be impossible")
	}
}

func CalculateTxFees(tx *solana.Transaction, instrs []sealevel.Instruction, computeBudgetLimits *sealevel.ComputeBudgetLimits, f *features.Features) *TxFeeInfo {
	numSignatures := uint64(tx.Message.Header.NumRequiredSignatures)
	secp256r1PrecompiledEnabled := f.IsActive(features.EnableSecp256r1Precompile)

	// have to pay fees per signatures to these precompiles as well
	for _, instr := range instrs {
		if instr.ProgramId == a.Secp256kPrecompileAddr || instr.ProgramId == a.Ed25519PrecompileAddr ||
			(instr.ProgramId == a.Secp256r1PrecompileAddr && secp256r1PrecompiledEnabled) {
			if len(instr.Data) != 0 {
				numSignatures += uint64(instr.Data[0])
			}
		}
	}

	// basic tx fee. 5000 lamports per signature.
	baseTxFee := numSignatures * 5000

	// prioritization fees
	var priorityFee uint64
	if computeBudgetLimits.ComputeUnitPrice != 0 {
		priorityFee = calculatePriorityFee(computeBudgetLimits)
	}

	totalTxFee := safemath.SaturatingAddU64(baseTxFee, priorityFee)
	return &TxFeeInfo{ExecutionFee: baseTxFee, PriorityFee: priorityFee, TotalFee: totalTxFee}
}

// TODO: implement new fee model
func CalculateAndDeductTxFees(tx *solana.Transaction, txMeta *rpc.TransactionMeta, instrs []sealevel.Instruction, transactionAccts *sealevel.TransactionAccounts, computeBudgetLimits *sealevel.ComputeBudgetLimits, f *features.Features, isSimulation bool) (*TxFeeInfo, uint64, error) {
	feePayerAcct, err := transactionAccts.GetAccount(feePayerIdx)
	if err != nil {
		if isSimulation {
			// RPC simulate must not crash the validator on user-submitted
			// input; surface the error to the client instead.
			return nil, 0, err
		}
		// Block-replay invariant: sanitize + loader guarantee feePayer at
		// AccountKeys[0] is loadable. Reaching here means our local state
		// or sanitize is broken — keep the loud signal.
		panic(fmt.Sprintf("CalculateAndDeductTxFees: feePayer GetAccount(0) failed: %v", err))
	}
	////mlog.Log.Debugf("feePayerAcct=%+v", feePayerAcct)

	defer transactionAccts.Unlock(feePayerIdx)

	numSignatures := uint64(tx.Message.Header.NumRequiredSignatures)
	secp256r1PrecompiledEnabled := f.IsActive(features.EnableSecp256r1Precompile)

	// have to pay fees per signatures to these precompiles as well
	for _, instr := range instrs {
		if instr.ProgramId == a.Secp256kPrecompileAddr || instr.ProgramId == a.Ed25519PrecompileAddr ||
			(instr.ProgramId == a.Secp256r1PrecompileAddr && secp256r1PrecompiledEnabled) {
			if len(instr.Data) != 0 {
				numSignatures += uint64(instr.Data[0])
			}
		}
	}

	// basic tx fee. 5000 lamports per signature.
	baseTxFee := numSignatures * 5000

	// prioritization fees
	var priorityFee uint64
	if computeBudgetLimits.ComputeUnitPrice != 0 {
		priorityFee = calculatePriorityFee(computeBudgetLimits)
	}

	totalTxFee := safemath.SaturatingAddU64(baseTxFee, priorityFee)
	feeInfo := &TxFeeInfo{ExecutionFee: baseTxFee, PriorityFee: priorityFee, TotalFee: totalTxFee}

	if feePayerAcct.Lamports < totalTxFee {
		return feeInfo, 0, sealevel.InstrErrInsufficientFunds
	}
	////mlog.Log.Debugf("feePayerAcct.Lamports=%d totalTxFee=%d", feePayerAcct.Lamports, totalTxFee)

	feePayerAcct, err = transactionAccts.Touch(feePayerIdx)
	if err != nil {
		return feeInfo, 0, err
	}
	feePayerAcct.Lamports -= totalTxFee

	return feeInfo, feePayerAcct.Lamports, nil
}

func DistributeTxFeesToSlotLeader(acctsDb *accountsdb.AccountsDb, slotCtx *sealevel.SlotCtx, leader solana.PublicKey, txFeeAccumulator *TxFeeInfoAccumulator) uint64 {
	var feesToBurn uint64
	var feesToLeader uint64

	if slotCtx.Features.IsActive(features.RewardFullPriorityFee) {
		halfFee := txFeeAccumulator.ExecutionFees / 2
		feesToLeader = safemath.SaturatingAddU64(txFeeAccumulator.PriorityFees, txFeeAccumulator.ExecutionFees-halfFee)
		feesToBurn = halfFee
	} else {
		feesToBurn = txFeeAccumulator.TotalFees / 2
		feesToLeader = txFeeAccumulator.TotalFees - feesToBurn
	}

	var leaderAcct *accounts.Account
	var err error

	leaderAcct, err = slotCtx.GetAccount(leader)
	if err != nil {
		// if leader didn't appear at all in the block, then retrieve its latest state from
		// accountsdb, and also add it to the parent accts object
		leaderAcct, err = acctsDb.GetAccount(slotCtx.Slot, leader)
		if err != nil {
			panic(fmt.Sprintf("unable to get leader acct %s from both slotCtx and accountsdb", leader))
		}
		slotCtx.ParentAccts.SetAccountWithoutLock(leader, leaderAcct.Clone())
	}

	leaderAcct.Lamports, err = safemath.CheckedAddU64(leaderAcct.Lamports, feesToLeader)
	if err != nil {
		panic("overflow when adding reward to slot leader balance")
	}

	err = slotCtx.SetAccount(leader, leaderAcct)
	if err != nil {
		panic(fmt.Sprintf("failed to SetAccount for leader acct %s when distributing tx fees", leader))
	}

	//mlog.Log.Debugf("calculated fees for leader: %d, post-balance: %d (%s)", feesToLeader, leaderAcct.Lamports, leader)

	return feesToBurn
}
