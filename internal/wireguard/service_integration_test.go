package wireguard

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5"
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
	var persisted Relay
	for _, candidate := range relays {
		if candidate.ID == relay.ID {
			persisted = candidate
			break
		}
	}
	if persisted.ID == "" {
		t.Fatalf("created relay %s is missing from ListRelays(): %#v", relay.ID, relays)
	}
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

func TestCreateRelayRejectsConcurrentCaseInsensitiveDuplicateNames(t *testing.T) {
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
	service := NewService(pool, nil)
	name := fmt.Sprintf("Concurrent relay %d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE lower(name)=lower($1)`, name)
	})

	type result struct {
		relay CreatedRelay
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, candidate := range []string{name, strings.ToUpper(name)} {
		candidate := candidate
		go func() {
			<-start
			relay, createErr := service.CreateRelay(ctx, CreateRelayRequest{
				Name:           candidate,
				PublicEndpoint: "203.0.113.10:51820",
				ClientCIDR:     "10.95.0.0/29",
				ClientDNS:      "1.1.1.1",
			})
			results <- result{relay: relay, err: createErr}
		}()
	}
	close(start)
	first, second := <-results, <-results

	created, conflicts := 0, 0
	for _, got := range []result{first, second} {
		if got.err == nil {
			created++
			continue
		}
		var domainError *workout.Error
		if errors.As(got.err, &domainError) && domainError.Status == http.StatusConflict {
			conflicts++
			continue
		}
		t.Fatalf("CreateRelay concurrent error = %v, want HTTP 409", got.err)
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("concurrent results: created=%d conflicts=%d, values=%#v/%#v", created, conflicts, first, second)
	}
}

type wireGuardQueryBarrierContextKey struct{}

type wireGuardQueryBarrier struct {
	pattern     string
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

func newWireGuardQueryBarrier(pattern string) *wireGuardQueryBarrier {
	return &wireGuardQueryBarrier{pattern: pattern, started: make(chan struct{}), release: make(chan struct{})}
}

func (barrier *wireGuardQueryBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(wireGuardQueryBarrierContextKey{}) == true && strings.Contains(data.SQL, barrier.pattern) {
		barrier.startedOnce.Do(func() { close(barrier.started) })
		<-barrier.release
	}
	return ctx
}

func (*wireGuardQueryBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (barrier *wireGuardQueryBarrier) unblock() {
	barrier.releaseOnce.Do(func() { close(barrier.release) })
}

func TestDesiredRevisionAndPeersUseOneSnapshot(t *testing.T) {
	barrier := newWireGuardQueryBarrier("SELECT public_key,assigned_ip FROM wireguard_peers")
	pool, service, relay, _ := tracedWireGuardFixture(t, barrier)
	t.Cleanup(barrier.unblock)
	if _, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "First"}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		state DesiredState
		err   error
	}
	results := make(chan result, 1)
	ctx := context.WithValue(context.Background(), wireGuardQueryBarrierContextKey{}, true)
	go func() {
		state, err := service.Desired(ctx, relay.ID)
		results <- result{state: state, err: err}
	}()
	select {
	case <-barrier.started:
	case <-time.After(3 * time.Second):
		t.Fatal("Desired() did not reach the peer query")
	}
	if _, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "Second"}); err != nil {
		t.Fatal(err)
	}
	barrier.unblock()
	got := <-results
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.state.Revision != 1 || len(got.state.Peers) != 1 {
		t.Fatalf("Desired() mixed snapshots: revision=%d peers=%#v", got.state.Revision, got.state.Peers)
	}
	var revision int64
	if err := pool.QueryRow(context.Background(), `SELECT desired_revision FROM wireguard_relays WHERE id=$1::uuid`, relay.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 2 {
		t.Fatalf("persisted revision=%d, want 2", revision)
	}
}

