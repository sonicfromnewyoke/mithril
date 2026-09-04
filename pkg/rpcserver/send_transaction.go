package rpcserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/config"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/global"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/sonicfromnewyoke/mithril/pkg/replay"
	"github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/filecoin-project/go-jsonrpc"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/mr-tron/base58"
)

type clusterNodesFetcher func(context.Context) ([]*solanarpc.GetClusterNodesResult, error)
type transactionSender func(context.Context, []byte, tpuEndpoint) error

type tpuTransport string

const (
	tpuTransportUDP  tpuTransport = "udp"
	tpuTransportQUIC tpuTransport = "quic"
)

type tpuEndpoint struct {
	Addr      netip.AddrPort
	Transport tpuTransport
}

func (endpoint tpuEndpoint) String() string {
	return fmt.Sprintf("%s/%s", endpoint.Addr.String(), endpoint.Transport)
}

type sendTransactionConfig struct {
	encoding            string
	skipPreflight       bool
	preflightCommitment string
	maxRetries          *uint
	minContextSlot      *uint64
}

const (
	maxBase58TxSize                         = 1683
	maxBase64TxSize                         = 1644
	packetDataSize                          = 1232
	sendTransactionLeaderForwardCount       = 10
	sendTransactionTargetCount              = sendTransactionLeaderForwardCount + 1
	sendTransactionLeaderLookahead          = 64
	sendTransactionClusterNodesRefreshEvery = 10 * time.Minute
	sendTransactionTPUSendTimeout           = 3 * time.Second
	maxSanitizedInstructionCount            = 64
)

var errInvalidSanitizedTransaction = &InvalidParamsError{
	Message: "invalid transaction: Transaction failed to sanitize accounts offsets correctly",
}

func (rpcServer *RpcServer) SendTransaction(ctx context.Context, p jsonrpc.RawParams) (string, error) {
	params, err := jsonrpc.DecodeParams[[]interface{}](p)
	if err != nil {
		return "", &InvalidParamsError{Message: fmt.Sprintf("decoding params: %v", err)}
	}
	if len(params) < 1 {
		return "", &InvalidParamsError{Message: "sendTransaction requires a transaction string as first parameter"}
	}

	txStr, ok := params[0].(string)
	if !ok {
		return "", &InvalidParamsError{Message: "sendTransaction requires a transaction string as first parameter"}
	}

	conf := parseSendTransactionConfig(params)

	tx, wire, err := decodeSendTransaction(txStr, conf.encoding)
	if err != nil {
		return "", err
	}

	// Legacy and already-expanded txs can be bounds-checked immediately.
	if err := validateSendTransactionSanitize(tx, featuresForSendValidation(rpcServer.getSlotCtx()), shouldValidateInstructionIndexes(tx)); err != nil {
		return "", err
	}

	if conf.minContextSlot != nil && global.Slot() < *conf.minContextSlot {
		return "", &MinContextSlotNotReachedError{ContextSlot: *conf.minContextSlot}
	}

	if !conf.skipPreflight {
		slotCtx := rpcServer.getSlotCtx()
		if slotCtx == nil {
			return "", fmt.Errorf("node is not ready for transaction preflight")
		}
		if err := rpcServer.preflightSendTransaction(ctx, tx, slotCtx); err != nil {
			return "", err
		}
	}

	signature, err := firstSignature(tx)
	if err != nil {
		return "", err
	}

	if err := rpcServer.forwardTransactionToUpcomingLeaders(ctx, wire); err != nil {
		return "", err
	}

	return signature.String(), nil
}

func parseSendTransactionConfig(params []interface{}) sendTransactionConfig {
	conf := sendTransactionConfig{
		encoding: "base58",
	}
	if len(params) < 2 {
		return conf
	}

	confMap, ok := params[1].(map[string]interface{})
	if !ok {
		return conf
	}

	if encoding, ok := confMap["encoding"].(string); ok {
		conf.encoding = encoding
	}
	if skipPreflight, ok := confMap["skipPreflight"].(bool); ok {
		conf.skipPreflight = skipPreflight
	}
	if preflightCommitment, ok := confMap["preflightCommitment"].(string); ok {
		conf.preflightCommitment = preflightCommitment
	}
	if maxRetries, ok := confMap["maxRetries"].(float64); ok && maxRetries >= 0 {
		v := uint(maxRetries)
		conf.maxRetries = &v
	}
	if minContextSlot, ok := confMap["minContextSlot"].(float64); ok && minContextSlot >= 0 {
		v := uint64(minContextSlot)
		conf.minContextSlot = &v
	}

	return conf
}

