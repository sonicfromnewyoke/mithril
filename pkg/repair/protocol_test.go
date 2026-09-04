package repair

import (
	"crypto/ed25519"
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/gossip"
)

func TestBuildWindowIndexRequest(t *testing.T) {
	pub, identity, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	var sender gossip.Pubkey
	copy(sender[:], pub)
	var recipient gossip.Pubkey
	for idx := range recipient {
		recipient[idx] = byte(idx + 1)
	}

	packet, err := buildRequest(identity, recipient, repairProtocolWindowIndex, 123, 456, 789, 101112)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if len(packet) != 160 {
		t.Fatalf("packet length = %d, want 160", len(packet))
	}
	if got := binary.LittleEndian.Uint32(packet[0:4]); got != repairProtocolWindowIndex {
		t.Fatalf("variant = %d, want %d", got, repairProtocolWindowIndex)
	}
	if string(packet[68:100]) != string(sender[:]) {
		t.Fatalf("sender was not encoded")
	}
	if string(packet[100:132]) != string(recipient[:]) {
		t.Fatalf("recipient was not encoded")
	}
	if got := binary.LittleEndian.Uint64(packet[132:140]); got != 101112 {
		t.Fatalf("timestamp = %d, want 101112", got)
	}
	if got := binary.LittleEndian.Uint32(packet[140:144]); got != 789 {
		t.Fatalf("nonce = %d, want 789", got)
	}
	if got := binary.LittleEndian.Uint64(packet[144:152]); got != 123 {
		t.Fatalf("slot = %d, want 123", got)
	}
	if got := binary.LittleEndian.Uint64(packet[152:160]); got != 456 {
		t.Fatalf("shred index = %d, want 456", got)
	}
	if !VerifySignedRequest(packet, sender) {
		t.Fatalf("request signature did not verify")
	}
}

func TestBuildHighestWindowIndexRequest(t *testing.T) {
	_, identity, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	var recipient gossip.Pubkey
	packet, err := buildRequest(identity, recipient, repairProtocolHighestWindowIndex, 123, 456, 789, 101112)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if got := binary.LittleEndian.Uint32(packet[0:4]); got != repairProtocolHighestWindowIndex {
		t.Fatalf("variant = %d, want %d", got, repairProtocolHighestWindowIndex)
	}
}

func TestDecodeRepairPingAndBuildPong(t *testing.T) {
	peerPub, peerIdentity, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey peer returned error: %v", err)
	}
	ourPub, ourIdentity, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey identity returned error: %v", err)
	}

	var token [32]byte
	for idx := range token {
		token[idx] = byte(idx + 11)
	}
	signature := ed25519.Sign(peerIdentity, token[:])
	packet := make([]byte, 0, repairPingSize)
	packet = binary.LittleEndian.AppendUint32(packet, repairProtocolPingResponse)
	packet = append(packet, peerPub...)
	packet = append(packet, token[:]...)
	packet = append(packet, signature...)

	ping, ok := DecodePing(packet)
	if !ok {
		t.Fatalf("DecodePing rejected valid repair ping")
	}
	if string(ping.From[:]) != string(peerPub) {
		t.Fatalf("ping sender was not decoded")
	}

	pong, err := BuildPong(ourIdentity, ping)
	if err != nil {
		t.Fatalf("BuildPong returned error: %v", err)
	}
	if len(pong) != repairPingSize {
		t.Fatalf("pong length = %d, want %d", len(pong), repairPingSize)
	}
	if got := binary.LittleEndian.Uint32(pong[0:4]); got != repairProtocolPong {
		t.Fatalf("pong variant = %d, want %d", got, repairProtocolPong)
	}
	if string(pong[4:36]) != string(ourPub) {
		t.Fatalf("pong sender was not encoded")
	}
	hash := hashPingToken(token)
	if string(pong[36:68]) != string(hash[:]) {
		t.Fatalf("pong hash was not encoded")
	}
	if !ed25519.Verify(ourPub, hash[:], pong[68:132]) {
		t.Fatalf("pong signature did not verify")
	}
}

func TestDecodeRepairPingRejectsInvalidSignature(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	packet := make([]byte, repairPingSize)
	binary.LittleEndian.PutUint32(packet[0:4], repairProtocolPingResponse)
	copy(packet[4:36], pub)
	if _, ok := DecodePing(packet); ok {
		t.Fatalf("DecodePing accepted invalid signature")
	}
}
