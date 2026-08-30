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
	if _, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Phone"}); err == nil || !strings.Contains(err.Error(), "server public key") {
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
	peer, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Alex phone"})
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

	now = now.Add(2 * time.Minute)
	degraded := healthyExitHealth(now)
	primary := degraded.Exits["primary"]
	primary.Healthy = false
	primaryReason := "egress_probe_failed"
	primary.Reason = &primaryReason
	primary.ObservedEgressIP = nil
	degraded.Exits["primary"] = primary
	secondaryExit := "secondary"
	secondaryInterface := "awg-exit-b"
	degraded.OverallStatus = "DEGRADED"
	degraded.ActiveExit = &secondaryExit
	degraded.ActiveInterface = &secondaryInterface
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey,
		PublicEndpoint:  relay.PublicEndpoint,
		AppliedRevision: 1,
		ExitHealth:      &degraded,
	}); err != nil {
		t.Fatalf("Heartbeat() degraded exit health error = %v", err)
	}

	snapshot, err := service.Snapshot(ctx, relay.ID, "HOUR")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Relay.ID != relay.ID || len(snapshot.Peers) != 1 || snapshot.Peers[0].ID != peer.Peer.ID {
		t.Fatalf("Snapshot relay/peers = %#v", snapshot)
	}
	if len(snapshot.Categories) != 2 || snapshot.Categories[0].Name != "Пользовательские" || snapshot.Categories[1].Name != "Служебные" {
		t.Fatalf("Snapshot categories = %#v", snapshot.Categories)
	}
	preview, ok := snapshot.PeerMetrics[peer.Peer.ID]
	if !ok || preview.Range != "HOUR" || preview.Summary.DownloadBytes != 280 || preview.Summary.UploadBytes != 160 {
		t.Fatalf("Snapshot metrics = %#v", snapshot.PeerMetrics)
	}
	if snapshot.Peers[0].Traffic.DownloadBytes != preview.Summary.DownloadBytes || snapshot.Peers[0].Traffic.UploadBytes != preview.Summary.UploadBytes {
		t.Fatalf("Snapshot peer traffic = %#v, preview = %#v", snapshot.Peers[0].Traffic, preview.Summary)
	}
	if snapshot.ExitHealthHistory.Range != "HOUR" || len(snapshot.ExitHealthHistory.Points) != 2 {
		t.Fatalf("Snapshot exit health history = %#v", snapshot.ExitHealthHistory)
	}
	latestHealth := snapshot.ExitHealthHistory.Points[1]
	if latestHealth.PrimaryAvailabilityPercent != 0 || latestHealth.SecondaryAvailabilityPercent != 100 || latestHealth.ActiveExit == nil || *latestHealth.ActiveExit != "secondary" {
		t.Fatalf("Snapshot latest exit health point = %#v", latestHealth)
	}
	if latestHealth.PrimaryFailureReason == nil || *latestHealth.PrimaryFailureReason != "egress_probe_failed" {
		t.Fatalf("Snapshot latest primary failure reason = %#v", latestHealth.PrimaryFailureReason)
	}
	if _, err := service.UpdateExitPreference(ctx, relay.ID, UpdateExitPreferenceRequest{Preference: "PRIMARY"}); err == nil || !strings.Contains(err.Error(), "not healthy") {
		t.Fatalf("UpdateExitPreference(PRIMARY) error = %v", err)
	}
	updatedRelay, err := service.UpdateExitPreference(ctx, relay.ID, UpdateExitPreferenceRequest{Preference: "SECONDARY"})
	if err != nil {
		t.Fatalf("UpdateExitPreference(SECONDARY) error = %v", err)
	}
	if updatedRelay.ExitPreference != "SECONDARY" || updatedRelay.DesiredRevision != 2 {
		t.Fatalf("updated relay = %#v", updatedRelay)
	}
	desired, err = service.Desired(ctx, relay.ID)
	if err != nil || desired.ExitPreference != "SECONDARY" || desired.Revision != 2 {
		t.Fatalf("Desired() after exit preference = %#v, %v", desired, err)
	}
}