func decodeSendTransaction(txStr string, encoding string) (*solana.Transaction, []byte, error) {
	if encoding != "base58" && encoding != "base64" {
		return nil, nil, &InvalidParamsError{
			Message: fmt.Sprintf("unsupported encoding: %s. Supported encodings: base58, base64", encoding),
		}
	}

	if encoding == "base58" && len(txStr) > maxBase58TxSize {
		return nil, nil, &InvalidParamsError{
			Message: fmt.Sprintf("base58 encoded solana_transaction too large: %d bytes (max: encoded/raw %d/%d)", len(txStr), maxBase58TxSize, packetDataSize),
		}
	}
	if encoding == "base64" && len(txStr) > maxBase64TxSize {
		return nil, nil, &InvalidParamsError{
			Message: fmt.Sprintf("base64 encoded solana_transaction too large: %d bytes (max: encoded/raw %d/%d)", len(txStr), maxBase64TxSize, packetDataSize),
		}
	}

	var (
		wire []byte
		err  error
	)
	switch encoding {
	case "base58":
		wire, err = base58.Decode(txStr)
		if err != nil {
			return nil, nil, &InvalidParamsError{Message: fmt.Sprintf("invalid base58 encoding: %v", err)}
		}
	default:
		wire, err = base64.StdEncoding.DecodeString(txStr)
		if err != nil {
			return nil, nil, &InvalidParamsError{Message: fmt.Sprintf("invalid base64 encoding: %v", err)}
		}
	}

	if len(wire) > packetDataSize {
		return nil, nil, &InvalidParamsError{
			Message: fmt.Sprintf("decoded solana_transaction too large: %d bytes (max: %d bytes)", len(wire), packetDataSize),
		}
	}

	tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(wire))
	if err != nil {
		return nil, nil, &InvalidParamsError{Message: fmt.Sprintf("failed to deserialize solana_transaction: %v", err)}
	}

	return tx, wire, nil
}

func featuresForSendValidation(slotCtx *sealevel.SlotCtx) *features.Features {
	if slotCtx != nil && slotCtx.Features != nil {
		return slotCtx.Features
	}
	return nil
}

func shouldValidateInstructionIndexes(tx *solana.Transaction) bool {
	return !tx.Message.IsVersioned() || tx.Message.AddressTableLookups.NumLookups() == 0
}

func validateSendTransactionSanitize(tx *solana.Transaction, feats *features.Features, validateInstructionIndexes bool) error {
	hdr := tx.Message.Header
	numKeys := len(tx.Message.AccountKeys)

	if hdr.NumReadonlySignedAccounts >= hdr.NumRequiredSignatures ||
		int(hdr.NumRequiredSignatures) > len(tx.Signatures) ||
		len(tx.Signatures) > numKeys {
		return errInvalidSanitizedTransaction
	}

	if feats != nil &&
		feats.IsActive(features.StaticInstructionLimit) &&
		len(tx.Message.Instructions) > maxSanitizedInstructionCount {
		return errInvalidSanitizedTransaction
	}

	if !validateInstructionIndexes {
		return nil
	}

	for _, ci := range tx.Message.Instructions {
		if int(ci.ProgramIDIndex) >= numKeys {
			return errInvalidSanitizedTransaction
		}
		for _, idx := range ci.Accounts {
			if int(idx) >= numKeys {
				return errInvalidSanitizedTransaction
			}
		}
	}

	return nil
}

func firstSignature(tx *solana.Transaction) (solana.Signature, error) {
	if len(tx.Signatures) == 0 {
		return solana.Signature{}, errInvalidSanitizedTransaction
	}
	return tx.Signatures[0], nil
}

