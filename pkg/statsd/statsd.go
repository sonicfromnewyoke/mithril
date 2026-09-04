package statsd

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	mithrilmetrics "github.com/sonicfromnewyoke/mithril/pkg/metrics"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
)

type metricType int

const (
	TimingT metricType = iota
	CountT
	GaugeT
)

// SAFE METRIC TYPE
type Metric struct {
	name string // unexported → cannot be created outside this package
}

func (m Metric) String() string { return m.name }

var (
	PreprocessBlock     = Metric{"preprocess_block"}
	TxLoop              = Metric{"tx_loop"}
	LoadBlockAccounts   = Metric{"load_block_accounts"}
	RunIncinerator      = Metric{"run_incinerator"}
	Reward              = Metric{"reward"}
	Rent                = Metric{"rent"}
	BlockUpdateAccounts = Metric{"block_update_accounts"}
	AccountsDeltaHash   = Metric{"accounts_delta_hash"}
	BankHash            = Metric{"bank_hash"}

	InstructionsAndAccountMetasFromTx  = Metric{"instructions_and_account_metas_from_tx"}
	ComputeBudgetExecutionInstructions = Metric{"compute_budget_execution_instructions"}
	AccountsFromTx                     = Metric{"accounts_from_tx"}
	PreBalanceDivergenceCheck          = Metric{"pre_balance_divergence_check"}
	CalcAndDeductFees                  = Metric{"calc_and_deduct_fees"}
	ReadRentSysvar                     = Metric{"read_rent_sysvar"}
	PreTxRentStates                    = Metric{"pre_tx_rent_states"}
	IxLoop                             = Metric{"ix_loop"}
	PostTxRentStates                   = Metric{"post_tx_rent_states"}
	PostBalanceDivergenceCheck         = Metric{"post_balance_divergence_check"}
	TxUpdateAccounts                   = Metric{"tx_update_accounts"}

	GetNextIxCtx                            = Metric{"get_next_ix_ctx"}
	NextIxCtxConfigure                      = Metric{"next_ix_ctx_configure"}
	IxPush                                  = Metric{"ix_push"}
	IxPop                                   = Metric{"ix_pop"}
	ExecIxResolveNativeProgram              = Metric{"exec_ix_resolve_native_program"}
	ExecIxNativeProgramSystem               = Metric{"exec_ix_native_program_system"}
	ExecIxNativeProgramStake                = Metric{"exec_ix_native_program_stake"}
	ExecIxNativeProgramVote                 = Metric{"exec_ix_native_program_vote"}
	ExecIxNativeProgramComputeBudget        = Metric{"exec_ix_native_program_compute_budget"}
	ExecIxNativeProgramBpfLoader2           = Metric{"exec_ix_native_program_bpf_loader2"}
	ExecIxNativeProgramBpfLoaderDeprecated  = Metric{"exec_ix_native_program_bpf_loader_deprecated"}
	ExecIxNativeProgramBpfLoaderUpgradeable = Metric{"exec_ix_native_program_bpf_loader_upgradeable"}
	ExecIxNativeProgramZkElgamalProof       = Metric{"exec_ix_native_program_zk_elgamal_proof"}
	ExecIxNativeProgramEd25519Precompile    = Metric{"exec_ix_native_program_ed25519precompile"}
	ExecIxNativeProgramSecp256kPrecompile   = Metric{"exec_ix_native_program_secp256k_precompile"}
	FixupInstructionsSysvarAccount          = Metric{"fixup_instructions_sysvar_account"}
	InstructionAccountsFromAccountMetas     = Metric{"instruction_accounts_from_account_metas"}

	SbpfInterpreterNew = Metric{"sbpf_interpreter_new"}
	SbpfInterpreterRun = Metric{"sbpf_interpreter_run"}

	AddProgramToCache                = Metric{"add_program_to_cache"}
	GetProgramAccount                = Metric{"get_program_account"}
	GetProgramDataCached             = Metric{"get_program_data_cached"}
	GetProgramDataUncachedAccountsDb = Metric{"get_program_data_uncached_accounts_db"}
	GetProgramDataUncachedAccounts   = Metric{"get_program_data_uncached_accounts"}
	GetProgramDataUncachedMarshal    = Metric{"get_program_data_uncached_marshal"}

	TaskIndexEntryCommitterLatency = Metric{"tasks_index_entry_committer_latency"}
	TaskSetIfSlotHigherLatency     = Metric{"tasks_set_if_slot_higher_latency"}
	TasksIndexEntryBuilderLatency  = Metric{"tasks_index_entry_builder_latency"}
	TasksAppendVecCopyingLatency   = Metric{"tasks_append_vec_copying_latency"}
	SlotReplayDurationMs           = Metric{"slot_replay_duration_ms"}
	TxsPerBlock                    = Metric{"txs_per_block"}
	SnapshotTarBytesRead           = Metric{"snapshot_tar_bytes_read"}
	SlotReplays                    = Metric{"slot_replays"}

	SnapshotWorkerPoolUtilization = Metric{"snapshot_worker_pool_utilization"}
	TasksSetIfSlotHigherQueueSize = Metric{"tasks_set_if_slot_higher_queue_size"}
	Epoch                         = Metric{"epoch"}
	Slot                          = Metric{"slot"}

	TestCount = Metric{"test_count"} // used for testing purposes, not a real metric
)

