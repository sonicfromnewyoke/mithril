package conformance

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Overclock-Validator/mithril/pkg/accounts"
	"github.com/Overclock-Validator/mithril/pkg/accountsdb"
	"github.com/Overclock-Validator/mithril/pkg/cu"
	"github.com/Overclock-Validator/mithril/pkg/features"
	"github.com/Overclock-Validator/mithril/pkg/fees"
	"github.com/Overclock-Validator/mithril/pkg/migration"
	"github.com/Overclock-Validator/mithril/pkg/rent"
	"github.com/Overclock-Validator/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"google.golang.org/protobuf/proto"
)

// fixtureToSolTx converts a TxnFixtureV2 protobuf to a gagliardetto solana.Transaction.
func fixtureToSolTx(fixture *TxnFixtureV2) (*solana.Transaction, error) {
	msg := fixture.Input.Tx.Message
	if msg.Header == nil {
		return nil, fmt.Errorf("message header is nil")
	}

	tx := &solana.Transaction{}
	tx.Message.Header = solana.MessageHeader{
		NumRequiredSignatures:       uint8(msg.Header.NumRequiredSignatures),
		NumReadonlySignedAccounts:   uint8(msg.Header.NumReadonlySignedAccounts),
		NumReadonlyUnsignedAccounts: uint8(msg.Header.NumReadonlyUnsignedAccounts),
	}

	for _, key := range msg.AccountKeys {
		tx.Message.AccountKeys = append(tx.Message.AccountKeys, solana.PublicKeyFromBytes(key))
	}

	if len(msg.RecentBlockhash) == 32 {
		copy(tx.Message.RecentBlockhash[:], msg.RecentBlockhash)
	}

	for _, ci := range msg.Instructions {
		accts := make([]uint16, len(ci.Accounts))
		for i, a := range ci.Accounts {
			accts[i] = uint16(a)
		}
		tx.Message.Instructions = append(tx.Message.Instructions, solana.CompiledInstruction{
			ProgramIDIndex: uint16(ci.ProgramIdIndex),
			Accounts:       accts,
			Data:           ci.Data,
		})
	}

	for _, sig := range fixture.Input.Tx.Signatures {
		var s solana.Signature
		copy(s[:], sig)
		tx.Signatures = append(tx.Signatures, s)
	}

	// ensure at least one signature for fee payer
	if len(tx.Signatures) == 0 {
		tx.Signatures = make([]solana.Signature, max(1, msg.Header.NumRequiredSignatures))
	}

	return tx, nil
}

// parseFeatures builds a Features set from the fixture's bank features.
func parseFeatures(fixture *TxnFixtureV2) *features.Features {
	f := features.NewFeaturesDefault()
	if fixture.Input.Bank == nil || fixture.Input.Bank.Features == nil {
		return f
	}
	for _, ftr := range fixture.Input.Bank.Features.Features {
		for _, featureGate := range features.AllFeatureGates {
			if binary.LittleEndian.Uint64(featureGate.Address[:8]) == ftr {
				f.EnableFeature(featureGate, 0)
			}
		}
	}
	return f
}

