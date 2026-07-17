package sealevel

import "github.com/Overclock-Validator/mithril/pkg/safemath"

type Logger interface {
	Log(s string)
}

type LogRecorder struct {
	Logs []string

	// BytesLimit, when non-nil, caps the total message bytes recorded,
	// mirroring Agave's LogCollector::log (svm-log-collector/src/lib.rs): a
	// message whose length would make bytesWritten reach the limit (>=) is
	// dropped WITHOUT advancing bytesWritten, and a single "Log truncated"
	// marker is appended in its place. Later messages that still fit
	// (bytesWritten + len < limit) ARE recorded, so logs can legitimately
	// contain entries after the marker; the truncated flag only suppresses
	// duplicate markers.
	BytesLimit *uint64

	bytesWritten uint64
	truncated    bool
}

// stableLog emits an Agave stable_log framing line ("Program ... invoke",
// "Program ... success", "Program ... failed: ...", "Program ... consumed ...",
// "Program return: ..."). It is nil-safe so execution contexts constructed
// without a logger (as in various unit tests) do not panic.
func (execCtx *ExecutionCtx) stableLog(s string) {
	if execCtx.Log != nil {
		execCtx.Log.Log(s)
	}
}

func (r *LogRecorder) Log(s string) {
	if r.BytesLimit == nil {
		r.Logs = append(r.Logs, s)
		return
	}
	bytesWritten := safemath.SaturatingAddU64(r.bytesWritten, uint64(len(s)))
	if bytesWritten >= *r.BytesLimit {
		if !r.truncated {
			r.truncated = true
			r.Logs = append(r.Logs, "Log truncated")
		}
		return
	}
	r.bytesWritten = bytesWritten
	r.Logs = append(r.Logs, s)
}
