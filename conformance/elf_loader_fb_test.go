package conformance

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf/loader"
	sealevelPkg "github.com/sonicfromnewyoke/mithril/pkg/sealevel"
)

func parseFeatureIds(featureIds []uint64) *features.Features {
	f := features.NewFeaturesDefault()
	for _, ftr := range featureIds {
		for _, featureGate := range features.AllFeatureGates {
			featureIdInt := binary.LittleEndian.Uint64(featureGate.Address[:8])
			if featureIdInt == ftr {
				f.EnableFeature(featureGate, 0)
			}
		}
	}
	return f
}

func parsePBFeatures(pbFeatures *FeatureSet) *features.Features {
	if pbFeatures == nil {
		return features.NewFeaturesDefault()
	}
	return parseFeatureIds(pbFeatures.GetFeatures())
}

func TestConformance_ElfLoader_Firedancer(t *testing.T) {
	basePath := "test-vectors/elf_loader/fixtures"

	entries, err := os.ReadDir(basePath)
	if err != nil {
		t.Skipf("test-vectors not available: %v", err)
	}

	var fixtures []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".fix") {
			fixtures = append(fixtures, filepath.Join(basePath, entry.Name()))
		}
	}

	if len(fixtures) == 0 {
		t.Skip("no .fix fixtures found")
	}

	t.Logf("Found %d ELF loader fixtures", len(fixtures))

	var (
		total       int
		passPass    int
		failFail    int
		falsePass   int
		falseFail   int
		panics      int
		parseErrors int
		entryMatch  int
		entryTotal  int
		textMatch   int
		textTotal   int
	)

	var failures []string
	var panicFixtures []string

	for _, fixturePath := range fixtures {
		total++
		name := filepath.Base(fixturePath)

		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Errorf("%s: read error: %v", name, err)
			continue
		}

		fixture, err := unmarshalFiredancerELFLoaderFixture(data)
		if err != nil {
			failures = append(failures, fmt.Sprintf("PARSE_ERROR %s: %v", name, err))
			parseErrors++
			continue
		}

		if len(fixture.ElfData) == 0 {
			failures = append(failures, fmt.Sprintf("PARSE_ERROR %s: missing ELF data", name))
			parseErrors++
			continue
		}

		output := fixture.Output
		fixtureExpectsSuccess := output.expectsSuccess()

		f := parsePBFeatures(fixture.Features)

		syscalls := sbpf.SyscallRegistry(func(hash uint32) (sbpf.Syscall, bool) {
			return sealevelPkg.Syscalls(f, fixture.DeployChecks, hash)
		})

		var program *sbpf.Program
		var loadErr error
		var didPanic bool

		func() {
			defer func() {
				if r := recover(); r != nil {
					didPanic = true
					panics++
					panicFixtures = append(panicFixtures, fmt.Sprintf("PANIC %s: %v", name, r))
				}
			}()

			l, err := loader.NewLoaderWithSyscalls(fixture.ElfData, syscalls, fixture.DeployChecks, f)
			if err != nil {
				loadErr = err
				return
			}
			program, loadErr = l.Load()
		}()

		if didPanic {
			continue
		}

		if loadErr == nil && fixtureExpectsSuccess {
			passPass++

			if output != nil {
				if output.HasEntryPc {
					entryTotal++
					if program.Entrypoint == output.EntryPc {
						entryMatch++
					} else {
						failures = append(failures, fmt.Sprintf("ENTRY_MISMATCH %s: got=%d want=%d", name, program.Entrypoint, output.EntryPc))
					}
				}

				if output.HasTextCnt {
					textTotal++
					if uint64(len(program.Text)) == output.TextCnt {
						textMatch++
					} else {
						failures = append(failures, fmt.Sprintf("TEXT_CNT_MISMATCH %s: got=%d want=%d", name, len(program.Text), output.TextCnt))
					}
				}
			}
		} else if loadErr != nil && !fixtureExpectsSuccess {
			failFail++
		} else if loadErr == nil && !fixtureExpectsSuccess {
			falsePass++
			failures = append(failures, fmt.Sprintf("FALSE_PASS %s: loaded OK but fixture expects failure", name))
		} else {
			falseFail++
			if output != nil {
				failures = append(failures, fmt.Sprintf("FALSE_FAIL %s: %v (entry_pc=%d text_cnt=%d err_code=%d)", name, loadErr, output.EntryPc, output.TextCnt, output.ErrCode))
			} else {
				failures = append(failures, fmt.Sprintf("FALSE_FAIL %s: %v", name, loadErr))
			}
		}
	}

	sort.Strings(failures)

	t.Logf("\n=== ELF Loader Conformance Results ===")
	t.Logf("Total fixtures:     %d", total)
	t.Logf("Parse errors:       %d", parseErrors)
	t.Logf("Both pass:          %d", passPass)
	t.Logf("Both fail:          %d", failFail)
	t.Logf("False pass (bad):   %d (mithril loads, fixture rejects)", falsePass)
	t.Logf("False fail (bad):   %d (mithril rejects, fixture loads)", falseFail)
	t.Logf("Panics (crash bug): %d", panics)
	t.Logf("Entry PC match:     %d / %d", entryMatch, entryTotal)
	t.Logf("Text count match:   %d / %d", textMatch, textTotal)

	if len(panicFixtures) > 0 {
		t.Logf("\n=== PANICS (crash bugs - highest priority) ===")
		for _, p := range panicFixtures {
			t.Logf("  %s", p)
		}
	}

	if len(failures) > 0 {
		t.Logf("\n=== First 50 failures ===")
		limit := 50
		if len(failures) < limit {
			limit = len(failures)
		}
		for _, f := range failures[:limit] {
			t.Logf("  %s", f)
		}
	}

	// Report pass rate
	agree := passPass + failFail
	disagree := falsePass + falseFail
	passRate := float64(agree) / float64(agree+disagree) * 100
	t.Logf("\nConformance rate: %.1f%% (%d/%d)", passRate, agree, agree+disagree)

	if panics > 0 {
		t.Errorf("CRITICAL: %d fixtures caused panics in the loader", panics)
	}
	if disagree > 0 {
		t.Errorf("%d ELF loader acceptance disagreements found", disagree)
	}
	if parseErrors > 0 {
		t.Errorf("%d ELF loader fixture parse errors found", parseErrors)
	}
	if entryMatch != entryTotal {
		t.Errorf("%d ELF loader entry PC mismatches found", entryTotal-entryMatch)
	}
	if textMatch != textTotal {
		t.Errorf("%d ELF loader text count mismatches found", textTotal-textMatch)
	}
}