// setupSlotCtx creates a SlotCtx from fixture data, populating accounts and bank state.
func setupSlotCtx(fixture *TxnFixtureV2, f *features.Features) *sealevel.SlotCtx {
	slotAccts := accounts.NewMemAccounts()
	for _, acctState := range fixture.Input.AccountSharedData {
		acct := accounts.Account{
			Key:        solana.PublicKeyFromBytes(acctState.Address),
			Lamports:   acctState.Lamports,
			Data:       acctState.Data,
			Executable: acctState.Executable,
			RentEpoch:  math.MaxUint64,
		}
		copy(acct.Owner[:], acctState.Owner)
		pk := [32]byte(acct.Key)
		slotAccts.SetAccount(&pk, &acct)
	}

	adb := &accountsdb.AccountsDb{}
	adb.InitCaches()

	slotCtx := &sealevel.SlotCtx{
		Accounts:        slotAccts,
		AccountsDb:      adb,
		Features:        f,
		AcctMapsMu:      &sync.Mutex{},
		ModifiedAccts:   make(map[solana.PublicKey]bool),
		WritableAccts:   make(map[solana.PublicKey]bool),
		VoteTimestampMu: &sync.Mutex{},
		VoteTimestamps:  make(map[solana.PublicKey]sealevel.BlockTimestamp),
		VoteAccts:       make(map[solana.PublicKey]uint64),
	}

	// set up fee rate governor from fixture bank
	slotCtx.FeeRateGovernor = &sealevel.FeeRateGovernor{
		LamportsPerSignature:     5000,
		PrevLamportsPerSignature: 5000,
	}
	if fixture.Input.Bank != nil {
		if fixture.Input.Bank.FeeRateGovernor != nil {
			frg := fixture.Input.Bank.FeeRateGovernor
			slotCtx.FeeRateGovernor.TargetLamportsPerSignature = frg.TargetLamportsPerSignature
			slotCtx.FeeRateGovernor.TargetSignaturesPerSlot = frg.TargetSignaturesPerSlot
			slotCtx.FeeRateGovernor.MinLamportsPerSignature = frg.MinLamportsPerSignature
			slotCtx.FeeRateGovernor.MaxLamportsPerSignature = frg.MaxLamportsPerSignature
			slotCtx.FeeRateGovernor.BurnPercent = byte(frg.BurnPercent)
		}

		// set up blockhash queue — the last entry is the most recent blockhash
		if len(fixture.Input.Bank.BlockhashQueue) > 0 {
			lastEntry := fixture.Input.Bank.BlockhashQueue[len(fixture.Input.Bank.BlockhashQueue)-1]
			if len(lastEntry.Blockhash) == 32 {
				copy(slotCtx.LastBlockhash[:], lastEntry.Blockhash)
			}
			slotCtx.FeeRateGovernor.PrevLamportsPerSignature = lastEntry.LamportsPerSignature
			if slotCtx.FeeRateGovernor.PrevLamportsPerSignature == 0 {
				slotCtx.FeeRateGovernor.PrevLamportsPerSignature = 5000
			}

			// build RecentBlockhashes sysvar from the queue
			rbh := make(sealevel.SysvarRecentBlockhashes, 0, len(fixture.Input.Bank.BlockhashQueue))
			for _, entry := range fixture.Input.Bank.BlockhashQueue {
				if len(entry.Blockhash) == 32 {
					var bh solana.Hash
					copy(bh[:], entry.Blockhash)
					rbh = append(rbh, sealevel.RecentBlockHashesEntry{
						Blockhash:     bh,
						FeeCalculator: sealevel.FeeCalculator{LamportsPerSignature: entry.LamportsPerSignature},
					})
				}
			}
			sealevel.SysvarCache.RecentBlockHashes.Sysvar = &rbh

			// set LatestEvictedBlockhash (the 151st-oldest blockhash)
			if len(fixture.Input.Bank.BlockhashQueue) > 0 {
				oldest := fixture.Input.Bank.BlockhashQueue[0]
				if len(oldest.Blockhash) == 32 {
					copy(slotCtx.LatestEvictedBlockhash[:], oldest.Blockhash)
				}
			}
		}

		slotCtx.TotalEpochStake = fixture.Input.Bank.TotalEpochStake
	}

	return slotCtx
}

