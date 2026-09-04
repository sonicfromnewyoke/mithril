package gossip

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
)

const (
	DefaultBindAddr      = "0.0.0.0:0"
	defaultPushInterval  = 2 * time.Second
	defaultPingInterval  = 10 * time.Second
	defaultEchoTimeout   = 5 * time.Second
	maxKnownGossipPeers  = 128
	peerExpirationWindow = 15 * time.Minute
)

type Config struct {
	Entrypoint        string
	BindAddr          string
	TVUAddr           string
	AdvertisedIP      string
	ShredVersion      uint16
	Identity          ed25519.PrivateKey
	PushInterval      time.Duration
	PingInterval      time.Duration
	EntrypointTimeout time.Duration
	Name              string
}

type Client struct {
	cfg Config

	entrypoint *net.UDPAddr
	bindAddr   *net.UDPAddr
	tvuAddr    *net.UDPAddr
	identity   ed25519.PrivateKey
	pubkey     Pubkey

	contactMu sync.RWMutex
	contact   *ContactInfo

	peerMu sync.Mutex
	peers  map[udpAddrKey]knownPeer

	repairPeerMu sync.Mutex
	repairPeers  map[Pubkey]RepairPeer

	rxPackets        atomic.Uint64
	txPackets        atomic.Uint64
	rxDecodeErrors   atomic.Uint64
	txErrors         atomic.Uint64
	rxPings          atomic.Uint64
	rxPongs          atomic.Uint64
	rxContacts       atomic.Uint64
	acceptedContacts atomic.Uint64
	rxPullRequests   atomic.Uint64
	txPushMessages   atomic.Uint64
	txPingMessages   atomic.Uint64
	txPongMessages   atomic.Uint64
	txPullResponses  atomic.Uint64
	lastRxUnix       atomic.Int64
	lastTxUnix       atomic.Int64
}

type knownPeer struct {
	addr     *net.UDPAddr
	lastSeen time.Time
}

type udpAddrKey struct {
	ip   [16]byte
	port int
}

type RepairPeer struct {
	Pubkey   Pubkey
	Addr     *net.UDPAddr
	LastSeen time.Time
}

type Stats struct {
	RxPackets        uint64
	TxPackets        uint64
	RxDecodeErrors   uint64
	TxErrors         uint64
	RxPings          uint64
	RxPongs          uint64
	RxContacts       uint64
	AcceptedContacts uint64
	RxPullRequests   uint64
	TxPushMessages   uint64
	TxPingMessages   uint64
	TxPongMessages   uint64
	TxPullResponses  uint64
	LastRxUnix       int64
	LastTxUnix       int64
	Peers            int
	RepairPeers      int
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.Name == "" {
		cfg.Name = ClientName
	}
	if cfg.Entrypoint == "" {
		return nil, fmt.Errorf("gossip entrypoint is required")
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = DefaultBindAddr
	}
	if cfg.PushInterval == 0 {
		cfg.PushInterval = defaultPushInterval
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = defaultPingInterval
	}
	if cfg.EntrypointTimeout == 0 {
		cfg.EntrypointTimeout = defaultEchoTimeout
	}
	entrypoint, err := net.ResolveUDPAddr("udp", cfg.Entrypoint)
	if err != nil {
		return nil, fmt.Errorf("resolve gossip entrypoint: %w", err)
	}
	bindAddr, err := net.ResolveUDPAddr("udp", cfg.BindAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve gossip bind address: %w", err)
	}
	tvuAddr, err := net.ResolveUDPAddr("udp", cfg.TVUAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve TVU address: %w", err)
	}
	identity := cfg.Identity
	if len(identity) == 0 {
		_, identity, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate gossip identity: %w", err)
		}
	}
	if len(identity) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid gossip identity size %d", len(identity))
	}
	pubkey, err := pubkeyFromPrivateKey(identity)
	if err != nil {
		return nil, err
	}

	return &Client{
		cfg:         cfg,
		entrypoint:  entrypoint,
		bindAddr:    bindAddr,
		tvuAddr:     tvuAddr,
		identity:    identity,
		pubkey:      pubkey,
		peers:       make(map[udpAddrKey]knownPeer),
		repairPeers: make(map[Pubkey]RepairPeer),
	}, nil
}