func TestSnapshotDoesNotMixPeersAndMetricsAcrossQueries(t *testing.T) {
	barrier := newWireGuardQueryBarrier("FROM wireguard_peer_metric_samples m JOIN wireguard_peers")
	pool, service, relay, now := tracedWireGuardFixture(t, barrier)
	t.Cleanup(barrier.unblock)
	first, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		snapshot Snapshot
		err      error
	}
	results := make(chan result, 1)
	ctx := context.WithValue(context.Background(), wireGuardQueryBarrierContextKey{}, true)
	go func() {
		snapshot, err := service.Snapshot(ctx, relay.ID, "HOUR")
		results <- result{snapshot: snapshot, err: err}
	}()
	select {
	case <-barrier.started:
	case <-time.After(3 * time.Second):
		t.Fatal("Snapshot() did not reach the metric query")
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO wireguard_peer_metric_samples(id,peer_id,recorded_at,download_bytes,upload_bytes) VALUES(gen_random_uuid(),$1::uuid,$2,1,1)`, first.Peer.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	barrier.unblock()
	got := <-results
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.snapshot.Peers) != 1 || len(got.snapshot.PeerMetrics) != 1 || got.snapshot.PeerMetrics[first.Peer.ID].Summary.DownloadBytes != 0 {
		t.Fatalf("Snapshot() mixed snapshots: peers=%d metrics=%#v", len(got.snapshot.Peers), got.snapshot.PeerMetrics)
	}
}

func TestCreatePeerCanStageDisabledPeerWithoutPublishingRevision(t *testing.T) {
	_, service, relay, _ := tracedWireGuardFixture(t, nil)
	disabled := false
	created, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "Pending ownership", Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if created.Peer.Enabled {
		t.Fatalf("created peer enabled=%v, want staged disabled peer", created.Peer.Enabled)
	}
	desired, err := service.Desired(context.Background(), relay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if desired.Revision != 0 || len(desired.Peers) != 0 {
		t.Fatalf("Desired() = revision %d peers %#v, disabled peer was published", desired.Revision, desired.Peers)
	}
}

func TestBlockedVPNBotPeerCannotBeReenabledThroughWireGuardService(t *testing.T) {
	pool, service, relay, _ := tracedWireGuardFixture(t, nil)
	created, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "Blocked owner"})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := service.UpdatePeer(context.Background(), relay.ID, created.Peer.ID, UpdatePeerRequest{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	if _, err := pool.Exec(context.Background(), `INSERT INTO wireguard_vpn_bot_users(telegram_user_id,chat_id,display_name,status) VALUES($1,$1,'Blocked owner','BLOCKED')`, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, userID)
	})
	if _, err := pool.Exec(context.Background(), `INSERT INTO wireguard_vpn_bot_peer_owners(peer_id,telegram_user_id) VALUES($1::uuid,$2)`, created.Peer.ID, userID); err != nil {
		t.Fatal(err)
	}

	enabled := true
	if _, err := service.UpdatePeer(context.Background(), relay.ID, created.Peer.ID, UpdatePeerRequest{Enabled: &enabled}); err == nil {
		t.Fatal("UpdatePeer() enabled a blocked VPN bot peer")
	}
	if err := service.SetPeerIDsEnabled(context.Background(), relay.ID, []string{created.Peer.ID}, true); err == nil {
		t.Fatal("SetPeerIDsEnabled() enabled a blocked VPN bot peer")
	}
	if _, err := service.ReissuePeerCredentialsForVPNBot(context.Background(), relay.ID, created.Peer.ID, userID); err == nil {
		t.Fatal("ReissuePeerCredentialsForVPNBot() mutated a blocked user's peer")
	}
	if err := service.DeletePeerForVPNBot(context.Background(), relay.ID, created.Peer.ID, userID); err == nil {
		t.Fatal("DeletePeerForVPNBot() deleted a blocked user's peer")
	}
	var persistedEnabled bool
	var revision int64
	var publicKey string
	if err := pool.QueryRow(context.Background(), `SELECT enabled FROM wireguard_peers WHERE id=$1::uuid`, created.Peer.ID).Scan(&persistedEnabled); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT public_key FROM wireguard_peers WHERE id=$1::uuid`, created.Peer.ID).Scan(&publicKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT desired_revision FROM wireguard_relays WHERE id=$1::uuid`, relay.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if persistedEnabled || revision != 2 || publicKey != created.Peer.PublicKey {
		t.Fatalf("enabled=%v revision=%d keyChanged=%v, rejected mutation changed state", persistedEnabled, revision, publicKey != created.Peer.PublicKey)
	}

	if _, err := pool.Exec(context.Background(), `UPDATE wireguard_vpn_bot_users SET status='APPROVED',access_revision=access_revision+1 WHERE telegram_user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdatePeer(context.Background(), relay.ID, created.Peer.ID, UpdatePeerRequest{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled {
		t.Fatalf("approved owner's peer enabled=%v, want true", updated.Enabled)
	}
}

func TestVPNBotPeerMutationAndAuditCommitAtomically(t *testing.T) {
	pool, service, relay, _ := tracedWireGuardFixture(t, nil)
	created, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "Audited peer"})
	if err != nil {
		t.Fatal(err)
	}
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	if _, err := pool.Exec(context.Background(), `INSERT INTO wireguard_vpn_bot_users(telegram_user_id,chat_id,display_name,status) VALUES($1,$1,'Audited owner','APPROVED')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO wireguard_vpn_bot_peer_owners(peer_id,telegram_user_id) VALUES($1::uuid,$2)`, created.Peer.ID, userID); err != nil {
		t.Fatal(err)
	}
	triggerName := fmt.Sprintf("wireguard_fail_bot_audit_%d", userID)
	functionName := triggerName + "_fn"
	functionSQL := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
	IF NEW.target_telegram_user_id = %d AND NEW.action IN ('TUNNEL_REISSUED','TUNNEL_DELETED') THEN
		RAISE EXCEPTION 'forced VPN bot audit failure';
	END IF;
	RETURN NEW;
END
$function$`, functionName, userID)
	if _, err := pool.Exec(context.Background(), functionSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON wireguard_vpn_bot_audit_events FOR EACH ROW EXECUTE FUNCTION %s()`, triggerName, functionName)); err != nil {
		t.Fatal(err)
	}
	dropFailureTrigger := func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON wireguard_vpn_bot_audit_events`, triggerName))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	}
	t.Cleanup(func() {
		dropFailureTrigger()
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_audit_events WHERE actor_telegram_user_id=$1 OR target_telegram_user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, userID)
	})

	if _, err := service.ReissuePeerCredentialsForVPNBot(context.Background(), relay.ID, created.Peer.ID, userID); err == nil {
		t.Fatal("ReissuePeerCredentialsForVPNBot() error=nil after forced audit failure")
	}
	if err := service.DeletePeerForVPNBot(context.Background(), relay.ID, created.Peer.ID, userID); err == nil {
		t.Fatal("DeletePeerForVPNBot() error=nil after forced audit failure")
	}
	var publicKey string
	var revision int64
	var peerCount int
	if err := pool.QueryRow(context.Background(), `SELECT public_key FROM wireguard_peers WHERE id=$1::uuid`, created.Peer.ID).Scan(&publicKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT desired_revision FROM wireguard_relays WHERE id=$1::uuid`, relay.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM wireguard_peers WHERE id=$1::uuid`, created.Peer.ID).Scan(&peerCount); err != nil {
		t.Fatal(err)
	}
	if publicKey != created.Peer.PublicKey || revision != 1 || peerCount != 1 {
		t.Fatalf("failed audit committed mutation: keyChanged=%v revision=%d peers=%d", publicKey != created.Peer.PublicKey, revision, peerCount)
	}

	dropFailureTrigger()
	if _, err := service.ReissuePeerCredentialsForVPNBot(context.Background(), relay.ID, created.Peer.ID, userID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeletePeerForVPNBot(context.Background(), relay.ID, created.Peer.ID, userID); err != nil {
		t.Fatal(err)
	}
	var reissued, deleted int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE action='TUNNEL_REISSUED'),count(*) FILTER (WHERE action='TUNNEL_DELETED') FROM wireguard_vpn_bot_audit_events WHERE target_telegram_user_id=$1`, userID).Scan(&reissued, &deleted); err != nil {
		t.Fatal(err)
	}
	if reissued != 1 || deleted != 1 {
		t.Fatalf("audit counts reissued=%d deleted=%d", reissued, deleted)
	}
}