// sanitizeTransaction checks basic structural validity of a transaction,
// mirroring Agave/FD's sanitization checks that happen before execution.
func sanitizeTransaction(tx *solana.Transaction) error {
	msg := &tx.Message
	numKeys := len(msg.AccountKeys)
	numSigs := int(msg.Header.NumRequiredSignatures)
	numReadonlySigned := int(msg.Header.NumReadonlySignedAccounts)
	numReadonlyUnsigned := int(msg.Header.NumReadonlyUnsignedAccounts)

	// signature count must match header
	if len(tx.Signatures) != numSigs {
		return fmt.Errorf("TxErrSanitizeFailure: sig count %d != required %d", len(tx.Signatures), numSigs)
	}

	// must have at least one signature
	if numSigs == 0 {
		return fmt.Errorf("TxErrSanitizeFailure: no required signatures")
	}

	// must have enough account keys for all signers
	if numKeys < numSigs {
		return fmt.Errorf("TxErrSanitizeFailure: not enough keys for signers")
	}

	// header counts must be consistent
	if numReadonlySigned >= numSigs {
		return fmt.Errorf("TxErrSanitizeFailure: readonly signed >= required sigs")
	}
	if numReadonlyUnsigned >= numKeys-numSigs {
		// all unsigned accounts are readonly — fee payer must be writable
		// this is only invalid if there ARE unsigned accounts
		if numKeys > numSigs && numReadonlyUnsigned >= numKeys-numSigs {
			return fmt.Errorf("TxErrSanitizeFailure: readonly unsigned >= unsigned keys")
		}
	}

	// check for duplicate account keys
	seen := make(map[solana.PublicKey]bool, numKeys)
	for _, key := range msg.AccountKeys {
		if seen[key] {
			return fmt.Errorf("TxErrSanitizeFailure: duplicate account key %s", key)
		}
		seen[key] = true
	}

	// validate instruction indices
	for _, instr := range msg.Instructions {
		if int(instr.ProgramIDIndex) >= numKeys {
			return fmt.Errorf("TxErrSanitizeFailure: program index %d >= %d keys", instr.ProgramIDIndex, numKeys)
		}
		for _, acctIdx := range instr.Accounts {
			if int(acctIdx) >= numKeys {
				return fmt.Errorf("TxErrSanitizeFailure: account index %d >= %d keys", acctIdx, numKeys)
			}
		}
		// program id cannot be the fee payer (index 0) — it must not be a signer-writable
		// Actually in Agave, programs at signer indices are demoted to non-writable
	}

	return nil
}