func (rpcServer *RpcServer) preflightSendTransaction(ctx context.Context, tx *solana.Transaction, slotCtx *sealevel.SlotCtx) error {
	if err := rpcServer.resolveAddressTablesForPreflight(ctx, tx, slotCtx); err != nil {
		return err
	}

	if err := validateSendTransactionSanitize(tx, slotCtx.Features, true); err != nil {
		return err
	}

	if err := tx.VerifySignatures(); err != nil {
		return signaturePreflightFailure()
	}

	output := replay.LoadAndExecuteTransaction(replay.LoadAndExecuteTransactionInput{
		SlotCtx:                 slotCtx,
		Transaction:             tx,
		TxMeta:                  nil,
		IsSimulation:            true,
		RecordInnerInstructions: false,
	})

	if output.ProcessingResult.TransactionError == nil {
		return nil
	}
	if output.ProcessingResult.TransactionError.ErrorType == replay.TransactionErrorSanitizeFailure {
		return errInvalidSanitizedTransaction
	}

	txErr := output.ProcessingResult.TransactionError
	return &SendTransactionPreflightFailureError{
		Message: fmt.Sprintf("Transaction simulation failed: %s", sendTransactionErrorMessage(txErr)),
		Result:  sendTransactionFailureResultFromOutput(output),
	}
}

func (rpcServer *RpcServer) resolveAddressTablesForPreflight(ctx context.Context, tx *solana.Transaction, slotCtx *sealevel.SlotCtx) error {
	if !tx.Message.IsVersioned() || tx.Message.AddressTableLookups.NumLookups() == 0 {
		return nil
	}
	if rpcServer.acctsDb == nil {
		return &InvalidParamsError{Message: "invalid transaction: address lookup table resolution unavailable"}
	}
	if err := replay.ResolveAddrTableLookupsForTx(ctx, rpcServer.acctsDb, slotCtx.Slot, tx); err != nil {
		return &InvalidParamsError{Message: fmt.Sprintf("invalid transaction: %s", err.Error())}
	}
	return nil
}

func signaturePreflightFailure() *SendTransactionPreflightFailureError {
	return &SendTransactionPreflightFailureError{
		Message: "Transaction simulation failed: Transaction did not pass signature verification",
		Result: sendTransactionFailureResult(
			"SignatureFailure",
			[]string{},
			0,
			0,
			nil,
		),
	}
}

func sendTransactionFailureResultFromOutput(output replay.LoadAndExecuteTransactionOutput) SimulateTransactionRespValue {
	logs := []string{}
	if output.ExecCtx != nil {
		if logRecorder, ok := output.ExecCtx.Log.(*sealevel.LogRecorder); ok && logRecorder != nil && logRecorder.Logs != nil {
			logs = clampLogs(logRecorder.Logs)
		}
	}

	var (
		unitsConsumed uint64
		dataSize      uint32
		returnData    *ReturnDataPayload
	)

	if processedTx := output.ProcessingResult.ProcessedTransaction; processedTx != nil && processedTx.Executed != nil {
		executed := processedTx.Executed
		unitsConsumed = executed.ExecutionDetails.ExecutedUnits
		dataSize = executed.LoadedTransaction.LoadedAccountsDataSize
		if executed.ExecutionDetails.ReturnData != nil {
			rd := executed.ExecutionDetails.ReturnData
			clamped := clampReturnData(rd.Data)
			returnData = &ReturnDataPayload{
				ProgramId: rd.ProgramId.String(),
				Data:      []string{base64.StdEncoding.EncodeToString(clamped), "base64"},
			}
		}
	} else if output.ExecCtx != nil {
		unitsConsumed = output.ExecCtx.ComputeMeter.Used()
		dataSize = loadedAccountsDataSizeFromExecCtx(output.ExecCtx)
	}

	return sendTransactionFailureResult(
		output.ProcessingResult.TransactionError,
		logs,
		unitsConsumed,
		dataSize,
		returnData,
	).withFee(output)
}

func sendTransactionFailureResult(errValue interface{}, logs []string, unitsConsumed uint64, dataSize uint32, returnData *ReturnDataPayload) SimulateTransactionRespValue {
	return SimulateTransactionRespValue{
		Err:                    errValue,
		Logs:                   ptrSlice(logs),
		UnitsConsumed:          &unitsConsumed,
		ReturnData:             returnData,
		InnerInstructions:      nil,
		LoadedAccountsDataSize: &dataSize,
		Fee:                    nil,
		PreBalances:            nil,
		PostBalances:           nil,
		PreTokenBalances:       nil,
		PostTokenBalances:      nil,
		LoadedAddresses:        nil,
	}
}

