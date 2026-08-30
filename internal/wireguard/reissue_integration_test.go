package wireguard

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReissuePeerCredentialsRotatesKeysAtomically(t *testing.T) {
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
	for index := range key {
		key[index] = byte(index + 1)
	}
	cipher, err := NewCredentialsCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, cipher)
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	relay, err := service.CreateRelay(ctx, CreateRelayRequest{
		Name: fmt.Sprintf("Reissue relay %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.10:51820",
		ClientCIDR: "10.90.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	original, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.ListRelays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reissued, err := service.ReissuePeerCredentials(ctx, relay.ID, original.Peer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reissued.Peer.ID != original.Peer.ID || reissued.Peer.AssignedIP != original.Peer.AssignedIP || reissued.Peer.PublicKey == original.Peer.PublicKey || reissued.ClientConfig == original.ClientConfig {
		t.Fatalf("original=%#v reissued=%#v", original, reissued)
	}
	after, err := service.ListRelays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeRelay, beforeFound := relayByID(before, relay.ID)
	afterRelay, afterFound := relayByID(after, relay.ID)
	if !beforeFound || !afterFound || afterRelay.DesiredRevision != beforeRelay.DesiredRevision+1 {
		t.Fatalf("desired revision before=%#v after=%#v", before, after)
	}
	persisted, err := service.Credentials(ctx, relay.ID, original.Peer.ID)
	if err != nil || persisted.ClientConfig != reissued.ClientConfig {
		t.Fatalf("persisted=%#v error=%v", persisted, err)
	}
	if err := service.SetPeerIDsEnabled(ctx, relay.ID, []string{original.Peer.ID}, false); err != nil {
		t.Fatal(err)
	}
	desired, err := service.Desired(ctx, relay.ID)
	if err != nil || len(desired.Peers) != 0 {
		t.Fatalf("blocked peer leaked into desired state: %#v error=%v", desired, err)
	}
	if err := service.SetPeerIDsEnabled(ctx, relay.ID, []string{original.Peer.ID}, true); err != nil {
		t.Fatal(err)
	}
	desired, err = service.Desired(ctx, relay.ID)
	if err != nil || len(desired.Peers) != 1 || desired.Peers[0].PublicKey != reissued.Peer.PublicKey {
		t.Fatalf("reapproved peer missing from desired state: %#v error=%v", desired, err)
	}
}

func relayByID(relays []Relay, relayID string) (Relay, bool) {
	for _, relay := range relays {
		if relay.ID == relayID {
			return relay, true
		}
	}
	return Relay{}, false
}
