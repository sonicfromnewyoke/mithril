package replay

import (
	"sync"
	"testing"

	"github.com/Overclock-Validator/mithril/pkg/accounts"
	"github.com/Overclock-Validator/mithril/pkg/addresses"
	"github.com/Overclock-Validator/mithril/pkg/features"
	"github.com/Overclock-Validator/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/stretchr/testify/require"
)

type isolatedEnv struct {
	payer     *solana.Wallet
	recipient *solana.Wallet
	blockhash solana.Hash
	slotCtx   *sealevel.SlotCtx
}

func newIsolatedEnv(t *testing.T, blockhashSeed byte) *isolatedEnv {
	t.Helper()

	feats := features.NewFeaturesDefault()
	feats.EnableFeature(features.FormalizeLoadedTransactionDataSize, 0)

	payer := solana.NewWallet()
	recipient := solana.NewWallet()

	mem := accounts.NewMemAccounts()

	payerAcct := &accounts.Account{
		Key:      payer.PublicKey(),
		Lamports: 1_000_000_000,
		Owner:    addresses.SystemProgramAddr,
	}
	pk := [32]byte(payer.PublicKey())
	require.NoError(t, mem.SetAccount(&pk, payerAcct))

	sysProg := &accounts.Account{
		Key:        addresses.SystemProgramAddr,
		Lamports:   1,
		Owner:      addresses.NativeLoaderAddr,
		Executable: true,
	}
	spk := [32]byte(addresses.SystemProgramAddr)
	require.NoError(t, mem.SetAccount(&spk, sysProg))

	var blockhash solana.Hash
	blockhash[0] = blockhashSeed

	rbh := sealevel.SysvarRecentBlockhashes{
		{Blockhash: blockhash, FeeCalculator: sealevel.FeeCalculator{LamportsPerSignature: 5000}},
	}
	rent := sealevel.NewDefaultRentSysvar()

	sysvarCache := &sealevel.SysvarCacheData{}
	sysvarCache.RecentBlockHashes.Sysvar = &rbh
	sysvarCache.Rent.Sysvar = &rent

	slotCtx := &sealevel.SlotCtx{
		Slot:            100,
		Accounts:        mem,
		ParentAccts:     accounts.NewMemAccounts(),
		Features:        feats,
		FeeRateGovernor: &sealevel.FeeRateGovernor{PrevLamportsPerSignature: 5000},
		AcctMapsMu:      &sync.Mutex{},
		ModifiedAccts:   make(map[solana.PublicKey]bool),
		WritableAccts:   make(map[solana.PublicKey]bool),
		SysvarCache:     sysvarCache,
	}

	return &isolatedEnv{
		payer:     payer,
		recipient: recipient,
		blockhash: blockhash,
		slotCtx:   slotCtx,
	}
}

func (env *isolatedEnv) buildTransferTx(t *testing.T) *solana.Transaction {
	t.Helper()

	transferIx := system.NewTransferInstruction(
		10_000_000, env.payer.PublicKey(), env.recipient.PublicKey()).Build()
	tx, err := solana.NewTransaction(
		[]solana.Instruction{transferIx},
		env.blockhash,
		solana.TransactionPayer(env.payer.PublicKey()),
	)
	require.NoError(t, err)

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(env.payer.PublicKey()) {
			k := env.payer.PrivateKey
			return &k
		}
		return nil
	})
	require.NoError(t, err)

	return tx
}

// TestLoadAndExecuteTransaction_PerInstanceSysvarCache proves that two slot
// contexts with distinct per-instance sysvar caches execute concurrently
// without observing each other's sysvars. The process-global SysvarCache is
// poisoned with an empty blockhash queue for the duration of the test, so any
// code path that still falls back to the global fails the age check and the
// test.
func TestLoadAndExecuteTransaction_PerInstanceSysvarCache(t *testing.T) {
	emptyRBH := sealevel.SysvarRecentBlockhashes{}
	prev := sealevel.SysvarCache.RecentBlockHashes.Sysvar
	sealevel.SysvarCache.RecentBlockHashes.Sysvar = &emptyRBH
	defer func() { sealevel.SysvarCache.RecentBlockHashes.Sysvar = prev }()

	envA := newIsolatedEnv(t, 42)
	envB := newIsolatedEnv(t, 43)

	var wg sync.WaitGroup
	for _, env := range []*isolatedEnv{envA, envB} {
		wg.Add(1)
		go func(env *isolatedEnv) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				tx := env.buildTransferTx(t)
				out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
					SlotCtx:      env.slotCtx,
					Transaction:  tx,
					IsSimulation: true,
				})
				if out.ProcessingResult.TransactionError != nil {
					t.Errorf("expected success, got %v", out.ProcessingResult.TransactionError)
					return
				}
			}
		}(env)
	}
	wg.Wait()

	// Cross-check that the caches are genuinely distinct: a tx whose
	// blockhash only exists in envA's cache must fail age validation when
	// executed against envB.
	txA := envA.buildTransferTx(t)
	out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
		SlotCtx:      envB.slotCtx,
		Transaction:  txA,
		IsSimulation: true,
	})
	require.NotNil(t, out.ProcessingResult.TransactionError,
		"tx with envA's blockhash must not validate against envB's sysvar cache")
}

// TestLoadAndExecuteTransaction_LogBytesLimit verifies that the input's
// LogBytesLimit reaches the execution log recorder. With stable_log framing
// the transfer's own "Program ... invoke [1]" line (45 bytes) already trips
// the 10-byte limit during execution; the recorder wiring is additionally
// asserted through the returned ExecCtx. LogRecorder truncation semantics
// themselves are covered by unit tests in pkg/sealevel.
func TestLoadAndExecuteTransaction_LogBytesLimit(t *testing.T) {
	env := newIsolatedEnv(t, 44)
	tx := env.buildTransferTx(t)

	limit := uint64(10)
	out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
		SlotCtx:       env.slotCtx,
		Transaction:   tx,
		IsSimulation:  true,
		LogBytesLimit: &limit,
	})
	require.Nil(t, out.ProcessingResult.TransactionError)
	require.NotNil(t, out.ExecCtx)

	rec, ok := out.ExecCtx.Log.(*sealevel.LogRecorder)
	require.True(t, ok, "execution logger must be the LogRecorder")
	require.Equal(t, &limit, rec.BytesLimit, "LogBytesLimit must reach the recorder")

	rec.Log("0123456789ab")
	require.Equal(t, []string{"Log truncated"}, rec.Logs)
}