func (v SimulateTransactionRespValue) withFee(output replay.LoadAndExecuteTransactionOutput) SimulateTransactionRespValue {
	if output.FeeInfo != nil {
		fee := output.FeeInfo.TotalFee
		v.Fee = &fee
	}
	return v
}

func sendTransactionErrorMessage(txErr *replay.TransactionError) string {
	if txErr == nil {
		return "unknown error"
	}

	switch txErr.ErrorType {
	case replay.TransactionErrorBlockhashNotFound:
		return "Blockhash not found"
	case replay.TransactionErrorSignatureFailure:
		return "Transaction did not pass signature verification"
	case replay.TransactionErrorSanitizeFailure:
		return "Transaction failed to sanitize accounts offsets correctly"
	case replay.TransactionErrorInstructionError:
		if txErr.InstructionError != nil {
			return normalizeSendTransactionErrorName(txErr.InstructionError.Error())
		}
	}

	return normalizeSendTransactionErrorName(txErr.ErrorType.String())
}

func normalizeSendTransactionErrorName(name string) string {
	switch {
	case strings.HasPrefix(name, "InstrErr"):
		return strings.TrimPrefix(name, "InstrErr")
	case strings.HasPrefix(name, "TxErr"):
		return strings.TrimPrefix(name, "TxErr")
	default:
		return name
	}
}

func (rpcServer *RpcServer) forwardTransactionToUpcomingLeaders(ctx context.Context, wire []byte) error {
	targetCount := int(rpcServer.sendTransactionLeaderForwardCount) + 1
	if targetCount <= 1 {
		targetCount = sendTransactionTargetCount
	}

	targets, err := rpcServer.resolveUpcomingLeaderTPUEndpoints(ctx, targetCount)
	if err != nil {
		return err
	}

	send := rpcServer.transactionSender
	if send == nil {
		send = defaultTransactionSender
	}

	var sendErrs []error
	sentCount := 0
	var sendMu sync.Mutex
	var sendWg sync.WaitGroup
	for _, target := range targets {
		target := target
		sendWg.Add(1)
		go func() {
			defer sendWg.Done()
			sendCtx, cancel := context.WithTimeout(ctx, sendTransactionTPUSendTimeout)
			defer cancel()

			err := send(sendCtx, wire, target)
			sendMu.Lock()
			defer sendMu.Unlock()
			if err != nil {
				sendErrs = append(sendErrs, fmt.Errorf("%s: %w", target.String(), err))
				return
			}
			sentCount++
		}()
	}
	sendWg.Wait()

	if sentCount == 0 {
		return fmt.Errorf("failed to forward transaction to any leader TPU: %w", errors.Join(sendErrs...))
	}
	if len(sendErrs) > 0 {
		mlog.Log.Warnf("sendTransaction: forwarded to %d/%d leader TPUs; partial failures: %v", sentCount, len(targets), errors.Join(sendErrs...))
	}
	return nil
}

func (rpcServer *RpcServer) resolveUpcomingLeaderTPUEndpoints(ctx context.Context, want int) ([]tpuEndpoint, error) {
	if want <= 0 {
		want = 1
	}

	targets, updatedAt := rpcServer.collectUpcomingLeaderTPUEndpointsFromCache(want)
	cacheStale := updatedAt.IsZero() || time.Since(updatedAt) >= rpcServer.clusterNodesRefreshInterval()
	if cacheStale || len(targets) < want {
		if err := rpcServer.refreshLeaderTPUCache(ctx); err != nil {
			if len(targets) == 0 {
				return nil, err
			}
			mlog.Log.Warnf("sendTransaction: using partial cached TPU target set after refresh failure: %v", err)
			return targets, nil
		}
		targets, _ = rpcServer.collectUpcomingLeaderTPUEndpointsFromCache(want)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("unable to resolve TPU addresses for upcoming leaders from current leader schedule")
	}
	return targets, nil
}

func (rpcServer *RpcServer) fetchClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
	if rpcServer.clusterNodesFetcher != nil {
		return rpcServer.clusterNodesFetcher(ctx)
	}

	endpoints := rpcServer.clusterRPCEndpoints
	if len(endpoints) == 0 {
		endpoints = configuredSendTransactionRPCEndpoints()
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no cluster RPC endpoints configured for sendTransaction leader resolution")
	}

	var lastErr error
	for _, endpoint := range endpoints {
		client := solanarpc.New(endpoint)
		nodes, err := client.GetClusterNodes(ctx)
		if err == nil {
			return nodes, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unable to fetch cluster nodes")
	}
	return nil, lastErr
}

