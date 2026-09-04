package duration

import (
	"fmt"
	"math"

	"github.com/sonicfromnewyoke/mithril/pkg/safemath"
)

type Duration struct {
	Secs  uint64
	Nanos uint32
}

const nanosPerSec = 1000000000

var durationMax = Duration{Secs: 18446744073709551615, Nanos: 999_999_999}

func NewDuration(secs uint64, nanos uint32) Duration {
	if nanos < nanosPerSec {
		return Duration{Secs: secs, Nanos: nanos}
	} else {
		var err error
		secs, err = safemath.CheckedAddU64(secs, uint64(nanos/nanosPerSec))
		if err != nil {
			panic("overflow")
		}
		nanos = nanos % nanosPerSec
		return Duration{Secs: secs, Nanos: nanos}
	}
}

func NewDurationFromNanos(nanos uint64) Duration {
	secs := nanos / nanosPerSec
	subsecNanos := uint32(nanos % nanosPerSec)
	subsecNanos = min(durationMax.Nanos, subsecNanos)
	return Duration{Secs: secs, Nanos: subsecNanos}
}

func NewDurationFromSecs(secs uint64) Duration {
	return Duration{Secs: secs}
}

func (duration Duration) CheckedMul(rhs uint32) (Duration, error) {
	totalNanos := uint64(duration.Nanos) * uint64(rhs)
	extraSecs := totalNanos / (nanosPerSec)
	nanos := uint32((totalNanos % (nanosPerSec)))

	s, err := safemath.CheckedMulU64(duration.Secs, uint64(rhs))
	if err != nil {
		return Duration{}, fmt.Errorf("overflow")
	}

	secs, err := safemath.CheckedAddU64(s, extraSecs)
	if err != nil {
		return Duration{}, fmt.Errorf("overflow")
	}

	return Duration{Secs: secs, Nanos: nanos}, nil
}

func (duration Duration) SaturatingMul(rhs uint32) Duration {
	result, err := duration.CheckedMul(rhs)
	if err != nil {
		return durationMax
	}
	return result
}

func (duration Duration) Div(rhs uint32) Duration {
	if rhs == 0 {
		panic("cannot divide by 0")
	}

	secs := duration.Secs / uint64(rhs)
	extraSecs := duration.Secs % uint64(rhs)
	nanos := duration.Nanos / rhs
	extraNanos := duration.Nanos % rhs
	nanos += uint32((extraSecs*nanosPerSec + uint64(extraNanos)) / (uint64(rhs)))
	return NewDuration(secs, nanos)
}

func (a Duration) Cmp(b Duration) int {
	if a.Secs < b.Secs {
		return -1
	} else if a.Secs > b.Secs {
		return 1
	}
	if a.Nanos < b.Nanos {
		return -1
	} else if a.Nanos > b.Nanos {
		return 1
	}
	return 0
}

func (a Duration) Lt(b Duration) bool { return a.Cmp(b) < 0 }
func (a Duration) Gt(b Duration) bool { return a.Cmp(b) > 0 }

func (a Duration) SaturatingAdd(b Duration) Duration {
	secs := a.Secs + b.Secs
	nanos := a.Nanos + b.Nanos

	if nanos >= nanosPerSec {
		nanos -= nanosPerSec
		if secs < math.MaxUint64 {
			secs++
		} else {
			nanos = nanosPerSec - 1
			return Duration{Secs: math.MaxUint64, Nanos: nanos}
		}
	}

	if secs < a.Secs || secs < b.Secs {
		return Duration{Secs: math.MaxUint64, Nanos: nanosPerSec - 1}
	}

	return Duration{Secs: secs, Nanos: nanos}
}

func (a Duration) SaturatingSub(b Duration) Duration {
	if a.Lt(b) {
		return Duration{Secs: 0, Nanos: 0}
	}

	secs := a.Secs
	nanos := int64(a.Nanos) - int64(b.Nanos)

	if nanos < 0 {
		if secs > 0 {
			secs--
			nanos += nanosPerSec
		} else {
			return Duration{Secs: 0, Nanos: 0}
		}
	}

	secs -= b.Secs
	return Duration{Secs: secs, Nanos: uint32(nanos)}
}
