package sealevel

import (
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/stretchr/testify/assert"
)

func TestSyscalls_Blake3RequiresFeature(t *testing.T) {
	ft := features.NewFeaturesDefault()

	_, ok := Syscalls(ft, false, sbpf.SymbolHash("sol_blake3"))
	assert.False(t, ok)

	ft.EnableFeature(features.Blake3SyscallEnabled, 0)

	_, ok = Syscalls(ft, false, sbpf.SymbolHash("sol_blake3"))
	assert.True(t, ok)
}
