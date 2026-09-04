package loader

import (
	"debug/elf"
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/stretchr/testify/require"
)

func strictV3Features() *features.Features {
	f := features.NewFeaturesDefault()
	f.EnableFeature(features.EnableSbpfV3DeploymentAndExecution, 0)
	return f
}

func strictV3ELF(t *testing.T, rodata []byte, text []sbpf.Slot, mutate func([]byte)) []byte {
	t.Helper()

	textBytes := make([]byte, len(text)*sbpf.SlotSize)
	for i, slot := range text {
		binary.LittleEndian.PutUint64(textBytes[i*sbpf.SlotSize:], uint64(slot))
	}

	phnum := uint16(1)
	if rodata != nil {
		phnum = 2
	}
	phTableEnd := uint64(ehLen) + uint64(phnum)*phEntLen
	rodataOffset := phTableEnd
	textOffset := phTableEnd + uint64(len(rodata))
	fileSize := textOffset + uint64(len(textBytes))
	buf := make([]byte, fileSize)

	copy(buf[0:4], []byte{0x7f, 'E', 'L', 'F'})
	buf[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	buf[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	buf[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	buf[elf.EI_OSABI] = byte(elf.ELFOSABI_NONE)

	binary.LittleEndian.PutUint16(buf[16:18], uint16(elf.ET_DYN))
	binary.LittleEndian.PutUint16(buf[18:20], uint16(elf.EM_BPF))
	binary.LittleEndian.PutUint32(buf[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(buf[24:32], sbpf.VaddrProgram)
	binary.LittleEndian.PutUint64(buf[32:40], ehLen)
	binary.LittleEndian.PutUint32(buf[48:52], 3)
	binary.LittleEndian.PutUint16(buf[52:54], ehLen)
	binary.LittleEndian.PutUint16(buf[54:56], phEntLen)
	binary.LittleEndian.PutUint16(buf[56:58], phnum)

	phoff := ehLen
	if rodata != nil {
		writeProgHeader(buf[phoff:], uint32(elf.PT_LOAD), progFlagR, rodataOffset, 0, uint64(len(rodata)))
		phoff += phEntLen
		copy(buf[int(rodataOffset):int(textOffset)], rodata)
	}
	writeProgHeader(buf[phoff:], uint32(elf.PT_LOAD), progFlagX, textOffset, sbpf.VaddrProgram, uint64(len(textBytes)))
	copy(buf[int(textOffset):], textBytes)

	if mutate != nil {
		mutate(buf)
	}
	return buf
}

func writeProgHeader(buf []byte, typ uint32, flags uint32, offset uint64, vaddr uint64, size uint64) {
	binary.LittleEndian.PutUint32(buf[0:4], typ)
	binary.LittleEndian.PutUint32(buf[4:8], flags)
	binary.LittleEndian.PutUint64(buf[8:16], offset)
	binary.LittleEndian.PutUint64(buf[16:24], vaddr)
	binary.LittleEndian.PutUint64(buf[24:32], vaddr)
	binary.LittleEndian.PutUint64(buf[32:40], size)
	binary.LittleEndian.PutUint64(buf[40:48], size)
}

func TestStrictV3LoaderUsesProgramHeaders(t *testing.T) {
	rodata := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	text := []sbpf.Slot{sbpf.Slot(sbpf.OpExit)}
	elfBytes := strictV3ELF(t, rodata, text, nil)

	l, err := NewLoaderWithSyscalls(elfBytes, nil, true, strictV3Features())
	require.NoError(t, err)
	program, err := l.Load()
	require.NoError(t, err)

	require.Equal(t, sbpf.VaddrProgram, program.TextVA)
	require.Equal(t, rodata, program.RO)
	require.Equal(t, text, program.Text)
	require.Equal(t, uint64(0), program.Entrypoint)
	require.NoError(t, program.Verify())
}

func TestStrictV3LoaderAllowsMissingRodataProgramHeader(t *testing.T) {
	text := []sbpf.Slot{sbpf.Slot(sbpf.OpExit)}
	elfBytes := strictV3ELF(t, nil, text, nil)

	l, err := NewLoaderWithSyscalls(elfBytes, nil, true, strictV3Features())
	require.NoError(t, err)
	program, err := l.Load()
	require.NoError(t, err)

	require.Empty(t, program.RO)
	require.Equal(t, sbpf.VaddrProgram, program.TextVA)
	require.Equal(t, text, program.Text)
}

func TestStrictV3LoaderDoesNotRequireDynElfType(t *testing.T) {
	text := []sbpf.Slot{sbpf.Slot(sbpf.OpExit)}
	elfBytes := strictV3ELF(t, nil, text, func(buf []byte) {
		binary.LittleEndian.PutUint16(buf[16:18], uint16(elf.ET_EXEC))
	})

	l, err := NewLoaderWithSyscalls(elfBytes, nil, true, strictV3Features())
	require.NoError(t, err)
	_, err = l.Load()
	require.NoError(t, err)
}

func TestStrictV3LoaderRejectsAgaveStrictProgramHeaderMismatches(t *testing.T) {
	text := []sbpf.Slot{sbpf.Slot(sbpf.OpExit)}

	tests := map[string]func([]byte){
		"wrong machine": func(buf []byte) {
			binary.LittleEndian.PutUint16(buf[18:20], uint16(EM_SBPF))
		},
		"wrong text offset": func(buf []byte) {
			textProgramHeader := ehLen
			binary.LittleEndian.PutUint64(buf[textProgramHeader+8:textProgramHeader+16], uint64(ehLen))
		},
		"wrong text vaddr": func(buf []byte) {
			textProgramHeader := ehLen
			binary.LittleEndian.PutUint64(buf[textProgramHeader+16:textProgramHeader+24], 0)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			elfBytes := strictV3ELF(t, nil, text, mutate)
			l, err := NewLoaderWithSyscalls(elfBytes, nil, true, strictV3Features())
			require.NoError(t, err)
			_, err = l.Load()
			require.Error(t, err)
		})
	}
}
