package turbine

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/block"
	"github.com/sonicfromnewyoke/mithril/pkg/gossip"
	"github.com/gagliardetto/solana-go"
)

type LeaderForSlotFunc func(slot uint64) (solana.PublicKey, bool)

type UDPReceiver struct {
	Addr string

	assembler     *SlotAssembler
	leaderForSlot LeaderForSlotFunc
	repairClient  *repairClient
	blocks        chan *block.Block
	errs          chan error
	ready         chan error
	once          sync.Once
	readyOnce     sync.Once

	packets         atomic.Uint64
	dataShreds      atomic.Uint64
	codingShreds    atomic.Uint64
	parseErrors     atomic.Uint64
	signatureErrors atomic.Uint64
	missingLeaders  atomic.Uint64
	assemblyErrors  atomic.Uint64
	blocksEmitted   atomic.Uint64
	lastPacketUnix  atomic.Int64
	lastDataSlot    atomic.Uint64
	lastBlockSlot   atomic.Uint64
}

type ReceiverStats struct {
	Packets          uint64
	DataShreds       uint64
	CodingShreds     uint64
	ParseErrors      uint64
	SignatureErrors  uint64
	MissingLeaders   uint64
	AssemblyErrors   uint64
	BlocksEmitted    uint64
	RecoveredData    uint64
	EvictedSlots     uint64
	IgnoredOldShreds uint64
	Repair           RepairStats
	LastPacketUnix   int64
	LastDataSlot     uint64
	LastBlockSlot    uint64
	ActiveSlots      int
}

func NewUDPReceiver(addr string) *UDPReceiver {
	return &UDPReceiver{
		Addr:      addr,
		assembler: NewSlotAssembler(),
		blocks:    make(chan *block.Block, 1024),
		errs:      make(chan error, 16),
		ready:     make(chan error, 1),
	}
}

func (r *UDPReceiver) SetLeaderForSlot(fn LeaderForSlotFunc) {
	r.leaderForSlot = fn
}

func (r *UDPReceiver) SetRepairPeerSource(identity ed25519.PrivateKey, source func() []gossip.RepairPeer) error {
	client, err := newRepairClient(identity, source)
	if err != nil {
		return err
	}
	r.repairClient = client
	return nil
}

func (r *UDPReceiver) Blocks() <-chan *block.Block {
	return r.blocks
}

func (r *UDPReceiver) Errors() <-chan error {
	return r.errs
}

func (r *UDPReceiver) Ready() <-chan error {
	return r.ready
}

func (r *UDPReceiver) Stats() ReceiverStats {
	return ReceiverStats{
		Packets:          r.packets.Load(),
		DataShreds:       r.dataShreds.Load(),
		CodingShreds:     r.codingShreds.Load(),
		ParseErrors:      r.parseErrors.Load(),
		SignatureErrors:  r.signatureErrors.Load(),
		MissingLeaders:   r.missingLeaders.Load(),
		AssemblyErrors:   r.assemblyErrors.Load(),
		BlocksEmitted:    r.blocksEmitted.Load(),
		RecoveredData:    r.assembler.RecoveredDataShreds(),
		EvictedSlots:     r.assembler.EvictedSlots(),
		IgnoredOldShreds: r.assembler.IgnoredOldShreds(),
		Repair:           r.repairStats(),
		LastPacketUnix:   r.lastPacketUnix.Load(),
		LastDataSlot:     r.lastDataSlot.Load(),
		LastBlockSlot:    r.lastBlockSlot.Load(),
		ActiveSlots:      r.assembler.ActiveSlots(),
	}
}

func (r *UDPReceiver) repairStats() RepairStats {
	if r.repairClient == nil {
		return RepairStats{}
	}
	return r.repairClient.stats()
}

func (r *UDPReceiver) signalReady(err error) {
	r.readyOnce.Do(func() {
		r.ready <- err
		close(r.ready)
	})
}

func (r *UDPReceiver) Run(ctx context.Context) error {
	defer r.once.Do(func() {
		close(r.blocks)
		close(r.errs)
	})

	udpAddr, err := net.ResolveUDPAddr("udp", r.Addr)
	if err != nil {
		r.signalReady(err)
		return err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		r.signalReady(err)
		return err
	}
	defer conn.Close()
	r.signalReady(nil)

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	if r.repairClient != nil {
		go r.repairClient.run(ctx, conn, r.assembler)
	}

	buf := make([]byte, packetDataSize)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		r.packets.Add(1)
		r.lastPacketUnix.Store(time.Now().Unix())
		packet := buf[:n]
		if r.repairClient != nil && r.repairClient.handleRepairPing(conn, packet, addr) {
			continue
		}
		shred, err := ParseShred(packet)
		if err != nil {
			if errors.Is(err, ErrCodingShredIgnored) {
				r.codingShreds.Add(1)
				continue
			}
			r.parseErrors.Add(1)
			select {
			case r.errs <- err:
			default:
			}
			continue
		}
		switch shred.Type {
		case ShredTypeData:
			r.dataShreds.Add(1)
			r.lastDataSlot.Store(shred.Slot)
		case ShredTypeCode:
			r.codingShreds.Add(1)
		}
		if r.leaderForSlot != nil {
			leader, ok := r.leaderForSlot(shred.Slot)
			if !ok {
				r.missingLeaders.Add(1)
				select {
				case r.errs <- fmt.Errorf("missing leader for turbine shred slot %d", shred.Slot):
				default:
				}
				continue
			}
			if err := shred.VerifySignature(leader); err != nil {
				r.signatureErrors.Add(1)
				select {
				case r.errs <- err:
				default:
				}
				continue
			}
		}
		if r.repairClient != nil {
			r.repairClient.observeShredResponse(conn, packet, addr, shred)
		}
		blk, err := r.assembler.AddShred(shred)
		if err != nil {
			if errors.Is(err, ErrDuplicateShred) {
				continue
			}
			r.assemblyErrors.Add(1)
			select {
			case r.errs <- err:
			default:
			}
			continue
		}
		if blk == nil {
			continue
		}
		r.blocksEmitted.Add(1)
		r.lastBlockSlot.Store(blk.Slot)
		select {
		case r.blocks <- blk:
		case <-ctx.Done():
			return nil
		}
	}
}