func TestPeerOrganizationRenameReorderAndDelete(t *testing.T) {
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
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	relay, err := service.CreateRelay(ctx, CreateRelayRequest{
		Name: fmt.Sprintf("Peer organization %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.10:51820",
		ClientCIDR: "10.91.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("CreateRelay() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey,
		PublicEndpoint:  relay.PublicEndpoint,
		AppliedRevision: 0,
	}); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	categories, err := service.ListPeerCategories(ctx, relay.ID)
	if err != nil || len(categories) != 2 || categories[0].Name != "Пользовательские" || categories[1].Name != "Служебные" {
		t.Fatalf("default peer categories = %#v, %v", categories, err)
	}
	customCategory, err := service.CreatePeerCategory(ctx, relay.ID, CreatePeerCategoryRequest{Name: "Мои"})
	if err != nil {
		t.Fatalf("CreatePeerCategory() error = %v", err)
	}
	if customCategory.SortOrder != 2 {
		t.Fatalf("created peer category = %#v", customCategory)
	}
	if _, err := service.CreatePeerCategory(ctx, relay.ID, CreatePeerCategoryRequest{Name: "мои"}); err == nil {
		t.Fatal("CreatePeerCategory() accepted a duplicate name")
	}

	phone, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Phone"})
	if err != nil {
		t.Fatalf("CreatePeer(phone) error = %v", err)
	}
	proxy, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Proxy", Category: "Служебные"})
	if err != nil {
		t.Fatalf("CreatePeer(proxy) error = %v", err)
	}
	if phone.Peer.Category != "Пользовательские" || phone.Peer.SortOrder != 0 || proxy.Peer.Category != "Служебные" || proxy.Peer.SortOrder != 1 {
		t.Fatalf("created peer organization = phone %#v, proxy %#v", phone.Peer, proxy.Peer)
	}

	renamed := "Main phone"
	userCategory := customCategory.Name
	updated, err := service.UpdatePeer(ctx, relay.ID, phone.Peer.ID, UpdatePeerRequest{Name: &renamed, Category: &userCategory})
	if err != nil {
		t.Fatalf("UpdatePeer() error = %v", err)
	}
	if updated.Name != renamed || updated.Category != userCategory {
		t.Fatalf("updated peer = %#v", updated)
	}
	customCategory, err = service.UpdatePeerCategory(ctx, relay.ID, customCategory.ID, UpdatePeerCategoryRequest{Name: "Личные"})
	if err != nil || customCategory.Name != "Личные" {
		t.Fatalf("UpdatePeerCategory() = %#v, %v", customCategory, err)
	}
	peers, err := service.ListPeers(ctx, relay.ID, "DAY")
	if err != nil || len(peers) != 2 || peers[0].Category != "Личные" {
		t.Fatalf("peers after category rename = %#v, %v", peers, err)
	}
	if err := service.DeletePeerCategory(ctx, relay.ID, customCategory.ID); err == nil {
		t.Fatal("DeletePeerCategory() deleted a non-empty category")
	}
	if err := service.ReorderPeerCategories(ctx, relay.ID, UpdatePeerCategoryOrderRequest{Items: []PeerCategoryOrderItem{
		{CategoryID: customCategory.ID},
		{CategoryID: categories[0].ID},
		{CategoryID: categories[1].ID},
	}}); err != nil {
		t.Fatalf("ReorderPeerCategories() error = %v", err)
	}
	categories, err = service.ListPeerCategories(ctx, relay.ID)
	if err != nil || len(categories) != 3 || categories[0].ID != customCategory.ID || categories[0].SortOrder != 0 {
		t.Fatalf("reordered peer categories = %#v, %v", categories, err)
	}
	desired, err := service.Desired(ctx, relay.ID)
	if err != nil || desired.Revision != 2 {
		t.Fatalf("metadata update changed desired state = %#v, %v", desired, err)
	}

	if err := service.ReorderPeers(ctx, relay.ID, UpdatePeerOrderRequest{Items: []PeerOrderItem{
		{PeerID: proxy.Peer.ID, Category: "Служебные"},
		{PeerID: phone.Peer.ID, Category: "Пользовательские"},
	}}); err != nil {
		t.Fatalf("ReorderPeers() error = %v", err)
	}
	peers, err = service.ListPeers(ctx, relay.ID, "DAY")
	if err != nil {
		t.Fatalf("ListPeers() error = %v", err)
	}
	if len(peers) != 2 || peers[0].ID != proxy.Peer.ID || peers[0].SortOrder != 0 || peers[1].ID != phone.Peer.ID || peers[1].Category != "Пользовательские" || peers[1].SortOrder != 1 {
		t.Fatalf("reordered peers = %#v", peers)
	}
	if err := service.ReorderPeers(ctx, relay.ID, UpdatePeerOrderRequest{Items: []PeerOrderItem{{PeerID: phone.Peer.ID, Category: "Пользовательские"}}}); err == nil {
		t.Fatal("ReorderPeers() accepted an incomplete peer list")
	}
	if err := service.DeletePeerCategory(ctx, relay.ID, customCategory.ID); err != nil {
		t.Fatalf("DeletePeerCategory() after moving peers error = %v", err)
	}
	emptyCategory, err := service.CreatePeerCategory(ctx, relay.ID, CreatePeerCategoryRequest{Name: "Пустая"})
	if err != nil {
		t.Fatalf("CreatePeerCategory(empty) error = %v", err)
	}
	snapshot, err := service.Snapshot(ctx, relay.ID, "DAY")
	if err != nil {
		t.Fatalf("Snapshot() with empty category error = %v", err)
	}
	foundEmpty := false
	for _, category := range snapshot.Categories {
		foundEmpty = foundEmpty || category.ID == emptyCategory.ID
	}
	if !foundEmpty {
		t.Fatalf("empty category missing from snapshot = %#v", snapshot.Categories)
	}
	if err := service.DeletePeerCategory(ctx, relay.ID, emptyCategory.ID); err != nil {
		t.Fatalf("DeletePeerCategory(empty) error = %v", err)
	}

	now = now.Add(time.Second)
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey,
		PublicEndpoint:  relay.PublicEndpoint,
		AppliedRevision: 2,
		Peers:           []PeerCounter{{PublicKey: proxy.Peer.PublicKey, ReceiveBytes: 100, TransmitBytes: 50}},
	}); err != nil {
		t.Fatalf("Heartbeat(peer sample) error = %v", err)
	}
	deleteCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := service.DeletePeer(deleteCtx, relay.ID, proxy.Peer.ID); err != nil {
		t.Fatalf("DeletePeer() error = %v", err)
	}
	peers, err = service.ListPeers(ctx, relay.ID, "DAY")
	if err != nil || len(peers) != 1 || peers[0].ID != phone.Peer.ID {
		t.Fatalf("ListPeers() after delete = %#v, %v", peers, err)
	}
	var samples int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peer_metric_samples WHERE peer_id=$1::uuid`, proxy.Peer.ID).Scan(&samples); err != nil || samples != 0 {
		t.Fatalf("peer metric samples after delete = %d, %v", samples, err)
	}
}

func TestDeletePeerDoesNotWaitForHeartbeatRetentionCleanup(t *testing.T) {
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
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	relay, err := service.CreateRelay(ctx, CreateRelayRequest{
		Name: fmt.Sprintf("Retention lock %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.11:51820",
		ClientCIDR: "10.92.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("CreateRelay() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey,
		PublicEndpoint:  relay.PublicEndpoint,
		AppliedRevision: 0,
	}); err != nil {
		t.Fatalf("initial Heartbeat() error = %v", err)
	}
	oldMac, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "oldmac"})
	if err != nil {
		t.Fatalf("CreatePeer(oldmac) error = %v", err)
	}
	metricPeer, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Metric holder"})
	if err != nil {
		t.Fatalf("CreatePeer(metric holder) error = %v", err)
	}

	now = now.Add(2 * time.Hour)
	var sampleID string
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peer_metric_samples(id,peer_id,recorded_at,download_bytes,upload_bytes) VALUES(gen_random_uuid(),$1::uuid,$2,0,0) RETURNING id::text`, metricPeer.Peer.ID, now.Add(-32*24*time.Hour)).Scan(&sampleID); err != nil {
		t.Fatalf("insert stale metric sample: %v", err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Rollback(context.Background()) })
	if _, err := blocker.Exec(ctx, `SELECT id FROM wireguard_peer_metric_samples WHERE id=$1::uuid FOR UPDATE`, sampleID); err != nil {
		t.Fatalf("lock stale metric sample: %v", err)
	}

	cleanupService := NewService(pool, cipher)
	cleanupService.clock = func() time.Time { return now }
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- cleanupService.Heartbeat(ctx, relay.ID, Heartbeat{
			ServerPublicKey: serverKey,
			PublicEndpoint:  relay.PublicEndpoint,
			AppliedRevision: 2,
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var lastSeen time.Time
		if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM wireguard_relays WHERE id=$1::uuid`, relay.ID).Scan(&lastSeen); err != nil {
			t.Fatalf("read heartbeat commit marker: %v", err)
		}
		if lastSeen.Equal(now) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("heartbeat kept its relay row lock while retention cleanup was blocked")
		}
		time.Sleep(20 * time.Millisecond)
	}

	deleteCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := cleanupService.DeletePeer(deleteCtx, relay.ID, oldMac.Peer.ID); err != nil {
		t.Fatalf("DeletePeer(oldmac) while cleanup is blocked error = %v", err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release stale metric sample: %v", err)
	}
	select {
	case err := <-heartbeatDone:
		if err != nil {
			t.Fatalf("Heartbeat() after releasing cleanup error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Heartbeat() did not finish after retention cleanup was released")
	}

	var oldMacCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peers WHERE id=$1::uuid`, oldMac.Peer.ID).Scan(&oldMacCount); err != nil {
		t.Fatalf("read oldmac after delete: %v", err)
	}
	if oldMacCount != 0 {
		t.Fatalf("oldmac row count after delete = %d, want 0", oldMacCount)
	}
}
