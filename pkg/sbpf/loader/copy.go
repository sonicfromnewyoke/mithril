package loader

import (
	"debug/elf"
	"fmt"
	"io"

	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
)

type sectionMapping struct {
	src addrRange
	dst addrRange
}

// The following ELF loading rules seem mostly arbitrary.
// For the sake of cleanliness, this loader doesn't process
// some badly malformed ELFs that would pass on Solana mainnet.
// The Solana protocol is being improved in this area.

// copy allocates program buffers and copies ELF contents.
func (l *Loader) copy() error {
	if l.enableStricterElfHeaders() {
		return l.copyStrict()
	}

	l.progRange = newAddrRange()
	l.rodatas = make([]addrRange, 0, 4)
	l.rodataMappings = make([]sectionMapping, 0, 4)
	if err := l.getText(); err != nil {
		return err
	}
	if err := l.mapSections(); err != nil {
		return err
	}
	if err := l.copySections(); err != nil {
		return err
	}
	return nil
}

func (l *Loader) copyStrict() error {
	l.progRange = addrRange{min: 0, max: l.rodataRange.len()}
	l.rodatas = nil
	l.rodataMappings = nil

	l.program = make([]byte, l.rodataRange.len())
	if l.rodataRange.len() != 0 {
		if err := l.readSection(l.rodataRange, l.program); err != nil {
			return err
		}
	}

	l.text = make([]byte, l.textRange.len())
	if err := l.readSection(l.textRange, l.text); err != nil {
		return err
	}

	return nil
}

// getText remembers the range of .text in the program buffer
func (l *Loader) getText() error {
	if err := l.checkSectionAddrs(l.shText); err != nil {
		return fmt.Errorf("invalid .text: %w", err)
	}

	textSize := (l.shText.Size / 8) * 8

	l.textRange = addrRange{min: l.shText.Off, max: l.shText.Off + textSize}
	return nil
}

// mapRodataLike reserves ranges for sections in the program buffer
func (l *Loader) mapSections() error {
	// Walk all non-standard rodata sections
	iter := l.newShTableIter()
	for iter.Next() && iter.Err() == nil {
		i, sh := iter.Index(), iter.Item()

		// Skip standard sections
		sectionName, err := l.getString(&l.shShstrtab, sh.Name, maxSectionNameLen)
		if err != nil {
			return fmt.Errorf("getString: %w", err)
		}
		switch sectionName {
		case ".text", ".rodata", ".data.rel.ro", ".eh_frame":
			// ok
		default:
			continue
		}

		if err := l.checkSectionAddrs(&sh); err != nil {
			return fmt.Errorf("invalid rodata-like section %d: %w", i, err)
		}

		// Section overlap check & bounds tracking
		src := addrRange{min: sh.Off, max: sh.Off + sh.Size}
		dst := l.sectionProgramRange(sectionName, &sh)
		if dst.len() == 0 {
			continue
		}
		if l.progRange.containsRange(dst) {
			// TODO rbpf probably doesn't have this restriction
			return fmt.Errorf("rodata section %d overlaps with other section", i)
		}
		l.progRange.insert(dst)

		if sectionName != ".text" {
			l.rodatas = append(l.rodatas, dst)
			l.rodataMappings = append(l.rodataMappings, sectionMapping{src: src, dst: dst})
		}
	}
	return iter.Err()
}

func (l *Loader) sectionProgramRange(sectionName string, sh *elf.Section64) addrRange {
	src := addrRange{min: sh.Off, max: sh.Off + sh.Size}
	return src
}

func (l *Loader) checkSectionAddrs(sh *elf.Section64) error {
	if sh.Size > l.fileSize {
		return io.ErrUnexpectedEOF
	}
	if (sh.Flags&uint64(elf.SHF_ALLOC)) != 0 && sh.Addr != sh.Off {
		return fmt.Errorf("section physical address out-of-place")
	}

	// Ensure section within VM program range
	vaddr := clampAddUint64(sbpf.VaddrProgram, sh.Addr)
	vaddrEnd := vaddr + sh.Size
	if vaddrEnd < vaddr || vaddrEnd > sbpf.VaddrStack {
		return fmt.Errorf("section virtual address out-of-bounds")
	}

	return nil
}

// copySections copies text and rodata-like sections from the ELF into VM memory.
func (l *Loader) copySections() error {
	if l.progRange.len() == 0 {
		// TODO what is the correct behavior here?
		return fmt.Errorf("program is empty (???)")
	}
	l.progRange.extendToFit(0)

	// Allocate!
	programSize := l.fileSize
	if l.progRange.max > programSize {
		programSize = l.progRange.max
	}
	l.program = make([]byte, programSize)

	// Read data from ELF file
	for _, section := range l.rodataMappings {
		if err := l.copySection(section.src, section.dst); err != nil {
			return err
		}
	}

	// Special sub-slice for text
	if err := l.copySection(l.textRange, l.textRange); err != nil {
		return err
	}
	l.text = l.getRange(l.textRange)

	return nil
}

func (l *Loader) copySection(src addrRange, dst addrRange) error {
	return l.readSection(src, l.program[dst.min:dst.max])
}

func (l *Loader) readSection(src addrRange, dst []byte) (err error) {
	off, size := int64(src.min), int64(src.len())
	rd := io.NewSectionReader(l.rd, off, size)
	_, err = io.ReadFull(rd, dst)
	return
}

func (l *Loader) getRange(section addrRange) []byte {
	return l.program[section.min:section.max]
}