func TestHeartbeatRejectsAppliedRevisionRegressionWithoutInflatingTraffic(t *testing.T) {
	pool, service, relay, now := tracedWireGuardFixture(t, nil)
	peer, err := service.CreatePeer(context.Background(), relay.ID, CreatePeerRequest{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	service.clock = func() time.Time { return now.Add(time.Minute) }
	newer := Heartbeat{
		ServerPublicKey: *relay.ServerPublicKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 1,
		Peers: []PeerCounter{{PublicKey: peer.Peer.PublicKey, ReceiveBytes: 200, TransmitBytes: 300}},
	}
	if err := service.Heartbeat(context.Background(), relay.ID, newer); err != nil {
		t.Fatal(err)
	}
	service.clock = func() time.Time { return now.Add(2 * time.Minute) }
	stale := newer
	stale.AppliedRevision = 0
	stale.Peers = []PeerCounter{{PublicKey: peer.Peer.PublicKey, ReceiveBytes: 100, TransmitBytes: 150}}
	if err := service.Heartbeat(context.Background(), relay.ID, stale); err == nil {
		t.Fatal("Heartbeat() accepted an applied revision regression")
	}
	var appliedRevision int64
	var rawReceive, rawTransmit, totalReceive, totalTransmit int64
	if err := pool.QueryRow(context.Background(), `SELECT relay.applied_revision,peer.raw_receive_bytes,peer.raw_transmit_bytes,peer.total_receive_bytes,peer.total_transmit_bytes FROM wireguard_relays relay JOIN wireguard_peers peer ON peer.relay_id=relay.id WHERE relay.id=$1::uuid AND peer.id=$2::uuid`, relay.ID, peer.Peer.ID).Scan(&appliedRevision, &rawReceive, &rawTransmit, &totalReceive, &totalTransmit); err != nil {
		t.Fatal(err)
	}
	if appliedRevision != 1 || rawReceive != 200 || rawTransmit != 300 || totalReceive != 200 || totalTransmit != 300 {
		t.Fatalf("applied=%d raw=%d/%d total=%d/%d", appliedRevision, rawReceive, rawTransmit, totalReceive, totalTransmit)
	}
}

func tracedWireGuardFixture(t *testing.T, tracer pgx.QueryTracer) (*pgxpool.Pool, *Service, CreatedRelay, time.Time) {
	t.Helper()
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
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
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	relay, err := service.CreateRelay(context.Background(), CreateRelayRequest{
		Name: fmt.Sprintf("WireGuard snapshot %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.10:51820",
		ClientCIDR: "10.94.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(context.Background(), relay.ID, Heartbeat{ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	relay.ServerPublicKey = &serverKey
	return pool, service, relay, now
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

type desiredBarrierContextKey struct{}

type desiredBarrierTracer struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (tracer *desiredBarrierTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(desiredBarrierContextKey{}) == true && strings.Contains(data.SQL, "SELECT public_key,assigned_ip FROM wireguard_peers") {
		tracer.once.Do(func() { close(tracer.started) })
		<-tracer.release
	}
	return ctx
}

func (*desiredBarrierTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestDesiredRevisionAndPeersComeFromOneDatabaseSnapshot(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	tracer := &desiredBarrierTracer{started: make(chan struct{}), release: make(chan struct{})}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, config)
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
	service.clock = func() time.Time { return time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC) }
	relay, err := service.CreateRelay(ctx, CreateRelayRequest{
		Name: fmt.Sprintf("Desired snapshot %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.10:51820",
		ClientCIDR: "10.94.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "First"}); err != nil {
		t.Fatal(err)
	}

	type desiredResult struct {
		state DesiredState
		err   error
	}
	result := make(chan desiredResult, 1)
	desiredCtx := context.WithValue(ctx, desiredBarrierContextKey{}, true)
	go func() {
		state, desiredErr := service.Desired(desiredCtx, relay.ID)
		result <- desiredResult{state: state, err: desiredErr}
	}()
	select {
	case <-tracer.started:
	case <-time.After(2 * time.Second):
		close(tracer.release)
		t.Fatal("Desired() did not reach the peer query")
	}
	if _, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Second"}); err != nil {
		close(tracer.release)
		t.Fatal(err)
	}
	close(tracer.release)
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.state.Revision != 1 || len(got.state.Peers) != 1 {
		t.Fatalf("Desired() mixed revisions: revision=%d peers=%#v", got.state.Revision, got.state.Peers)
	}
	fresh, err := service.Desired(ctx, relay.ID)
	if err != nil || fresh.Revision != 2 || len(fresh.Peers) != 2 {
		t.Fatalf("fresh Desired() = %#v, %v", fresh, err)
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

func TestDeletePeerBoundsSamePeerMetricCascadeLockAndSucceedsAfterRelease(t *testing.T) {
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
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	relay, err := service.CreateRelay(ctx, CreateRelayRequest{
		Name: fmt.Sprintf("Same-peer cascade %d", time.Now().UnixNano()), PublicEndpoint: "203.0.113.12:51820",
		ClientCIDR: "10.96.0.0/29", ClientDNS: "1.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relay.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relay.ID)
	})
	serverKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := service.Heartbeat(ctx, relay.ID, Heartbeat{
		ServerPublicKey: serverKey, PublicEndpoint: relay.PublicEndpoint, AppliedRevision: 0,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreatePeer(ctx, relay.ID, CreatePeerRequest{Name: "Contended peer"})
	if err != nil {
		t.Fatal(err)
	}
	var sampleID string
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peer_metric_samples(id,peer_id,recorded_at,download_bytes,upload_bytes) VALUES(gen_random_uuid(),$1::uuid,$2,0,0) RETURNING id::text`, created.Peer.ID, now).Scan(&sampleID); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Rollback(context.Background()) })
	if _, err := blocker.Exec(ctx, `SELECT id FROM wireguard_peer_metric_samples WHERE id=$1::uuid FOR UPDATE`, sampleID); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	deleteCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	err = service.DeletePeer(deleteCtx, relay.ID, created.Peer.ID)
	cancel()
	elapsed := time.Since(started)
	var domainError *workout.Error
	if !errors.As(err, &domainError) || domainError.Status != http.StatusConflict {
		t.Fatalf("contended DeletePeer error = %v, want HTTP 409", err)
	}
	if elapsed > 7*time.Second {
		t.Fatalf("contended DeletePeer took %v, want bounded lock wait", elapsed)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.DeletePeer(ctx, relay.ID, created.Peer.ID); err != nil {
		t.Fatalf("DeletePeer after releasing same-peer metric lock: %v", err)
	}
	var peerCount, sampleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peers WHERE id=$1::uuid`, created.Peer.ID).Scan(&peerCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peer_metric_samples WHERE id=$1::uuid`, sampleID).Scan(&sampleCount); err != nil {
		t.Fatal(err)
	}
	if peerCount != 0 || sampleCount != 0 {
		t.Fatalf("cascade cleanup after delete: peers=%d samples=%d", peerCount, sampleCount)
	}
}