// processTransactionConformance mirrors replay.ProcessTransaction but without
// signature verification, divergence panics, or txMeta checks.
func processTransactionConformance(slotCtx *sealevel.SlotCtx, tx *solana.Transaction) (txErr error) {
	f := slotCtx.Features

	if f.IsActive(features.StaticInstructionLimit) {
		if len(tx.Message.Instructions) > 64 {
			return fmt.Errorf("TxErrSanitizeFailure")
		}
	}

	// build instructions and account metas
	instrs := make([]sealevel.Instruction, 0, len(tx.Message.Instructions))
	acctMetasPerInstr := make([][]sealevel.AccountMeta, 0, len(tx.Message.Instructions))
	for _, compiledInstr := range tx.Message.Instructions {
		if int(compiledInstr.ProgramIDIndex) >= len(tx.Message.AccountKeys) {
			return sealevel.InstrErrMissingAccount
		}
		// validate all account indices before calling ResolveInstructionAccounts
		for _, acctIdx := range compiledInstr.Accounts {
			if int(acctIdx) >= len(tx.Message.AccountKeys) {
				return sealevel.InstrErrMissingAccount
			}
		}
		programId := tx.Message.AccountKeys[compiledInstr.ProgramIDIndex]

		ams, err := compiledInstr.ResolveInstructionAccounts(&tx.Message)
		if err != nil {
			return err
		}

		acctMetas := make([]sealevel.AccountMeta, 0, len(ams))
		for _, am := range ams {
			acctMeta := sealevel.AccountMeta{
				Pubkey:     am.PublicKey,
				IsSigner:   am.IsSigner,
				IsWritable: sealevel.IsWritable(&sealevel.AccountMeta{Pubkey: am.PublicKey, IsSigner: am.IsSigner, IsWritable: am.IsWritable}, f),
			}
			acctMetas = append(acctMetas, acctMeta)
		}

		instr := sealevel.Instruction{Accounts: acctMetas, ProgramId: programId, Data: compiledInstr.Data}
		instrs = append(instrs, instr)
		acctMetasPerInstr = append(acctMetasPerInstr, acctMetas)
	}

	// compute budget
	computeBudgetLimits, err := sealevel.ComputeBudgetExecuteInstructions(instrs, f)
	if err != nil {
		return err
	}

	// blockhash validation
	if !sealevel.IsTransactionAgeValid(tx, instrs, slotCtx) {
		return fmt.Errorf("TxErrInvalidBlockhash")
	}

	// build instructions sysvar account
	instrsAcct := sealevel.MakeInstructionsSysvarAccount(instrs)

	// load and build transaction accounts from SlotCtx
	txAcctMetas, err := tx.AccountMetaList()
	if err != nil {
		return err
	}

	acctsForTx := make([]accounts.Account, 0, len(txAcctMetas))
	var loadedBytesAccumulator uint32
	for _, acctMeta := range txAcctMetas {
		var acct *accounts.Account

		if acctMeta.PublicKey == sealevel.SysvarInstructionsAddr {
			acct = instrsAcct
		} else {
			acct, err = slotCtx.GetAccount(acctMeta.PublicKey)
			if err != nil {
				// account not found — create empty
				acct = &accounts.Account{Key: acctMeta.PublicKey, RentEpoch: math.MaxUint64}
			}
		}

		if acctMeta.PublicKey != sealevel.SysvarInstructionsAddr {
			loadedBytesAccumulator += uint32(len(acct.Data))
			if loadedBytesAccumulator > computeBudgetLimits.LoadedAccountBytes {
				return fmt.Errorf("TxErrMaxLoadedAccountsDataSizeExceeded")
			}
		}

		// validate program accounts
		if isProgramIndex(tx, acctMeta.PublicKey) {
			if !acct.Executable && acct.Owner != sealevel.SysvarInstructionsAddr {
				return fmt.Errorf("TxErrInvalidProgramForExecution")
			}
		}

		acctsForTx = append(acctsForTx, *acct)
	}

	transactionAccts := sealevel.NewTransactionAccounts(acctsForTx)

	// populate AcctMetas — needed by rent state checks
	convertedAcctMetas := make([]*sealevel.AccountMeta, 0, len(txAcctMetas))
	for _, am := range txAcctMetas {
		convertedAcctMetas = append(convertedAcctMetas, &sealevel.AccountMeta{
			Pubkey:     am.PublicKey,
			IsSigner:   am.IsSigner,
			IsWritable: sealevel.IsWritable(&sealevel.AccountMeta{Pubkey: am.PublicKey, IsSigner: am.IsSigner, IsWritable: am.IsWritable}, f),
		})
	}
	transactionAccts.AcctMetas = convertedAcctMetas

	var log sealevel.LogRecorder
	txCtx := sealevel.NewTransactionCtx(*transactionAccts, 5, 64)
	txCtx.AllInstructions = instrs
	txCtx.ComputeBudgetLimits = computeBudgetLimits
	if len(tx.Signatures) > 0 {
		txCtx.Signature = tx.Signatures[0]
	}

	execCtx := &sealevel.ExecutionCtx{
		Log:                    &log,
		TransactionContext:     txCtx,
		ComputeMeter:           cu.NewComputeMeter(uint64(computeBudgetLimits.ComputeUnitLimit)),
		SlotCtx:                slotCtx,
		Features:               *f,
		Accounts:               slotCtx.Accounts,
		PrevLamportsPerSignature: slotCtx.FeeRateGovernor.PrevLamportsPerSignature,
		ModifiedVoteStates:     make(map[solana.PublicKey]*sealevel.VoteStateVersions),
	}

	// fee deduction
	_, _, err = fees.CalculateAndDeductTxFees(tx, nil, instrs, &execCtx.TransactionContext.Accounts, computeBudgetLimits, f)
	if err != nil {
		return err
	}

	// rent state setup
	rentSysvar, err := sealevel.ReadRentSysvar(execCtx)
	if err != nil {
		// use defaults if not available
		rentSysvar = sealevel.SysvarRent{LamportsPerUint8Year: 3480, ExemptionThreshold: 2.0, BurnPercent: 50}
	}
	execCtx.TransactionContext.Rent = rentSysvar

	rent.MaybeSetRentExemptRentEpochMax(slotCtx, &rentSysvar, &execCtx.Features, &execCtx.TransactionContext.Accounts)
	preTxRentStates := rent.NewRentStateInfo(&rentSysvar, execCtx.TransactionContext, &execCtx.Features)

	// instruction execution loop
	var instrErr error
	for instrIdx, instr := range tx.Message.Instructions {
		// fixup instructions sysvar
		instructionsSysvarIdx, idxErr := execCtx.TransactionContext.IndexOfAccount(sealevel.SysvarInstructionsAddr)
		if idxErr == nil {
			instructionsAcct, acctErr := execCtx.TransactionContext.AccountAtIndex(instructionsSysvarIdx)
			if acctErr == nil && len(instructionsAcct.Data) >= 2 {
				lastIndex := len(instructionsAcct.Data) - 2
				binary.LittleEndian.PutUint16(instructionsAcct.Data[lastIndex:], uint16(instrIdx))
			}
		}

		acctMetas := acctMetasPerInstr[instrIdx]
		instructionAccts := sealevel.InstructionAcctsFromAccountMetas(acctMetas, *transactionAccts)

		programId := tx.Message.AccountKeys[instr.ProgramIDIndex]
		migratingCus, isMigrating := migration.IsMigratingProgramAndGetCUs(programId)
		if isMigrating {
			err = execCtx.ComputeMeter.Consume(migratingCus)
			if err != nil {
				instrErr = err
				break
			}
			execCtx.ComputeMeter.Disable()
		}

		err = execCtx.ProcessInstruction(instr.Data, instructionAccts, []uint64{uint64(instr.ProgramIDIndex)})
		if err != nil {
			instrErr = err
			break
		}

		if isMigrating {
			execCtx.ComputeMeter.Enable()
		}
	}

	// post-tx rent state verification
	postTxRentStates := rent.NewRentStateInfo(&rentSysvar, execCtx.TransactionContext, &execCtx.Features)
	rentStateErr := rent.VerifyRentStateChanges(preTxRentStates, postTxRentStates, execCtx.TransactionContext)

	if instrErr != nil {
		return instrErr
	}
	if rentStateErr != nil {
		return rentStateErr
	}

	return nil
}

