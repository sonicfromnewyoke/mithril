package turbine

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/klauspost/reedsolomon"
)

var (
	ErrDuplicateShred = errors.New("duplicate data shred")
	ErrSlotIncomplete = errors.New("slot incomplete")
	ErrSlotOverflow   = errors.New("slot has too many data shreds")
)

const (
	maxDataShredsPerSlot         = 64 * 1024
	maxRetainedIncompleteSlotLag = uint64(512)
	maxRetainedIncompleteSlotCap = 1024
	maxRetainedCompletedSlotLag  = uint64(512)
	repairObservedSlotLag        = uint64(1)
	repairScanSlotWindow         = uint64(96)
)

type SlotAssembler struct {
	mu                  sync.Mutex
	slots               map[uint64]*slotState
	completedSlots      map[uint64]struct{}
	encoders            map[fecLayout]reedsolomon.Encoder
	maxObservedSlot     uint64
	recoveredDataShreds uint64
	evictedSlots        uint64
	ignoredOldShreds    uint64
}

type SlotRepairRequest struct {
	Slot                  uint64
	MissingDataShreds     []uint32
	NeedHighestDataShred  bool
	HighestDataShredIndex uint32
}

type slotState struct {
	slot        uint64
	parentSlot  uint64
	shreds      map[uint32]*Shred
	fecSets     map[uint32]*fecState
	lastIndex   uint32
	haveLast    bool
	shredVer    uint16
	firstParent bool
}

type fecLayout struct {
	dataShreds   uint16
	codingShreds uint16
	shardSize    int
}

type fecState struct {
	slot        uint64
	fecSetIndex uint32
	data        map[uint32]*Shred
	coding      map[uint16]*Shred
	layout      fecLayout
	haveLayout  bool
	signature   [64]byte
	haveSig     bool
	dataVariant byte
	codeVariant byte
}

func NewSlotAssembler() *SlotAssembler {
	return &SlotAssembler{
		slots:          make(map[uint64]*slotState),
		completedSlots: make(map[uint64]struct{}),
		encoders:       make(map[fecLayout]reedsolomon.Encoder),
	}
}

func (a *SlotAssembler) AddPacket(packet []byte) (*block.Block, error) {
	shred, err := ParseShred(packet)
	if err != nil {
		if errors.Is(err, ErrCodingShredIgnored) {
			return nil, nil
		}
		return nil, err
	}
	return a.AddShred(shred)
}

