package replay

import (
	"encoding/json"
	"testing"

	"github.com/Overclock-Validator/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/stretchr/testify/require"
)

func executionLogs(t *testing.T, out LoadAndExecuteTransactionOutput) []string {
	t.Helper()
	require.NotNil(t, out.ExecCtx)
	rec, ok := out.ExecCtx.Log.(*sealevel.LogRecorder)
	require.True(t, ok, "execution logger must be the LogRecorder")
	return rec.Logs
}

// TestStableLogFraming_SystemTransferSuccess asserts the Agave stable_log
// bracketing for a successful native instruction. The Rust engine logs
// exactly these two lines for a System transfer, and the differential
// harness compares the full log array verbatim.
func TestStableLogFraming_SystemTransferSuccess(t *testing.T) {
	env := newIsolatedEnv(t, 50)
	tx := env.buildTransferTx(t)

	out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
		SlotCtx:      env.slotCtx,
		Transaction:  tx,
		IsSimulation: true,
	})
	require.Nil(t, out.ProcessingResult.TransactionError)

	require.Equal(t, []string{
		"Program 11111111111111111111111111111111 invoke [1]",
		"Program 11111111111111111111111111111111 success",
	}, executionLogs(t, out))
}

// TestStableLogFraming_SystemTransferFailure asserts the failure bracket:
// a transfer exceeding the payer balance fails with SystemError 1
// (ResultWithNegativeLamports), which Agave's InstructionError Display
// renders as "custom program error: 0x1". The system program's ic_msg
// detail line appears between the invoke and failed framing lines,
// mirroring Agave's system_processor.
func TestStableLogFraming_SystemTransferFailure(t *testing.T) {
	env := newIsolatedEnv(t, 51)

	transferIx := system.NewTransferInstruction(
		2_000_000_000, // payer only holds 1 SOL
		env.payer.PublicKey(), env.recipient.PublicKey()).Build()
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

	out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
		SlotCtx:      env.slotCtx,
		Transaction:  tx,
		IsSimulation: true,
	})
	require.NotNil(t, out.ProcessingResult.TransactionError)

	require.Equal(t, []string{
		"Program 11111111111111111111111111111111 invoke [1]",
		"Transfer: insufficient lamports 999995000, need 2000000000",
		"Program 11111111111111111111111111111111 failed: custom program error: 0x1",
	}, executionLogs(t, out))
}

// TestStableLogFraming_RespectsBytesLimit: framing lines go through the same
// LogRecorder byte accounting as syscall log lines, so a tiny limit truncates
// the very first framing line.
func TestStableLogFraming_RespectsBytesLimit(t *testing.T) {
	env := newIsolatedEnv(t, 52)
	tx := env.buildTransferTx(t)

	limit := uint64(10)
	out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
		SlotCtx:       env.slotCtx,
		Transaction:   tx,
		IsSimulation:  true,
		LogBytesLimit: &limit,
	})
	require.Nil(t, out.ProcessingResult.TransactionError)

	require.Equal(t, []string{"Log truncated"}, executionLogs(t, out))
}

// TestInsufficientFundsForRent_AccountIndex asserts that a transfer leaving
// the recipient below the rent-exempt minimum reports the RECIPIENT's index
// in the message account keys (Agave TransactionError::InsufficientFundsForRent
// { account_index }), index 1 for a simple transfer.
func TestInsufficientFundsForRent_AccountIndex(t *testing.T) {
	env := newIsolatedEnv(t, 53)

	transferIx := system.NewTransferInstruction(
		1000, // far below the 0-byte rent-exempt minimum (890880)
		env.payer.PublicKey(), env.recipient.PublicKey()).Build()
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

	out := LoadAndExecuteTransaction(LoadAndExecuteTransactionInput{
		SlotCtx:      env.slotCtx,
		Transaction:  tx,
		IsSimulation: true,
	})

	txErr := out.ProcessingResult.TransactionError
	require.NotNil(t, txErr)
	require.Equal(t, TransactionErrorInsufficientFundsForRent, txErr.ErrorType)
	require.NotNil(t, txErr.AccountIndex)
	require.Equal(t, uint8(1), *txErr.AccountIndex)

	got, err := json.Marshal(txErr)
	require.NoError(t, err)
	require.JSONEq(t, `{"InsufficientFundsForRent":{"account_index":1}}`, string(got))
}