func isProgramIndex(tx *solana.Transaction, pubkey solana.PublicKey) bool {
	for _, instr := range tx.Message.Instructions {
		if int(instr.ProgramIDIndex) < len(tx.Message.AccountKeys) && tx.Message.AccountKeys[instr.ProgramIDIndex] == pubkey {
			return true
		}
	}
	return false
}

func txnResultIsExpected(fixture *TxnFixtureV2, txErr error) bool {
	output := fixture.Output
	if txErr == nil && output.IsOk {
		return true
	}
	if txErr == nil && !output.IsOk {
		return false
	}
	if txErr != nil && output.IsOk {
		return false
	}
	// both errored — just check agreement on success/failure,
	// not specific error codes (matching FD's consensus_txn_diff_effects)
	return true
}

func txnAccountStatesMatch(fixture *TxnFixtureV2, slotCtx *sealevel.SlotCtx, txErr error) bool {
	expectedAccts := fixture.Output.ModifiedAccounts
	if len(expectedAccts) == 0 {
		return true
	}

	for _, expectedAcct := range expectedAccts {
		expectedKey := solana.PublicKeyFromBytes(expectedAcct.Address)
		pk := [32]byte(expectedKey)
		acct, err := slotCtx.Accounts.GetAccount(&pk)
		if err != nil {
			fmt.Printf("account %s not found in slot accounts\n", expectedKey)
			return false
		}

		if expectedAcct.Lamports != acct.Lamports {
			fmt.Printf("account %s lamports mismatch: expected=%d, got=%d\n", expectedKey, expectedAcct.Lamports, acct.Lamports)
			return false
		}
		if expectedAcct.Executable != acct.Executable {
			fmt.Printf("account %s executable mismatch\n", expectedKey)
			return false
		}
		if solana.PublicKeyFromBytes(expectedAcct.Owner) != solana.PublicKeyFromBytes(acct.Owner[:]) {
			fmt.Printf("account %s owner mismatch\n", expectedKey)
			return false
		}
		if !bytes.Equal(expectedAcct.Data, acct.Data) {
			fmt.Printf("account %s data mismatch: expected=%d bytes, got=%d bytes\n", expectedKey, len(expectedAcct.Data), len(acct.Data))
			return false
		}
	}

	return true
}