//
// MAP METRICS TO TYPES + LABELS
//

// making these public so they can be used in tests and validate that if there is metrics there are corresponding types and labels

var MetricToType = map[Metric]metricType{
	PreprocessBlock:     TimingT,
	TxLoop:              TimingT,
	LoadBlockAccounts:   TimingT,
	RunIncinerator:      TimingT,
	Reward:              TimingT,
	Rent:                TimingT,
	BlockUpdateAccounts: TimingT,
	AccountsDeltaHash:   TimingT,
	BankHash:            TimingT,

	InstructionsAndAccountMetasFromTx:  TimingT,
	ComputeBudgetExecutionInstructions: TimingT,
	AccountsFromTx:                     TimingT,
	PreBalanceDivergenceCheck:          TimingT,
	CalcAndDeductFees:                  TimingT,
	ReadRentSysvar:                     TimingT,
	PreTxRentStates:                    TimingT,
	IxLoop:                             TimingT,
	PostTxRentStates:                   TimingT,
	PostBalanceDivergenceCheck:         TimingT,
	TxUpdateAccounts:                   TimingT,

	GetNextIxCtx:                            TimingT,
	NextIxCtxConfigure:                      TimingT,
	IxPush:                                  TimingT,
	IxPop:                                   TimingT,
	ExecIxResolveNativeProgram:              TimingT,
	ExecIxNativeProgramSystem:               TimingT,
	ExecIxNativeProgramStake:                TimingT,
	ExecIxNativeProgramVote:                 TimingT,
	ExecIxNativeProgramComputeBudget:        TimingT,
	ExecIxNativeProgramBpfLoader2:           TimingT,
	ExecIxNativeProgramBpfLoaderDeprecated:  TimingT,
	ExecIxNativeProgramBpfLoaderUpgradeable: TimingT,
	ExecIxNativeProgramZkElgamalProof:       TimingT,
	ExecIxNativeProgramEd25519Precompile:    TimingT,
	ExecIxNativeProgramSecp256kPrecompile:   TimingT,
	FixupInstructionsSysvarAccount:          TimingT,
	InstructionAccountsFromAccountMetas:     TimingT,

	SbpfInterpreterNew:               TimingT,
	SbpfInterpreterRun:               TimingT,
	AddProgramToCache:                TimingT,
	GetProgramAccount:                TimingT,
	GetProgramDataCached:             TimingT,
	GetProgramDataUncachedAccountsDb: TimingT,
	GetProgramDataUncachedAccounts:   TimingT,
	GetProgramDataUncachedMarshal:    TimingT,

	TaskIndexEntryCommitterLatency: TimingT,
	TaskSetIfSlotHigherLatency:     TimingT,
	TasksIndexEntryBuilderLatency:  TimingT,
	TasksAppendVecCopyingLatency:   TimingT,

	SlotReplayDurationMs: TimingT,
	TxsPerBlock:          TimingT,

	SnapshotTarBytesRead: CountT,
	SlotReplays:          CountT,

	TestCount: CountT,

	SnapshotWorkerPoolUtilization: GaugeT,
	TasksSetIfSlotHigherQueueSize: GaugeT,
	Epoch:                         GaugeT,
	Slot:                          GaugeT,
}
var MetricToLabels = map[Metric][]string{
	PreprocessBlock:     {"phase"},
	TxLoop:              {"phase"},
	LoadBlockAccounts:   {"phase"},
	RunIncinerator:      {"phase"},
	Reward:              {"phase"},
	Rent:                {"phase"},
	BlockUpdateAccounts: {"phase"},
	AccountsDeltaHash:   {"phase"},
	BankHash:            {"phase"},

	InstructionsAndAccountMetasFromTx:  {"phase"},
	ComputeBudgetExecutionInstructions: {"phase"},
	AccountsFromTx:                     {"phase"},
	PreBalanceDivergenceCheck:          {"phase"},
	CalcAndDeductFees:                  {"phase"},
	ReadRentSysvar:                     {"phase"},
	PreTxRentStates:                    {"phase"},
	IxLoop:                             {"phase"},
	PostTxRentStates:                   {"phase"},
	PostBalanceDivergenceCheck:         {"phase"},
	TxUpdateAccounts:                   {"phase"},

	GetNextIxCtx:                            {"phase"},
	NextIxCtxConfigure:                      {"phase"},
	IxPush:                                  {"phase"},
	IxPop:                                   {"phase"},
	ExecIxResolveNativeProgram:              {"phase"},
	ExecIxNativeProgramSystem:               {"phase"},
	ExecIxNativeProgramStake:                {"phase"},
	ExecIxNativeProgramVote:                 {"phase"},
	ExecIxNativeProgramComputeBudget:        {"phase"},
	ExecIxNativeProgramBpfLoader2:           {"phase"},
	ExecIxNativeProgramBpfLoaderDeprecated:  {"phase"},
	ExecIxNativeProgramBpfLoaderUpgradeable: {"phase"},
	ExecIxNativeProgramZkElgamalProof:       {"phase"},
	ExecIxNativeProgramEd25519Precompile:    {"phase"},
	ExecIxNativeProgramSecp256kPrecompile:   {"phase"},
	FixupInstructionsSysvarAccount:          {"phase"},
	InstructionAccountsFromAccountMetas:     {"phase"},

	SbpfInterpreterNew:               {"phase"},
	SbpfInterpreterRun:               {"phase"},
	AddProgramToCache:                {"phase"},
	GetProgramAccount:                {"phase"},
	GetProgramDataCached:             {"phase"},
	GetProgramDataUncachedAccountsDb: {"phase"},
	GetProgramDataUncachedAccounts:   {"phase"},
	GetProgramDataUncachedMarshal:    {"phase"},

	TaskIndexEntryCommitterLatency: {},
	TaskSetIfSlotHigherLatency:     {},
	TasksIndexEntryBuilderLatency:  {},
	TasksAppendVecCopyingLatency:   {},

	SlotReplayDurationMs: {},
	TxsPerBlock:          {},
	SnapshotTarBytesRead: {},
	SlotReplays:          {},

	SnapshotWorkerPoolUtilization: {"task"},
	TasksSetIfSlotHigherQueueSize: {},
	Epoch:                         {},
	Slot:                          {},

	TestCount: {"test"}, // used for testing purposes, not a real metric
}

