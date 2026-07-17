package sealevel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogRecorder_Unbounded(t *testing.T) {
	var r LogRecorder
	r.Log("one")
	r.Log("two")
	assert.Equal(t, []string{"one", "two"}, r.Logs)
}

// TestLogRecorder_BytesLimit mirrors Agave's LogCollector::log semantics
// (svm-log-collector/src/lib.rs): a message that would reach the byte limit
// is dropped and replaced by a single "Log truncated" marker without
// advancing bytesWritten. In this sequence every later message also overflows
// the limit, so nothing after the marker is recorded.
func TestLogRecorder_BytesLimit(t *testing.T) {
	limit := uint64(10)
	r := LogRecorder{BytesLimit: &limit}

	r.Log("12345")     // 5 bytes written, under the limit
	r.Log("1234")      // 9 bytes written, still under
	r.Log("x")         // would reach 10 >= limit: truncate, bytesWritten stays 9
	r.Log("more")      // 9+4 >= limit: dropped, no second marker
	r.Log("even more") // dropped

	assert.Equal(t, []string{"12345", "1234", "Log truncated"}, r.Logs)
}

func TestLogRecorder_BytesLimit_FirstMessageTooLarge(t *testing.T) {
	limit := uint64(4)
	r := LogRecorder{BytesLimit: &limit}
	r.Log("hello")
	assert.Equal(t, []string{"Log truncated"}, r.Logs)
}

// TestLogRecorder_BytesLimit_RecordsAfterMarker pins the Agave behavior the
// earlier semantics got wrong: an over-limit message pushes "Log truncated"
// ONCE and is dropped WITHOUT advancing bytesWritten, so later messages that
// fit under the limit are still recorded - after the marker.
func TestLogRecorder_BytesLimit_RecordsAfterMarker(t *testing.T) {
	limit := uint64(100)
	r := LogRecorder{BytesLimit: &limit}

	big := strings.Repeat("a", 200)
	r.Log("0123456789") // 10 bytes written, far below the limit
	r.Log(big)          // 10+200 >= 100: marker, bytesWritten stays 10
	r.Log("small")      // 10+5 < 100: still recorded
	r.Log(big)          // dropped again, and NO second marker
	r.Log("tiny")       // 15+4 < 100: still recorded

	assert.Equal(t, []string{"0123456789", "Log truncated", "small", "tiny"}, r.Logs)
}

func TestLogRecorder_BytesLimit_ExactBoundaryUnreachable(t *testing.T) {
	// Agave compares with >=, so bytesWritten can never reach the limit
	// exactly: a message landing exactly on the limit is truncated.
	limit := uint64(10)
	r := LogRecorder{BytesLimit: &limit}
	r.Log("123456789") // 9 < 10: recorded
	r.Log("x")         // 9+1 >= 10: marker
	r.Log("y")         // still 9+1 >= 10: dropped, no second marker
	assert.Equal(t, []string{"123456789", "Log truncated"}, r.Logs)
}