func TestConformance_Txn(t *testing.T) {
	basePath := "test-vectors/txn/fixtures"

	entries, err := os.ReadDir(basePath)
	if err != nil {
		t.Skipf("test-vectors not available: %v", err)
	}

	var fixturePaths []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".fix") {
			fixturePaths = append(fixturePaths, filepath.Join(basePath, entry.Name()))
		}
	}

	if len(fixturePaths) == 0 {
		t.Skip("no .fix fixtures found")
	}

	t.Logf("Found %d txn fixtures", len(fixturePaths))

	var (
		total          int
		passPass       int
		failFail       int
		falsePass      int
		falseFail      int
		panics         int
		parseErrors    int
		skipped        int
		acctStateMatch int
		acctStateTotal int
	)

	var failures []string
	var panicFixtures []string

	for _, fixturePath := range fixturePaths {
		total++
		name := filepath.Base(fixturePath)

		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Errorf("%s: read error: %v", name, err)
			continue
		}

		fixture := &TxnFixtureV2{}
		if err := proto.Unmarshal(data, fixture); err != nil {
			parseErrors++
			continue
		}

		if fixture.Input == nil || fixture.Output == nil ||
			fixture.Input.Tx == nil || fixture.Input.Tx.Message == nil {
			parseErrors++
			continue
		}

		fixtureExpectsSuccess := fixture.Output.IsOk

		var txErr error
		var didPanic bool

		func() {
			defer func() {
				if r := recover(); r != nil {
					didPanic = true
					panics++
					panicFixtures = append(panicFixtures, fmt.Sprintf("PANIC %s: %v", name, r))
				}
			}()

			sealevel.ResetSysvarCache()

			f := parseFeatures(fixture)

			tx, err := fixtureToSolTx(fixture)
			if err != nil {
				txErr = err
				return
			}

			// run sanitization checks first
			if err := sanitizeTransaction(tx); err != nil {
				txErr = err
				return
			}

			// skip execution if no account keys (degenerate fixture)
			if len(tx.Message.AccountKeys) == 0 {
				txErr = fmt.Errorf("TxErrSanitizeFailure: no account keys")
				return
			}

			slotCtx := setupSlotCtx(fixture, f)
			writeSysvarDefaults(slotCtx, f, fixture)

			txErr = processTransactionConformance(slotCtx, tx)

			// check account states on success
			if txErr == nil && fixtureExpectsSuccess {
				acctStateTotal++
				if txnAccountStatesMatch(fixture, slotCtx, txErr) {
					acctStateMatch++
				} else {
					failures = append(failures, fmt.Sprintf("ACCT_STATE_MISMATCH %s", name))
				}
			}
		}()

		if didPanic {
			continue
		}

		mithrilSucceeded := txErr == nil

		if mithrilSucceeded && fixtureExpectsSuccess {
			passPass++
		} else if !mithrilSucceeded && !fixtureExpectsSuccess {
			failFail++
		} else if mithrilSucceeded && !fixtureExpectsSuccess {
			falsePass++
			failures = append(failures, fmt.Sprintf("FALSE_PASS %s: succeeded but fixture expects failure (status=%d)", name, fixture.Output.Status))
		} else {
			falseFail++
			failures = append(failures, fmt.Sprintf("FALSE_FAIL %s: %v (fixture expects success)", name, txErr))
		}
	}

	sort.Strings(failures)

	t.Logf("\n=== Txn Conformance Results ===")
	t.Logf("Total fixtures:     %d", total)
	t.Logf("Parse errors:       %d", parseErrors)
	t.Logf("Skipped:            %d", skipped)
	t.Logf("Both pass:          %d", passPass)
	t.Logf("Both fail:          %d", failFail)
	t.Logf("False pass (bad):   %d (mithril succeeds, fixture rejects)", falsePass)
	t.Logf("False fail (bad):   %d (mithril rejects, fixture succeeds)", falseFail)
	t.Logf("Panics (crash bug): %d", panics)
	t.Logf("Acct state match:   %d / %d", acctStateMatch, acctStateTotal)

	if len(panicFixtures) > 0 {
		t.Logf("\n=== PANICS (crash bugs - highest priority) ===")
		for _, p := range panicFixtures {
			t.Logf("  %s", p)
		}
	}

	if len(failures) > 0 {
		t.Logf("\n=== First 50 failures ===")
		limit := 50
		if len(failures) < limit {
			limit = len(failures)
		}
		for _, f := range failures[:limit] {
			t.Logf("  %s", f)
		}
	}

	agree := passPass + failFail
	disagree := falsePass + falseFail
	if agree+disagree > 0 {
		passRate := float64(agree) / float64(agree+disagree) * 100
		t.Logf("\nConformance rate: %.1f%% (%d/%d)", passRate, agree, agree+disagree)
	}

	if panics > 0 {
		t.Errorf("CRITICAL: %d fixtures caused panics", panics)
	}
	if disagree > 0 {
		t.Logf("WARNING: %d disagreements found", disagree)
	}
}