type Prometheusmetrics struct {
	counters   map[Metric]*prometheus.CounterVec
	gauges     map[Metric]*prometheus.GaugeVec
	histograms map[Metric]*prometheus.HistogramVec
}

var metricsCollection *Prometheusmetrics
var metricsServerStarted bool

func init() {
	initializeStatsdMetrics()
}

// StartMetricsServer starts the Prometheus metrics HTTP server.
// Call this after printing the banner to avoid error messages appearing before it.
func StartMetricsServer() {
	if metricsServerStarted {
		return
	}
	metricsServerStarted = true
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		addr := ":9090"
		if err := http.ListenAndServe(addr, nil); err != nil {
			mlog.Log.Errorf("Prometheus metrics server failed: %v", err)
		}
	}()
}

func initializeStatsdMetrics() Prometheusmetrics {
	metricsCollection = &Prometheusmetrics{
		counters:   make(map[Metric]*prometheus.CounterVec),
		gauges:     make(map[Metric]*prometheus.GaugeVec),
		histograms: make(map[Metric]*prometheus.HistogramVec),
	}
	for m, t := range MetricToType {
		labelNames := MetricToLabels[m]

		switch t {
		case TimingT:
			hv := promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name: m.name,
				Help: fmt.Sprintf("Histogram for %s", m.name),
			}, labelNames)
			metricsCollection.histograms[m] = hv
		case CountT:
			cv := promauto.NewCounterVec(prometheus.CounterOpts{
				Name: m.name,
				Help: fmt.Sprintf("Counter for %s", m.name),
			}, labelNames)
			metricsCollection.counters[m] = cv
		case GaugeT:
			gv := promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: m.name,
				Help: fmt.Sprintf("Gauge for %s", m.name),
			}, labelNames)
			metricsCollection.gauges[m] = gv
		}
	}
	return *metricsCollection
}

