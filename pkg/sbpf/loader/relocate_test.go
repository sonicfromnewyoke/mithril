package loader

import (
	"debug/elf"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf/sbpfver"
	"github.com/stretchr/testify/assert"
)

func TestSymbolHash_Entrypoint(t *testing.T) {
	assert.Equal(t, sbpf.EntrypointHash, sbpf.SymbolHash("entrypoint"))
}

func TestSectionProgramRangeUsesFileOffsets(t *testing.T) {
	l := &Loader{eh: elf.Header64{Flags: sbpfver.SbpfVersionV2}}
	rodata := elf.Section64{Addr: 128, Off: 128, Size: 4}
	text := elf.Section64{Addr: 64, Off: 64, Size: 8}

	assert.Equal(t, addrRange{min: 128, max: 132}, l.sectionProgramRange(".rodata", &rodata))
	assert.Equal(t, addrRange{min: 64, max: 72}, l.sectionProgramRange(".text", &text))
}
