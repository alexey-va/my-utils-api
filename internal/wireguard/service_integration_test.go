package wireguard

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestControlPlaneProvisionHeartbeatAndCounters(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := NewCredentialsCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, cipher)
	relay, err := service.CreateRelay(ctx, CreateRelayRequest{
		Name: fmt.Sprintf("Go relay %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.10:51820",
		ClientCIDR: "10.89.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("CreateRelay() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	if relay.Status != "WAITING_FOR_AGENT" || len(relay.AgentToken) < 40 || !service.AgentTokenMatches(ctx, relay.ID, relay.AgentToken) {
		t.Fatalf("created relay = %#v", relay)
	}
	if _, err := service.CreatePeer(ctx, relay.ID, "Phone"); err == nil || !strings.Contains(err.Error(), "server public key") {
		t.Fatalf("CreatePeer before heartbeat error = %v", err)
	}
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 0}); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	peer, err := service.CreatePeer(ctx, relay.ID, "Alex phone")
	if err != nil {
		t.Fatalf("CreatePeer() error = %v", err)
	}
	if peer.Peer.AssignedIP != "10.89.0.2" || peer.FileName != "alex-phone.conf" || !strings.Contains(peer.ClientConfig, "PrivateKey = ") {
		t.Fatalf("created peer = %#v", peer)
	}
	desired, err := service.Desired(ctx, relay.ID)
	if err != nil || desired.Revision != 1 || len(desired.Peers) != 1 || desired.Peers[0].AllowedIP != "10.89.0.2/32" {
		t.Fatalf("Desired() = %#v, %v", desired, err)
	}
	for _, counters := range [][2]int64{{100, 200}, {150, 260}, {10, 20}} {
		if err := service.Heartbeat(ctx, relay.ID, Heartbeat{ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 1, Peers: []PeerCounter{{PublicKey: peer.Peer.PublicKey, LatestHandshakeEpochSecond: 1_800_000_000, ReceiveBytes: counters[0], TransmitBytes: counters[1]}}}); err != nil {
			t.Fatal(err)
		}
	}
	peers, err := service.ListPeers(ctx, relay.ID)
	if err != nil || peers[0].TotalReceiveBytes != 160 || peers[0].TotalTransmitBytes != 280 {
		t.Fatalf("ListPeers counters = %#v, %v", peers, err)
	}
}