func (c *Client) Pubkey() Pubkey {
	return c.pubkey
}

func (c *Client) Identity() ed25519.PrivateKey {
	return append(ed25519.PrivateKey(nil), c.identity...)
}

func (c *Client) ShredVersion() uint16 {
	c.contactMu.RLock()
	defer c.contactMu.RUnlock()
	if c.contact == nil {
		return c.cfg.ShredVersion
	}
	return c.contact.ShredVer
}

func (c *Client) Run(ctx context.Context) error {
	conn, err := net.ListenUDP("udp", c.bindAddr)
	if err != nil {
		return fmt.Errorf("bind gossip socket %s: %w", c.bindAddr, err)
	}
	defer conn.Close()

	if err := c.initializeContact(conn.LocalAddr().(*net.UDPAddr)); err != nil {
		return err
	}
	c.contactMu.RLock()
	contact := c.contact
	c.contactMu.RUnlock()
	if contact != nil {
		mlog.Log.Infof("gossip client listening: local=%s advertised_gossip=%s advertised_tvu=%s shred_version=%d client=%s",
			conn.LocalAddr().String(), contact.GossipAddr.String(), contact.TVUAddr.String(), contact.ShredVer, c.cfg.Name)
	}
	c.recordPeer(c.entrypoint)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.recvLoop(ctx, conn)
	}()

	if err := c.pushContact(conn); err != nil {
		return err
	}
	_ = c.sendPing(conn, c.entrypoint)

	pushTicker := time.NewTicker(c.cfg.PushInterval)
	defer pushTicker.Stop()
	pingTicker := time.NewTicker(c.cfg.PingInterval)
	defer pingTicker.Stop()
	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if err == nil || ctx.Err() != nil {
				return nil
			}
			return err
		case <-pushTicker.C:
			if err := c.pushContact(conn); err != nil {
				return err
			}
		case <-pingTicker.C:
			_ = c.sendPing(conn, c.entrypoint)
		case <-statsTicker.C:
			stats := c.Stats()
			mlog.Log.FileOnlyf("gossip client stats: rx=%d tx=%d peers=%d repair_peers=%d decode_errors=%d tx_errors=%d pings=%d/%d pongs=%d/%d contacts_rx=%d contacts_accepted=%d pull_requests=%d pull_responses=%d last_rx=%s last_tx=%s",
				stats.RxPackets, stats.TxPackets, stats.Peers, stats.RepairPeers, stats.RxDecodeErrors, stats.TxErrors,
				stats.RxPings, stats.TxPingMessages, stats.RxPongs, stats.TxPongMessages,
				stats.RxContacts, stats.AcceptedContacts, stats.RxPullRequests, stats.TxPullResponses,
				formatAge(stats.LastRxUnix), formatAge(stats.LastTxUnix))
		}
	}
}

func (c *Client) Stats() Stats {
	c.peerMu.Lock()
	peers := len(c.peers)
	c.peerMu.Unlock()
	c.repairPeerMu.Lock()
	repairPeers := len(c.repairPeers)
	c.repairPeerMu.Unlock()
	return Stats{
		RxPackets:        c.rxPackets.Load(),
		TxPackets:        c.txPackets.Load(),
		RxDecodeErrors:   c.rxDecodeErrors.Load(),
		TxErrors:         c.txErrors.Load(),
		RxPings:          c.rxPings.Load(),
		RxPongs:          c.rxPongs.Load(),
		RxContacts:       c.rxContacts.Load(),
		AcceptedContacts: c.acceptedContacts.Load(),
		RxPullRequests:   c.rxPullRequests.Load(),
		TxPushMessages:   c.txPushMessages.Load(),
		TxPingMessages:   c.txPingMessages.Load(),
		TxPongMessages:   c.txPongMessages.Load(),
		TxPullResponses:  c.txPullResponses.Load(),
		LastRxUnix:       c.lastRxUnix.Load(),
		LastTxUnix:       c.lastTxUnix.Load(),
		Peers:            peers,
		RepairPeers:      repairPeers,
	}
}