// writeSysvarDefaults writes default sysvars to slot accounts, then overrides
// with fixture account data where available.
func writeSysvarDefaults(slotCtx *sealevel.SlotCtx, f *features.Features, fixture *TxnFixtureV2) {
	accts := slotCtx.Accounts

	// default rent
	{
		var defaultRent sealevel.SysvarRent
		if fixture.Input.Bank != nil && fixture.Input.Bank.Rent != nil {
			r := fixture.Input.Bank.Rent
			defaultRent.LamportsPerUint8Year = r.LamportsPerByteYear
			defaultRent.ExemptionThreshold = r.ExemptionThreshold
			defaultRent.BurnPercent = byte(r.BurnPercent)
		} else {
			defaultRent.LamportsPerUint8Year = 3480
			defaultRent.ExemptionThreshold = 2.0
			defaultRent.BurnPercent = 50
		}
		rentAcct := accounts.Account{Lamports: 1}
		accts.SetAccount(&sealevel.SysvarRentAddr, &rentAcct)
		sealevel.WriteRentSysvar(&accts, defaultRent)
	}
	// default clock
	{
		var clock sealevel.SysvarClock
		clock.Slot = slotCtx.Slot
		clockAcct := accounts.Account{Lamports: 1}
		accts.SetAccount(&sealevel.SysvarClockAddr, &clockAcct)
		sealevel.WriteClockSysvar(&accts, clock)
	}
	// default epoch schedule
	{
		var es sealevel.SysvarEpochSchedule
		if fixture.Input.Bank != nil && fixture.Input.Bank.EpochSchedule != nil {
			e := fixture.Input.Bank.EpochSchedule
			es.SlotsPerEpoch = e.SlotsPerEpoch
			es.LeaderScheduleSlotOffset = e.LeaderScheduleSlotOffset
			es.Warmup = e.Warmup
			es.FirstNormalEpoch = e.FirstNormalEpoch
			es.FirstNormalSlot = e.FirstNormalSlot
		} else {
			es = sealevel.SysvarEpochSchedule{
				SlotsPerEpoch: 432000, LeaderScheduleSlotOffset: 432000,
				Warmup: true, FirstNormalEpoch: 14, FirstNormalSlot: 524256,
			}
		}
		esAcct := accounts.Account{Lamports: 1}
		accts.SetAccount(&sealevel.SysvarEpochScheduleAddr, &esAcct)
		sealevel.WriteEpochScheduleSysvar(&accts, es)
	}

	// override with fixture account data for sysvars already in shared_data
	sysvarAddrs := [][32]byte{
		sealevel.SysvarClockAddr, sealevel.SysvarRentAddr,
		sealevel.SysvarSlotHashesAddr, sealevel.SysvarStakeHistoryAddr,
		sealevel.SysvarEpochScheduleAddr, sealevel.SysvarEpochRewardsAddr,
		sealevel.SysvarRecentBlockHashesAddr,
	}
	for _, acctState := range fixture.Input.AccountSharedData {
		pk := solana.PublicKeyFromBytes(acctState.Address)
		for idx, sa := range sysvarAddrs {
			if pk == sa {
				acct := accounts.Account{
					Key: pk, Lamports: acctState.Lamports,
					Data: acctState.Data, Executable: acctState.Executable,
				}
				copy(acct.Owner[:], acctState.Owner)
				accts.SetAccount(&sysvarAddrs[idx], &acct)
				break
			}
		}
	}
}