func (a *SlotAssembler) AddShred(shred *Shred) (*block.Block, error) {
	if shred == nil {
		return nil, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if shred.Slot > a.maxObservedSlot {
		a.maxObservedSlot = shred.Slot
	}
	a.pruneOldSlotsLocked()
	if a.slotTooOldLocked(shred.Slot) {
		a.ignoredOldShreds++
		return nil, nil
	}
	if _, completed := a.completedSlots[shred.Slot]; completed {
		a.ignoredOldShreds++
		return nil, nil
	}

	state := a.slotState(shred.Slot, shred.Version)
	var err error
	switch shred.Type {
	case ShredTypeData:
		err = state.addDataShred(shred)
	case ShredTypeCode:
		err = state.addCodingShred(shred)
	default:
		return nil, nil
	}
	if err != nil {
		if errors.Is(err, ErrDuplicateShred) {
			return nil, nil
		}
		return nil, err
	}

	recovered, err := a.recoverFEC(state, shred.FECSetIndex)
	if err != nil {
		return nil, err
	}
	for _, recoveredShred := range recovered {
		err := state.addDataShred(recoveredShred)
		if err != nil && !errors.Is(err, ErrDuplicateShred) {
			return nil, err
		}
		if err == nil {
			a.recoveredDataShreds++
		}
	}

	if !state.complete() {
		return nil, nil
	}

	blk, err := state.block()
	if err != nil {
		return nil, err
	}
	delete(a.slots, shred.Slot)
	a.completedSlots[shred.Slot] = struct{}{}
	return blk, nil
}

func (a *SlotAssembler) slotState(slot uint64, version uint16) *slotState {
	state := a.slots[slot]
	if state != nil {
		return state
	}
	state = &slotState{
		slot:      slot,
		shreds:    make(map[uint32]*Shred),
		fecSets:   make(map[uint32]*fecState),
		shredVer:  version,
		lastIndex: ^uint32(0),
	}
	a.slots[slot] = state
	return state
}

func (a *SlotAssembler) slotTooOldLocked(slot uint64) bool {
	if a.maxObservedSlot <= maxRetainedIncompleteSlotLag {
		return false
	}
	return slot < a.maxObservedSlot-maxRetainedIncompleteSlotLag
}

func (a *SlotAssembler) pruneOldSlotsLocked() {
	if len(a.slots) > 0 && a.maxObservedSlot > maxRetainedIncompleteSlotLag {
		minSlot := a.maxObservedSlot - maxRetainedIncompleteSlotLag
		for slot := range a.slots {
			if slot < minSlot {
				delete(a.slots, slot)
				a.evictedSlots++
			}
		}
	}
	if a.maxObservedSlot > maxRetainedCompletedSlotLag {
		minSlot := a.maxObservedSlot - maxRetainedCompletedSlotLag
		for slot := range a.completedSlots {
			if slot < minSlot {
				delete(a.completedSlots, slot)
			}
		}
	}

	if len(a.slots) == 0 {
		return
	}
	for len(a.slots) > maxRetainedIncompleteSlotCap {
		var oldest uint64
		first := true
		for slot := range a.slots {
			if first || slot < oldest {
				oldest = slot
				first = false
			}
		}
		if first {
			return
		}
		delete(a.slots, oldest)
		a.evictedSlots++
	}
}

func (s *slotState) addDataShred(shred *Shred) error {
	if _, exists := s.shreds[shred.Index]; exists {
		return ErrDuplicateShred
	}
	if shred.Index >= maxDataShredsPerSlot {
		return fmt.Errorf("%w: slot %d shred index %d", ErrSlotOverflow, shred.Slot, shred.Index)
	}
	if s.shredVer != shred.Version {
		return fmt.Errorf("mixed shred versions for slot %d: %d and %d", shred.Slot, s.shredVer, shred.Version)
	}
	if !s.firstParent {
		s.parentSlot = shred.ParentSlot()
		s.firstParent = true
	} else if s.parentSlot != shred.ParentSlot() {
		return fmt.Errorf("mixed parent slots for slot %d: %d and %d", shred.Slot, s.parentSlot, shred.ParentSlot())
	}
	if err := s.addDataToFEC(shred); err != nil {
		return err
	}
	s.shreds[shred.Index] = shred
	if shred.LastInSlot() {
		s.haveLast = true
		s.lastIndex = shred.Index
	}
	return nil
}

func (s *slotState) repairRequest(maxMissing int) (SlotRepairRequest, bool) {
	req := SlotRepairRequest{Slot: s.slot}

	var maxObserved uint32
	haveData := false
	for index := range s.shreds {
		if !haveData || index > maxObserved {
			maxObserved = index
			haveData = true
		}
	}

	req.NeedHighestDataShred = !s.haveLast
	if haveData && maxObserved < maxDataShredsPerSlot-1 {
		req.HighestDataShredIndex = maxObserved + 1
	}

	missingThrough := maxObserved
	if s.haveLast {
		missingThrough = s.lastIndex
	}
	for index := uint32(0); index <= missingThrough && len(req.MissingDataShreds) < maxMissing; index++ {
		if s.shreds[index] == nil {
			req.MissingDataShreds = append(req.MissingDataShreds, index)
		}
		if index == maxDataShredsPerSlot-1 {
			break
		}
	}

	if len(req.MissingDataShreds) == 0 && !req.NeedHighestDataShred {
		return SlotRepairRequest{}, false
	}
	return req, true
}

func (s *slotState) addCodingShred(shred *Shred) error {
	if shred.NumDataShreds == 0 || shred.NumCodingShreds == 0 {
		return fmt.Errorf("invalid coding shred FEC layout for slot %d fec_set=%d: data=%d coding=%d", shred.Slot, shred.FECSetIndex, shred.NumDataShreds, shred.NumCodingShreds)
	}
	if shred.Position >= shred.NumCodingShreds {
		return fmt.Errorf("invalid coding shred position for slot %d fec_set=%d: position=%d coding=%d", shred.Slot, shred.FECSetIndex, shred.Position, shred.NumCodingShreds)
	}
	if s.shredVer != shred.Version {
		return fmt.Errorf("mixed shred versions for slot %d: %d and %d", shred.Slot, s.shredVer, shred.Version)
	}
	fec := s.fecSet(shred.FECSetIndex)
	if _, exists := fec.coding[shred.Position]; exists {
		return ErrDuplicateShred
	}
	if err := fec.acceptShred(shred); err != nil {
		return err
	}
	layout, err := shred.fecLayout()
	if err != nil {
		return err
	}
	if err := fec.setLayout(layout); err != nil {
		return err
	}
	fec.coding[shred.Position] = shred
	return nil
}

func (a *SlotAssembler) CompleteSlot(slot uint64) (*block.Block, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := a.slots[slot]
	if state == nil || !state.complete() {
		return nil, ErrSlotIncomplete
	}
	blk, err := state.block()
	if err != nil {
		return nil, err
	}
	delete(a.slots, slot)
	a.completedSlots[slot] = struct{}{}
	return blk, nil
}

func (a *SlotAssembler) ActiveSlots() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.slots)
}