func (c *Client) RepairPeers() []RepairPeer {
	now := time.Now()
	c.repairPeerMu.Lock()
	defer c.repairPeerMu.Unlock()
	for key, peer := range c.repairPeers {
		if now.Sub(peer.LastSeen) > peerExpirationWindow {
			delete(c.repairPeers, key)
		}
	}
	peers := make([]RepairPeer, 0, len(c.repairPeers))
	for _, peer := range c.repairPeers {
		peers = append(peers, peer)
	}
	return peers
}

func (c *Client) initializeContact(localGossipAddr *net.UDPAddr) error {
	shredVersion := c.cfg.ShredVersion
	var advertisedIP net.IP
	if c.cfg.AdvertisedIP != "" {
		advertisedIP = net.ParseIP(c.cfg.AdvertisedIP)
		if advertisedIP == nil {
			return fmt.Errorf("invalid advertised IP %q", c.cfg.AdvertisedIP)
		}
	}

	if shredVersion == 0 || advertisedIP == nil {
		echo, err := QueryEntrypoint(c.entrypoint, c.cfg.EntrypointTimeout)
		if err != nil {
			if shredVersion == 0 {
				return fmt.Errorf("query gossip entrypoint %s: %w", c.entrypoint.String(), err)
			}
		} else {
			if shredVersion == 0 {
				shredVersion = echo.ShredVersion
			}
			if advertisedIP == nil {
				advertisedIP = echo.Address
			}
		}
	}
	if advertisedIP == nil {
		advertisedIP = localGossipAddr.IP
	}
	advertisedIP = normalizedIP(advertisedIP)
	if advertisedIP == nil || advertisedIP.IsUnspecified() {
		return fmt.Errorf("could not determine advertised gossip IP")
	}
	if shredVersion == 0 {
		return fmt.Errorf("could not determine shred version")
	}

	gossipAddr := &net.UDPAddr{IP: advertisedIP, Port: localGossipAddr.Port}
	tvuAddr := &net.UDPAddr{IP: advertisedIP, Port: c.tvuAddr.Port}
	if tvuIP := normalizedIP(c.tvuAddr.IP); tvuIP != nil && !tvuIP.IsUnspecified() {
		tvuAddr.IP = tvuIP
	}

	contact, err := NewContactInfo(c.pubkey, shredVersion, gossipAddr, tvuAddr)
	if err != nil {
		return err
	}
	c.contactMu.Lock()
	c.contact = contact
	c.contactMu.Unlock()
	return nil
}

func (c *Client) currentContactValue() (CrdsValue, error) {
	c.contactMu.RLock()
	contact := c.contact
	c.contactMu.RUnlock()
	if contact == nil {
		return CrdsValue{}, fmt.Errorf("gossip contact info is not initialized")
	}
	return signCrdsContactInfo(contact.CloneWithWallclock(wallclockMillis()), c.identity)
}

func (c *Client) pushContact(conn *net.UDPConn) error {
	value, err := c.currentContactValue()
	if err != nil {
		return err
	}
	packet, err := encodePushMessage(c.pubkey, []CrdsValue{value})
	if err != nil {
		return err
	}
	for _, peer := range c.currentPeers() {
		if err := sendUDP(conn, packet, peer); err != nil {
			c.txErrors.Add(1)
			return err
		}
		c.recordTx()
		c.txPushMessages.Add(1)
	}
	return nil
}

func (c *Client) sendPullResponse(conn *net.UDPConn, addr *net.UDPAddr) error {
	value, err := c.currentContactValue()
	if err != nil {
		return err
	}
	packet, err := encodePullResponse(c.pubkey, []CrdsValue{value})
	if err != nil {
		return err
	}
	if err := sendUDP(conn, packet, addr); err != nil {
		c.txErrors.Add(1)
		return err
	}
	c.recordTx()
	c.txPullResponses.Add(1)
	return nil
}

