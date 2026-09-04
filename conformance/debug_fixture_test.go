package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf/loader"
	sealevelPkg "github.com/sonicfromnewyoke/mithril/pkg/sealevel"
	"github.com/gagliardetto/solana-go"
)

func TestDebugDumpVMProgramFixture(t *testing.T) {
	filter := os.Getenv("MITHRIL_CONFORMANCE_DUMP_FIXTURE")
	if filter == "" {
		t.Skip("set MITHRIL_CONFORMANCE_DUMP_FIXTURE")
	}

	basePath := "test-vectors/instr/fixtures/vm-programs"
	entries, err := os.ReadDir(basePath)
	if err != nil {
		t.Skipf("test-vectors not available: %v", err)
	}

	var fixtures []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".fix") && strings.Contains(entry.Name(), filter) {
			fixtures = append(fixtures, filepath.Join(basePath, entry.Name()))
		}
	}
	sort.Strings(fixtures)
	if len(fixtures) == 0 {
		t.Fatalf("no fixture matching %q", filter)
	}

	data, err := os.ReadFile(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := unmarshalFiredancerInstrFixture(data)
	if err != nil {
		t.Fatal(err)
	}

	programID := solana.PublicKeyFromBytes(fixture.GetInput().GetProgramId())
	t.Logf("fixture=%s program_id=%s input_cu=%d output_cu=%d output_result=%d custom=%d data=%x", filepath.Base(fixtures[0]), programID, fixture.GetInput().GetCuAvail(), fixture.GetOutput().GetCuAvail(), fixture.GetOutput().GetResult(), fixture.GetOutput().GetCustomErr(), fixture.GetInput().GetData())
	t.Logf("%s", fixtureProgramSummary(fixture))
	for i, acct := range fixture.GetInput().GetAccounts() {
		key := solana.PublicKeyFromBytes(acct.GetAddress())
		owner := solana.PublicKeyFromBytes(acct.GetOwner())
		prefixLen := min(len(acct.GetData()), 8)
		prefix := acct.GetData()[:prefixLen]
		t.Logf("acct[%d] key=%s owner=%s exec=%v lamports=%d data_len=%d data_prefix=%x", i, key, owner, acct.GetExecutable(), acct.GetLamports(), len(acct.GetData()), prefix)
	}
	for i, acct := range fixture.GetInput().GetInstrAccounts() {
		t.Logf("instr_acct[%d] index=%d writable=%v signer=%v", i, acct.GetIndex(), acct.GetIsWritable(), acct.GetIsSigner())
	}

	execCtx, instrAccts, programIndices, err := newVMProgramExecCtxAndInstrAccts(fixture)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("features=%v", execCtx.Features.AllEnabled())

	if idxStr := os.Getenv("MITHRIL_CONFORMANCE_DUMP_PROGRAM_INDEX"); idxStr != "" {
		for i, acct := range fixture.GetInput().GetAccounts() {
			if idxStr == strconv.Itoa(i) {
				programID = solana.PublicKeyFromBytes(acct.GetAddress())
				break
			}
		}
	}
	var programBytes []byte
	for _, acct := range fixture.GetInput().GetAccounts() {
		if solana.PublicKeyFromBytes(acct.GetAddress()) == programID {
			programBytes = acct.GetData()
			break
		}
	}
	if len(programBytes) == 0 {
		t.Fatalf("program account %s has no bytes", programID)
	}

	var program *sbpf.Program
	if os.Getenv("MITHRIL_CONFORMANCE_DUMP_NO_DISASM") == "" {
		syscalls := func(u uint32) (sbpf.Syscall, bool) {
			syscall, ok := sealevelPkg.Syscalls(&execCtx.Features, false, u)
			if !ok {
				return nil, false
			}
			return debugSyscall{t: t, hash: u, inner: syscall}, true
		}
		l, err := loader.NewLoaderWithSyscalls(programBytes, syscalls, false, &execCtx.Features)
		if err != nil {
			t.Fatalf("loader: %v", err)
		}
		program, err = l.Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}

		t.Logf("sbpf_version=%v entry=%d text_slots=%d funcs=%d", program.SbpfVersion, program.Entrypoint, len(program.Text), len(program.Funcs))
		t.Logf("verify_err=%v", program.Verify())
	}
	execCtx.RecordInnerInstructions = true
	execCtx.SetCurrentTopLevelInstr(0)
	runErr := execCtx.ProcessInstruction(fixture.GetInput().GetData(), instrAccts, programIndices)
	t.Logf("run_err=%v translated_result=%d remaining_cu=%d", runErr, instrResultFromErr(runErr), execCtx.ComputeMeter.Remaining())
	if recorder, ok := execCtx.Log.(*sealevelPkg.LogRecorder); ok {
		for i, log := range recorder.Logs {
			t.Logf("log[%d]=%s", i, log)
		}
	}
	for i, inner := range execCtx.InnerInstrs {
		t.Logf("inner[%d] stack=%d program_index=%d accounts=%v data=%x", i, inner.StackHeight, inner.ProgramIdIndex, inner.Accounts, inner.Data)
	}
	if program != nil {
		limit := len(program.Text)
		for pc := 0; pc < limit; pc++ {
			slot := program.Text[pc]
			t.Logf("%03d raw=%016x op=%02x dst=%d src=%d off=%d imm=%d uimm=%d", pc, uint64(slot), slot.Op(), slot.Dst(), slot.Src(), slot.Off(), slot.Imm(), slot.Uimm())
		}
	}
}

type debugSyscall struct {
	t     *testing.T
	hash  uint32
	inner sbpf.Syscall
}

func (d debugSyscall) Invoke(vm sbpf.VM, r1, r2, r3, r4, r5 uint64) (uint64, error) {
	before := vm.ComputeMeter().Remaining()
	r0, err := d.inner.Invoke(vm, r1, r2, r3, r4, r5)
	after := vm.ComputeMeter().Remaining()
	d.t.Logf("syscall hash=0x%08x args=[0x%x 0x%x 0x%x 0x%x 0x%x] before_cu=%d after_cu=%d r0=%d err=%v", d.hash, r1, r2, r3, r4, r5, before, after, r0, err)
	return r0, err
}

func (d debugSyscall) String() string {
	return fmt.Sprintf("debugSyscall(0x%08x)", d.hash)
}
