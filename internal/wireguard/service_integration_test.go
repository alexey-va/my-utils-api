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
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
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
	health := healthyExitHealth(now)
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey,
		PublicEndpoint:  relay.PublicEndpoint,
		AppliedRevision: 0,
		RoutingStatus:   &RoutingStatus{Mode: "RU_DIRECT_AWG_DEFAULT", RUPrefixCount: 8651, UpdatedAt: now, Healthy: true, CheckedAt: now},
		ExitHealth:      &health,
	}); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	relays, err := service.ListRelays(ctx)
	if err != nil || len(relays) == 0 {
		t.Fatalf("ListRelays() after health heartbeat = %#v, %v", relays, err)
	}
	persisted := relays[len(relays)-1]
	if persisted.Status != "READY" || persisted.RoutingHealthy == nil || !*persisted.RoutingHealthy || persisted.ExitHealth == nil || persisted.ExitHealth.ActiveExit == nil || *persisted.ExitHealth.ActiveExit != "primary" {
		t.Fatalf("persisted relay health = %#v", persisted)
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
	counters := []struct {
		receive, transmit int64
		routing           RoutingTraffic
	}{
		{100, 200, RoutingTraffic{RUDownloadBytes: 80, RUUploadBytes: 30, NonRUDownloadBytes: 120, NonRUUploadBytes: 70}},
		{150, 260, RoutingTraffic{RUDownloadBytes: 100, RUUploadBytes: 50, NonRUDownloadBytes: 160, NonRUUploadBytes: 100}},
		{10, 20, RoutingTraffic{RUDownloadBytes: 5, RUUploadBytes: 2, NonRUDownloadBytes: 15, NonRUUploadBytes: 8}},
	}
	for _, counter := range counters {
		now = now.Add(10 * time.Second)
		if err := service.Heartbeat(ctx, relay.ID, Heartbeat{ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 1, Peers: []PeerCounter{{PublicKey: peer.Peer.PublicKey, LatestHandshakeEpochSecond: 1_800_000_000, ReceiveBytes: counter.receive, TransmitBytes: counter.transmit, RoutingTraffic: &counter.routing}}}); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Second)
	peers, err := service.ListPeers(ctx, relay.ID, "HOUR")
	if err != nil || peers[0].TotalReceiveBytes != 160 || peers[0].TotalTransmitBytes != 280 {
		t.Fatalf("ListPeers counters = %#v, %v", peers, err)
	}
	if peers[0].CurrentDownloadBytesPerSecond != 2 || peers[0].CurrentUploadBytesPerSecond != 1 {
		t.Fatalf("ListPeers persisted rates = %#v", peers[0])
	}
	if peers[0].Traffic.Range != "HOUR" || peers[0].Traffic.DownloadBytes != 280 || peers[0].Traffic.UploadBytes != 160 {
		t.Fatalf("ListPeers period traffic = %#v", peers[0].Traffic)
	}
	if peers[0].Traffic.RUDownloadBytes != 105 || peers[0].Traffic.RUUploadBytes != 52 || peers[0].Traffic.NonRUDownloadBytes != 175 || peers[0].Traffic.NonRUUploadBytes != 108 {
		t.Fatalf("ListPeers route counter deltas = %#v", peers[0].Traffic)
	}

	metrics, err := service.Metrics(ctx, relay.ID, peer.Peer.ID, "HOUR")
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	if metrics.Summary.DownloadBytes != 280 || metrics.Summary.UploadBytes != 160 {
		t.Fatalf("Metrics summary = %#v", metrics.Summary)
	}
	if metrics.Summary.RUDownloadBytes != 105 || metrics.Summary.RUUploadBytes != 52 || metrics.Summary.NonRUDownloadBytes != 175 || metrics.Summary.NonRUUploadBytes != 108 {
		t.Fatalf("Metrics route counter deltas = %#v", metrics.Summary)
	}
}