func Count(m Metric, value int64, labels []string) error {
	if labels == nil {
		labels = []string{}
	}
	metricsCollection.counters[m].WithLabelValues(labels...).Add(float64(value))
	return nil
}

func Gauge(m Metric, value float64, labels []string) error {
	if labels == nil {
		labels = []string{}
	}
	metricsCollection.gauges[m].WithLabelValues(labels...).Set(value)
	return nil
}

// Create timing function for readability, but under the hood its implemented via distribution
func Timing(m Metric, nanos uint64, labels []string) error {
	if labels == nil {
		labels = []string{}
	}
	metricsCollection.histograms[m].WithLabelValues(labels...).Observe(float64(nanos))
	return nil
}

func SendBlockReplayMetrics(r mithrilmetrics.BlockReplay) {
	blockLatency := "replay_block"
	txLatency := "replay_tx_sum"
	ixLatency := "replay_ix_sum"
	sbpfLatency := "replay_sbpf"

	Timing(PreprocessBlock, r.PreprocessBlock.SumNanoseconds, []string{blockLatency})
	Timing(LoadBlockAccounts, r.LoadBlockAccounts.SumNanoseconds, []string{blockLatency})
	Timing(TxLoop, r.TxLoop.SumNanoseconds, []string{blockLatency})
	Timing(Reward, r.Reward.SumNanoseconds, []string{blockLatency})
	Timing(Rent, r.Rent.SumNanoseconds, []string{blockLatency})
	Timing(RunIncinerator, r.RunIncinerator.SumNanoseconds, []string{blockLatency})
	Timing(BlockUpdateAccounts, r.BlockUpdateAccounts.SumNanoseconds, []string{blockLatency})
	Timing(AccountsDeltaHash, r.AccountsDeltaHash.SumNanoseconds, []string{blockLatency})
	Timing(BankHash, r.BankHash.SumNanoseconds, []string{blockLatency})
	Timing(InstructionsAndAccountMetasFromTx, r.InstructionsAndAccountMetasFromTx.SumNanoseconds, []string{txLatency})
	Timing(ComputeBudgetExecutionInstructions, r.ComputeBudgetExecutionInstructions.SumNanoseconds, []string{txLatency})
	Timing(AccountsFromTx, r.AccountsFromTx.SumNanoseconds, []string{txLatency})
	Timing(PreBalanceDivergenceCheck, r.PreBalanceDivergenceCheck.SumNanoseconds, []string{txLatency})
	Timing(CalcAndDeductFees, r.CalcAndDeductFees.SumNanoseconds, []string{txLatency})
	Timing(ReadRentSysvar, r.ReadRentSysvar.SumNanoseconds, []string{txLatency})
	Timing(PreTxRentStates, r.PreTxRentStates.SumNanoseconds, []string{txLatency})
	Timing(IxLoop, r.IxLoop.SumNanoseconds, []string{txLatency})
	Timing(PostTxRentStates, r.PostTxRentStates.SumNanoseconds, []string{txLatency})
	Timing(PostBalanceDivergenceCheck, r.PostBalanceDivergenceCheck.SumNanoseconds, []string{txLatency})
	Timing(TxUpdateAccounts, r.TxUpdateAccounts.SumNanoseconds, []string{txLatency})
	Timing(GetNextIxCtx, r.GetNextIxCtx.SumNanoseconds, []string{ixLatency})
	Timing(NextIxCtxConfigure, r.NextIxCtxConfigure.SumNanoseconds, []string{ixLatency})
	Timing(IxPush, r.IxPush.SumNanoseconds, []string{ixLatency})
	Timing(IxPop, r.IxPop.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxResolveNativeProgram, r.ExecIxResolveNativeProgram.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramSystem, r.ExecIxNativeProgramSystem.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramStake, r.ExecIxNativeProgramStake.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramVote, r.ExecIxNativeProgramVote.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramComputeBudget, r.ExecIxNativeProgramComputeBudget.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramBpfLoader2, r.ExecIxNativeProgramBpfLoader2.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramBpfLoaderDeprecated, r.ExecIxNativeProgramBpfLoaderDeprecated.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramBpfLoaderUpgradeable, r.ExecIxNativeProgramBpfLoaderUpgradeable.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramZkElgamalProof, r.ExecIxNativeProgramZkElgamalProof.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramEd25519Precompile, r.ExecIxNativeProgramEd25519Precompile.SumNanoseconds, []string{ixLatency})
	Timing(ExecIxNativeProgramSecp256kPrecompile, r.ExecIxNativeProgramSecp256kPrecompile.SumNanoseconds, []string{ixLatency})
	Timing(FixupInstructionsSysvarAccount, r.FixupInstructionsSysvarAccount.SumNanoseconds, []string{ixLatency})
	Timing(InstructionAccountsFromAccountMetas, r.InstructionAccountsFromAccountMetas.SumNanoseconds, []string{ixLatency})
	Timing(SbpfInterpreterNew, r.SbpfInterpreterNew.SumNanoseconds, []string{sbpfLatency})
	Timing(SbpfInterpreterRun, r.SbpfInterpreterRun.SumNanoseconds, []string{sbpfLatency})
	Timing(AddProgramToCache, r.AddProgramToCache.SumNanoseconds, []string{sbpfLatency})
	Timing(GetProgramAccount, r.GetProgramAccount.SumNanoseconds, []string{sbpfLatency})
	Timing(GetProgramDataCached, r.GetProgramDataCached.SumNanoseconds, []string{sbpfLatency})
	Timing(GetProgramDataUncachedAccountsDb, r.GetProgramDataUncachedAccountsDb.SumNanoseconds, []string{sbpfLatency})
	Timing(GetProgramDataUncachedAccounts, r.GetProgramDataUncachedAccounts.SumNanoseconds, []string{sbpfLatency})
	Timing(GetProgramDataUncachedMarshal, r.GetProgramDataUncachedMarshal.SumNanoseconds, []string{sbpfLatency})
}
