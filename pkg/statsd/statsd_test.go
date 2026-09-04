package statsd

import (
	"testing"
	"time"

	mithrilmetrics "github.com/sonicfromnewyoke/mithril/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
)

func TestInitializeStatsdMetrics(t *testing.T) {
	//metricsCollection := InitializeStatsdMetrics()
	// Check that PreprocessBlock is in histograms
	if _, ok := metricsCollection.histograms[PreprocessBlock]; !ok {
		t.Errorf("Expected PreprocessBlock to be in histograms")
	}
	// Check that Epoch is in gauges
	if _, ok := metricsCollection.gauges[Epoch]; !ok {
		t.Errorf("Expected Epoch to be in gauges")
	}

	// Check that SnapshotTarBytesRead is in counters
	if _, ok := metricsCollection.counters[SnapshotTarBytesRead]; !ok {
		t.Errorf("Expected SnapshotTarBytesRead to be in counters")
	}
}

func TestThatEveryMetricHasLabelsAndType(t *testing.T) {
	for metric := range MetricToType {
		if _, ok := MetricToLabels[metric]; !ok {
			t.Errorf("Metric %v is missing labels in MetricToLabels", metric)
		}
	}

	for metric := range MetricToLabels {
		if _, ok := MetricToType[metric]; !ok {
			t.Errorf("Metric %v is missing type in MetricToType", metric)
		}
	}
}

func TestCountWithoutLabelValues(t *testing.T) {

	val := float64(5)
	Count(SnapshotTarBytesRead, int64(val), nil)
	counterVec := metricsCollection.counters[SnapshotTarBytesRead]
	metric, _ := counterVec.GetMetricWithLabelValues([]string{}...)
	// check that the counter value is 5
	m := &dto.Metric{}
	metric.Write(m)
	assert.Equal(t, m.GetCounter().GetValue(), val, "Counter value should be 5")
}

func TestCountWithLabelValues(t *testing.T) {
	val := float64(10)
	Count(TestCount, int64(val), []string{"testLabel"})
	counterVec := metricsCollection.counters[TestCount]
	metric, _ := counterVec.GetMetricWithLabelValues("testLabel")
	// check that the counter value is 10
	m := &dto.Metric{}
	metric.Write(m)
	assert.Equal(t, m.GetCounter().GetValue(), val, "Counter value should be 10")
}

func TestGaugeWithLabelValues(t *testing.T) {
	val := float64(15)
	Gauge(SnapshotWorkerPoolUtilization, val, []string{"testLabel"})
	gaugeVec := metricsCollection.gauges[SnapshotWorkerPoolUtilization]
	metric, _ := gaugeVec.GetMetricWithLabelValues([]string{"testLabel"}...)
	// check that the gauge value is 15
	m := &dto.Metric{}
	metric.Write(m)
	assert.Equal(t, m.GetGauge().GetValue(), val, "Gauge value should be 15")
}

func TestGaugeWithoutLabelValues(t *testing.T) {
	val := float64(20)
	Gauge(Epoch, val, nil)
	gaugeVec := metricsCollection.gauges[Epoch]
	metric, _ := gaugeVec.GetMetricWithLabelValues([]string{}...)
	// check that the gauge value is 20
	m := &dto.Metric{}
	metric.Write(m)
	assert.Equal(t, m.GetGauge().GetValue(), val, "Gauge value should be 20")
}

func TestTimingWithLabelValues(t *testing.T) {
	val := uint64(25)
	Timing(PreprocessBlock, val, []string{"testLabel"})
	histogramVec := metricsCollection.histograms[PreprocessBlock]
	metric, _ := histogramVec.GetMetricWithLabelValues([]string{"testLabel"}...)
	// check that the histogram value is 25
	mcollector := metric.(prometheus.Metric)
	m := &dto.Metric{}
	mcollector.Write(m)

	assert.Equal(t, m.GetHistogram().GetSampleCount(), uint64(1), "Histogram sample count should be 1")
	assert.Equal(t, uint64(m.GetHistogram().GetSampleSum()), val, "Histogram sample sum should be 25")
}

func TestTimingWithoutLabelValues(t *testing.T) {
	val := uint64(30)
	Timing(TaskIndexEntryCommitterLatency, val, []string{})
	histogramVec := metricsCollection.histograms[TaskIndexEntryCommitterLatency]
	metric, _ := histogramVec.GetMetricWithLabelValues([]string{}...)
	// check that the histogram value is 30
	mcollector := metric.(prometheus.Metric)
	m := &dto.Metric{}
	mcollector.Write(m)

	assert.Equal(t, m.GetHistogram().GetSampleCount(), uint64(1), "Histogram sample count should be 1")
	assert.Equal(t, uint64(m.GetHistogram().GetSampleSum()), val, "Histogram sample sum should be 30")
}

func TestBlockReplayMetrics(t *testing.T) {

	// Instantiate mithrilmetrics.BlockReplay
	blockReplay := &mithrilmetrics.BlockReplay{
		Slot: 12345,
	}

	// Add some timings
	blockReplay.PreprocessBlock.AddTiming(time.Millisecond * 100)
	blockReplay.LoadBlockAccounts.AddTiming(time.Millisecond * 200)
	blockReplay.TxLoop.AddTiming(time.Millisecond * 300)
	// Sanity test to ensure that the function completes without error
	SendBlockReplayMetrics(*blockReplay)
}