func TestConformance_Txn_Single_Testcase(t *testing.T) {
	basePath := "test-vectors/txn/fixtures"
	fn := "000e1b660e98199b93bad05140f0d0d106c2f61c_265678.fix"

	data, err := os.ReadFile(filepath.Join(basePath, fn))
	if err != nil {
		t.Fatalf("Error reading file: %v", err)
	}

	fixture := &TxnFixtureV2{}
	if err := proto.Unmarshal(data, fixture); err != nil {
		t.Fatalf("Failed to parse fixture: %v", err)
	}

	if fixture.Input == nil || fixture.Input.Tx == nil || fixture.Input.Tx.Message == nil {
		t.Fatal("fixture has no input transaction")
	}

	msg := fixture.Input.Tx.Message
	t.Logf("num account keys: %d, num instructions: %d, num shared_data: %d",
		len(msg.AccountKeys), len(msg.Instructions), len(fixture.Input.AccountSharedData))

	if fixture.Output != nil {
		t.Logf("expected: executed=%t isOk=%t status=%d units=%d modified=%d",
			fixture.Output.Executed, fixture.Output.IsOk, fixture.Output.Status,
			fixture.Output.ExecutedUnits, len(fixture.Output.ModifiedAccounts))
	}

	sealevel.ResetSysvarCache()
	f := parseFeatures(fixture)

	tx, err := fixtureToSolTx(fixture)
	if err != nil {
		t.Fatalf("failed to build tx: %v", err)
	}

	slotCtx := setupSlotCtx(fixture, f)
	writeSysvarDefaults(slotCtx, f, fixture)

	txErr := processTransactionConformance(slotCtx, tx)

	if txErr != nil {
		t.Logf("transaction failed: %s", txErr)
	} else {
		t.Logf("transaction succeeded")
	}

	if fixture.Output != nil {
		if txnResultIsExpected(fixture, txErr) {
			t.Logf("RESULT: matches fixture expectation")
		} else {
			t.Logf("RESULT: does NOT match fixture expectation")
		}
	}
}