func (c *Client) sendPing(conn *net.UDPConn, addr *net.UDPAddr) error {
	ping, err := newPing(c.identity)
	if err != nil {
		return err
	}
	if err := sendUDP(conn, encodePingMessage(ping), addr); err != nil {
		c.txErrors.Add(1)
		return err
	}
	c.recordTx()
	c.txPingMessages.Add(1)
	return nil
}

func (c *Client) sendPong(conn *net.UDPConn, ping Ping, addr *net.UDPAddr) error {
	if !ping.Verify() {
		return nil
	}
	pong, err := newPong(ping, c.identity)
	if err != nil {
		return err
	}
	if err := sendUDP(conn, encodePongMessage(pong), addr); err != nil {
		c.txErrors.Add(1)
		return err
	}
	c.recordTx()
	c.txPongMessages.Add(1)
	return nil
}

func (c *Client) recvLoop(ctx context.Context, conn *net.UDPConn) error {
	buf := make([]byte, packetDataSize)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c.rxPackets.Add(1)
		c.lastRxUnix.Store(time.Now().Unix())
		if err := c.handlePacket(conn, buf[:n], addr); err != nil {
			c.rxDecodeErrors.Add(1)
			continue
		}
	}
}

func (c *Client) handlePacket(conn *net.UDPConn, packet []byte, from *net.UDPAddr) error {
	shredVersion := c.ShredVersion()
	decoded, err := decodePacketWithContactHandler(packet, func(record contactRecord) {
		c.handleContactRecord(record, shredVersion)
	})
	if err != nil {
		return err
	}
	switch decoded.Kind {
	case packetPing:
		c.rxPings.Add(1)
		return c.sendPong(conn, decoded.Ping, from)
	case packetPong:
		c.rxPongs.Add(1)
		if decoded.Pong.Verify() {
			c.recordPeer(from)
		}
	case packetContacts:
		c.rxContacts.Add(uint64(decoded.ContactCount))
	case packetPullRequest:
		c.rxPullRequests.Add(1)
		return c.sendPullResponse(conn, from)
	}
	return nil
}

func (c *Client) handleContactRecord(record contactRecord, shredVersion uint16) {
	if record.ShredVer != shredVersion || !record.GossipAddr.ok {
		return
	}
	if sameEndpointUDPAddr(record.GossipAddr, c.entrypoint) {
		return
	}
	c.recordPeerEndpoint(record.GossipAddr)
	c.recordRepairPeerRecord(record)
	c.acceptedContacts.Add(1)
}

func (c *Client) recordRepairPeer(contact *ContactInfo) {
	if contact == nil || contact.ServeRepairAddr == nil || contact.ServeRepairAddr.Port == 0 {
		return
	}
	now := time.Now()
	key := contact.Pubkey
	c.repairPeerMu.Lock()
	if existing, ok := c.repairPeers[key]; ok && sameAddr(existing.Addr, contact.ServeRepairAddr) {
		existing.LastSeen = now
		c.repairPeers[key] = existing
		c.repairPeerMu.Unlock()
		return
	}
	ip := normalizedIP(contact.ServeRepairAddr.IP)
	if ip == nil || ip.IsUnspecified() {
		c.repairPeerMu.Unlock()
		return
	}
	addr := &net.UDPAddr{IP: ip, Port: contact.ServeRepairAddr.Port}
	c.repairPeers[key] = RepairPeer{
		Pubkey:   contact.Pubkey,
		Addr:     addr,
		LastSeen: now,
	}
	c.repairPeerMu.Unlock()
}