func (a *SlotAssembler) RecoveredDataShreds() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.recoveredDataShreds
}

func (a *SlotAssembler) EvictedSlots() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.evictedSlots
}

func (a *SlotAssembler) IgnoredOldShreds() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ignoredOldShreds
}

func (a *SlotAssembler) RepairRequests(maxSlots int, maxMissingPerSlot int) []SlotRepairRequest {
	if maxSlots <= 0 {
		return nil
	}
	if maxMissingPerSlot <= 0 {
		maxMissingPerSlot = 1
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.maxObservedSlot <= repairObservedSlotLag {
		return nil
	}
	repairThrough := a.maxObservedSlot - repairObservedSlotLag
	start := uint64(0)
	if repairThrough > repairScanSlotWindow {
		start = repairThrough - repairScanSlotWindow
	}
	if a.maxObservedSlot > maxRetainedIncompleteSlotLag {
		minRetained := a.maxObservedSlot - maxRetainedIncompleteSlotLag
		if start < minRetained {
			start = minRetained
		}
	}

	requests := make([]SlotRepairRequest, 0, maxSlots)
	for slot := start; slot <= repairThrough && len(requests) < maxSlots; slot++ {
		if _, completed := a.completedSlots[slot]; completed {
			continue
		}
		state := a.slots[slot]
		if state == nil {
			requests = append(requests, SlotRepairRequest{
				Slot:                  slot,
				NeedHighestDataShred:  true,
				HighestDataShredIndex: 0,
			})
			continue
		}
		if req, ok := state.repairRequest(maxMissingPerSlot); ok {
			requests = append(requests, req)
		}
	}
	return requests
}

func (a *SlotAssembler) recoverFEC(state *slotState, fecSetIndex uint32) ([]*Shred, error) {
	fec := state.fecSets[fecSetIndex]
	if fec == nil || !fec.haveLayout {
		return nil, nil
	}
	layout := fec.layout
	if int(layout.dataShreds)+int(layout.codingShreds) == len(fec.data)+len(fec.coding) {
		return nil, nil
	}
	if len(fec.data)+len(fec.coding) < int(layout.dataShreds) {
		return nil, nil
	}

	shards := make([][]byte, int(layout.dataShreds)+int(layout.codingShreds))
	for idx, shred := range fec.data {
		if idx >= uint32(layout.dataShreds) {
			continue
		}
		shard, err := shred.erasureShard()
		if err != nil {
			return nil, err
		}
		shards[int(idx)] = append([]byte(nil), shard...)
	}
	for pos, shred := range fec.coding {
		if pos >= layout.codingShreds {
			continue
		}
		shard, err := shred.erasureShard()
		if err != nil {
			return nil, err
		}
		shards[int(layout.dataShreds)+int(pos)] = append([]byte(nil), shard...)
	}
	encoder, err := a.fecEncoder(layout)
	if err != nil {
		return nil, err
	}
	required := make([]bool, int(layout.dataShreds)+int(layout.codingShreds))
	var missingData int
	for idx := 0; idx < int(layout.dataShreds); idx++ {
		if fec.data[uint32(idx)] == nil {
			required[idx] = true
			missingData++
		}
	}
	if missingData == 0 {
		return nil, nil
	}
	if err := encoder.ReconstructSome(shards, required); err != nil {
		if errors.Is(err, reedsolomon.ErrTooFewShards) {
			return nil, nil
		}
		return nil, fmt.Errorf("recover FEC set slot %d fec_set=%d: %w", state.slot, fecSetIndex, err)
	}

	var recovered []*Shred
	for idx := 0; idx < int(layout.dataShreds); idx++ {
		if !required[idx] {
			continue
		}
		shard := shards[idx]
		if len(shard) != layout.shardSize {
			return nil, fmt.Errorf("recover FEC set slot %d fec_set=%d: recovered shard %d size %d, want %d", state.slot, fecSetIndex, idx, len(shard), layout.shardSize)
		}
		shred, err := fec.recoveredDataShred(uint32(idx), shard)
		if err != nil {
			return nil, err
		}
		recovered = append(recovered, shred)
	}
	return recovered, nil
}

func (a *SlotAssembler) fecEncoder(layout fecLayout) (reedsolomon.Encoder, error) {
	encoder := a.encoders[layout]
	if encoder != nil {
		return encoder, nil
	}
	encoder, err := reedsolomon.New(
		int(layout.dataShreds),
		int(layout.codingShreds),
		reedsolomon.WithInversionCache(false),
	)
	if err != nil {
		return nil, err
	}
	a.encoders[layout] = encoder
	return encoder, nil
}

func (s *slotState) fecSet(fecSetIndex uint32) *fecState {
	fec := s.fecSets[fecSetIndex]
	if fec != nil {
		return fec
	}
	fec = &fecState{
		slot:        s.slot,
		fecSetIndex: fecSetIndex,
		data:        make(map[uint32]*Shred),
		coding:      make(map[uint16]*Shred),
	}
	s.fecSets[fecSetIndex] = fec
	return fec
}

func (s *slotState) addDataToFEC(shred *Shred) error {
	if !isMerkleVariant(shred.Variant) || len(shred.Payload) < dataPayloadSize {
		return nil
	}
	if shred.Index < shred.FECSetIndex {
		return nil
	}
	fec := s.fecSet(shred.FECSetIndex)
	if err := fec.acceptShred(shred); err != nil {
		return err
	}
	fec.data[shred.Index-shred.FECSetIndex] = shred
	return nil
}

func (f *fecState) acceptShred(shred *Shred) error {
	if !f.haveSig {
		copy(f.signature[:], shred.Signature[:])
		f.haveSig = true
	} else if f.signature != shred.Signature {
		return fmt.Errorf("mixed FEC signatures for slot %d fec_set=%d", f.slot, f.fecSetIndex)
	}

	switch shred.Type {
	case ShredTypeData:
		expected, ok := merkleCounterpartVariant(shred.Variant, ShredTypeCode)
		if !ok {
			return fmt.Errorf("unsupported data shred variant for slot %d fec_set=%d: 0x%02x", f.slot, f.fecSetIndex, shred.Variant)
		}
		if f.dataVariant == 0 {
			f.dataVariant = shred.Variant
		} else if f.dataVariant != shred.Variant {
			return fmt.Errorf("mixed data shred variants for slot %d fec_set=%d: 0x%02x and 0x%02x", f.slot, f.fecSetIndex, f.dataVariant, shred.Variant)
		}
		if f.codeVariant != 0 && f.codeVariant != expected {
			return fmt.Errorf("mixed data/code shred variants for slot %d fec_set=%d: data=0x%02x code=0x%02x", f.slot, f.fecSetIndex, shred.Variant, f.codeVariant)
		}
	case ShredTypeCode:
		expected, ok := merkleCounterpartVariant(shred.Variant, ShredTypeData)
		if !ok {
			return fmt.Errorf("unsupported coding shred variant for slot %d fec_set=%d: 0x%02x", f.slot, f.fecSetIndex, shred.Variant)
		}
		if f.codeVariant == 0 {
			f.codeVariant = shred.Variant
		} else if f.codeVariant != shred.Variant {
			return fmt.Errorf("mixed coding shred variants for slot %d fec_set=%d: 0x%02x and 0x%02x", f.slot, f.fecSetIndex, f.codeVariant, shred.Variant)
		}
		if f.dataVariant != 0 && f.dataVariant != expected {
			return fmt.Errorf("mixed data/code shred variants for slot %d fec_set=%d: data=0x%02x code=0x%02x", f.slot, f.fecSetIndex, f.dataVariant, shred.Variant)
		}
	}
	return nil
}

func (f *fecState) setLayout(layout fecLayout) error {
	if !f.haveLayout {
		f.layout = layout
		f.haveLayout = true
		return nil
	}
	if f.layout != layout {
		return fmt.Errorf("mixed FEC layouts for slot %d fec_set=%d: %+v and %+v", f.slot, f.fecSetIndex, f.layout, layout)
	}
	return nil
}

func (s *Shred) fecLayout() (fecLayout, error) {
	shard, err := s.erasureShard()
	if err != nil {
		return fecLayout{}, err
	}
	return fecLayout{
		dataShreds:   s.NumDataShreds,
		codingShreds: s.NumCodingShreds,
		shardSize:    len(shard),
	}, nil
}

func (f *fecState) recoveredDataShred(dataIndex uint32, shard []byte) (*Shred, error) {
	code := f.firstCodingShred()
	if code == nil {
		return nil, fmt.Errorf("recover FEC set slot %d fec_set=%d: no coding shred template", f.slot, f.fecSetIndex)
	}
	payload := make([]byte, dataPayloadSize)
	copy(payload[:shredSignatureSize], code.Signature[:])
	copy(payload[shredSignatureSize:], shard)
	shred, err := ParseShred(payload)
	if err != nil {
		return nil, fmt.Errorf("parse recovered data shred slot %d fec_set=%d data_index=%d: %w", f.slot, f.fecSetIndex, dataIndex, err)
	}
	expectedVariant, ok := merkleCounterpartVariant(code.Variant, ShredTypeData)
	if !ok {
		return nil, fmt.Errorf("recover FEC set slot %d fec_set=%d: unsupported coding variant 0x%02x", f.slot, f.fecSetIndex, code.Variant)
	}
	if shred.Type != ShredTypeData ||
		shred.Variant != expectedVariant ||
		shred.Slot != f.slot ||
		shred.FECSetIndex != f.fecSetIndex ||
		shred.Index != f.fecSetIndex+dataIndex ||
		shred.Version != code.Version {
		return nil, fmt.Errorf("recover FEC set slot %d fec_set=%d: invalid recovered data shred index=%d slot=%d variant=0x%02x version=%d", f.slot, f.fecSetIndex, shred.Index, shred.Slot, shred.Variant, shred.Version)
	}
	shred.Recovered = true
	return shred, nil
}

func (f *fecState) firstCodingShred() *Shred {
	for _, shred := range f.coding {
		return shred
	}
	return nil
}

func (s *slotState) complete() bool {
	if !s.haveLast {
		return false
	}
	for idx := uint32(0); idx <= s.lastIndex; idx++ {
		if s.shreds[idx] == nil {
			return false
		}
	}
	return true
}

func (s *slotState) orderedShreds() []*Shred {
	indexes := make([]int, 0, len(s.shreds))
	for idx := range s.shreds {
		indexes = append(indexes, int(idx))
	}
	sort.Ints(indexes)
	out := make([]*Shred, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, s.shreds[uint32(idx)])
	}
	return out
}

func (s *slotState) block() (*block.Block, error) {
	entries, err := DecodeEntriesFromDataShreds(s.orderedShreds())
	if err != nil {
		return nil, err
	}
	blk := BlockFromEntries(s.slot, s.parentSlot, entries)
	if err := validateBlockTransactions(blk); err != nil {
		return nil, err
	}
	return blk, nil
}