func (rpcServer *RpcServer) refreshLeaderTPUCache(ctx context.Context) error {
	nodes, err := rpcServer.fetchClusterNodes(ctx)
	if err != nil {
		return err
	}

	leaderTPUs := make(map[solana.PublicKey]tpuEndpoint, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}

		endpoint, ok := leaderTPUEndpointFromClusterNode(node)
		if !ok {
			continue
		}
		leaderTPUs[node.Pubkey] = endpoint
	}

	if len(leaderTPUs) == 0 {
		return fmt.Errorf("cluster did not advertise any TPU endpoints")
	}

	rpcServer.leaderTPUCacheMu.Lock()
	rpcServer.leaderTPUByIdentity = leaderTPUs
	rpcServer.leaderTPUCacheUpdatedAt = time.Now()
	rpcServer.leaderTPUCacheMu.Unlock()
	return nil
}

func leaderTPUEndpointFromClusterNode(node *solanarpc.GetClusterNodesResult) (tpuEndpoint, bool) {
	if endpoint, ok := parseLeaderTPUEndpoint(node.TPUQUIC, tpuTransportQUIC); ok {
		return endpoint, true
	}
	return parseLeaderTPUEndpoint(node.TPU, tpuTransportUDP)
}

func parseLeaderTPUEndpoint(raw *string, transport tpuTransport) (tpuEndpoint, bool) {
	if raw == nil || *raw == "" {
		return tpuEndpoint{}, false
	}
	addr, err := netip.ParseAddrPort(*raw)
	if err != nil {
		return tpuEndpoint{}, false
	}
	return tpuEndpoint{Addr: addr, Transport: transport}, true
}

func (rpcServer *RpcServer) collectUpcomingLeaderTPUEndpointsFromCache(want int) ([]tpuEndpoint, time.Time) {
	rpcServer.leaderTPUCacheMu.RLock()
	nodeTPUs := make(map[solana.PublicKey]tpuEndpoint, len(rpcServer.leaderTPUByIdentity))
	for leader, endpoint := range rpcServer.leaderTPUByIdentity {
		nodeTPUs[leader] = endpoint
	}
	updatedAt := rpcServer.leaderTPUCacheUpdatedAt
	rpcServer.leaderTPUCacheMu.RUnlock()

	currentSlot := global.Slot()
	targets := make([]tpuEndpoint, 0, want)
	seenLeaders := make(map[solana.PublicKey]struct{}, want)
	seenTargets := make(map[tpuEndpoint]struct{}, want)

	for offset := uint64(0); offset < sendTransactionLeaderLookahead && len(targets) < want; offset++ {
		leader, ok := global.LeaderForSlot(currentSlot + offset)
		if !ok {
			continue
		}
		if _, exists := seenLeaders[leader]; exists {
			continue
		}
		seenLeaders[leader] = struct{}{}

		target, ok := nodeTPUs[leader]
		if !ok {
			continue
		}
		if _, exists := seenTargets[target]; exists {
			continue
		}
		seenTargets[target] = struct{}{}
		targets = append(targets, target)
	}

	return targets, updatedAt
}

func configuredSendTransactionRPCEndpoints() []string {
	endpoints := config.GetStringSlice("network.rpc")
	if len(endpoints) == 0 {
		endpoints = config.GetStringSlice("rpc.rpc")
	}
	return endpoints
}

func defaultTransactionSender(ctx context.Context, payload []byte, target tpuEndpoint) error {
	switch target.Transport {
	case tpuTransportQUIC:
		return defaultTPUQUICSender.Send(ctx, payload, target.Addr)
	case tpuTransportUDP:
		return defaultUDPPacketSender(payload, target.Addr)
	default:
		return fmt.Errorf("unsupported TPU transport %q", target.Transport)
	}
}

func defaultUDPPacketSender(payload []byte, target netip.AddrPort) error {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	written, err := conn.WriteToUDPAddrPort(payload, target)
	if err != nil {
		return err
	}
	if written != len(payload) {
		return fmt.Errorf("short write: wrote %d of %d bytes", written, len(payload))
	}
	return nil
}