func (c *Client) recordRepairPeerRecord(record contactRecord) {
	if !record.ServeRepairAddr.ok || record.ServeRepairAddr.port == 0 {
		return
	}
	now := time.Now()
	key := record.Pubkey
	c.repairPeerMu.Lock()
	if existing, ok := c.repairPeers[key]; ok && sameEndpointUDPAddr(record.ServeRepairAddr, existing.Addr) {
		existing.LastSeen = now
		c.repairPeers[key] = existing
		c.repairPeerMu.Unlock()
		return
	}
	c.repairPeers[key] = RepairPeer{
		Pubkey:   record.Pubkey,
		Addr:     record.ServeRepairAddr.UDPAddr(),
		LastSeen: now,
	}
	c.repairPeerMu.Unlock()
}

func (c *Client) recordTx() {
	c.txPackets.Add(1)
	c.lastTxUnix.Store(time.Now().Unix())
}

func (c *Client) recordPeer(addr *net.UDPAddr) {
	if addr == nil || addr.Port == 0 {
		return
	}
	ip := normalizedIP(addr.IP)
	if ip == nil || ip.IsUnspecified() {
		return
	}
	key, ok := makeUDPAddrKey(addr)
	if !ok {
		return
	}
	now := time.Now()
	c.peerMu.Lock()
	defer c.peerMu.Unlock()
	if existing, exists := c.peers[key]; exists {
		existing.lastSeen = now
		c.peers[key] = existing
		return
	}
	if len(c.peers) >= maxKnownGossipPeers {
		c.prunePeersLocked(now)
	}
	if len(c.peers) >= maxKnownGossipPeers {
		return
	}
	c.peers[key] = knownPeer{addr: cloneUDPAddr(addr), lastSeen: now}
}

func (c *Client) recordPeerEndpoint(endpoint contactEndpoint) {
	if !endpoint.ok || endpoint.port == 0 {
		return
	}
	key, ok := makeUDPAddrKeyFromEndpoint(endpoint)
	if !ok {
		return
	}
	now := time.Now()
	c.peerMu.Lock()
	defer c.peerMu.Unlock()
	if existing, exists := c.peers[key]; exists {
		existing.lastSeen = now
		c.peers[key] = existing
		return
	}
	if len(c.peers) >= maxKnownGossipPeers {
		c.prunePeersLocked(now)
	}
	if len(c.peers) >= maxKnownGossipPeers {
		return
	}
	c.peers[key] = knownPeer{addr: endpoint.UDPAddr(), lastSeen: now}
}

func (c *Client) currentPeers() []*net.UDPAddr {
	now := time.Now()
	c.peerMu.Lock()
	defer c.peerMu.Unlock()
	c.prunePeersLocked(now)
	peers := make([]*net.UDPAddr, 0, len(c.peers))
	for _, peer := range c.peers {
		peers = append(peers, cloneUDPAddr(peer.addr))
	}
	return peers
}

func (c *Client) prunePeersLocked(now time.Time) {
	for key, peer := range c.peers {
		if now.Sub(peer.lastSeen) > peerExpirationWindow {
			delete(c.peers, key)
		}
	}
}

func formatAge(unix int64) string {
	if unix == 0 {
		return "never"
	}
	return time.Since(time.Unix(unix, 0)).Round(time.Second).String()
}

func makeUDPAddrKey(addr *net.UDPAddr) (udpAddrKey, bool) {
	var key udpAddrKey
	if addr == nil || addr.Port == 0 {
		return key, false
	}
	ip := addr.IP.To16()
	if ip == nil {
		return key, false
	}
	copy(key.ip[:], ip)
	key.port = addr.Port
	return key, true
}

func makeUDPAddrKeyFromEndpoint(endpoint contactEndpoint) (udpAddrKey, bool) {
	var key udpAddrKey
	if !endpoint.ok || endpoint.port == 0 {
		return key, false
	}
	copy(key.ip[:], endpoint.ip[:])
	key.port = endpoint.port
	return key, true
}

func sameEndpointUDPAddr(endpoint contactEndpoint, addr *net.UDPAddr) bool {
	key, ok := makeUDPAddrKeyFromEndpoint(endpoint)
	if !ok {
		return addr == nil
	}
	addrKey, ok := makeUDPAddrKey(addr)
	return ok && key == addrKey
}
