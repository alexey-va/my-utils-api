package vpnbot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreApprovalAndOwnershipIsolation(t *testing.T) {
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
	store := NewStore(pool)
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	identity := Identity{TelegramUserID: userID, ChatID: userID, Username: "vpn_test", DisplayName: "VPN Test"}
	user, notify, err := store.RequestAccess(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, userID)
	})
	if !notify || user.Status != StatusPending || user.PeerLimit != 1 {
		t.Fatalf("initial request = %#v, notify=%v", user, notify)
	}
	if _, notify, err := store.RequestAccess(ctx, identity); err != nil || notify {
		t.Fatalf("repeated request notify=%v error=%v", notify, err)
	}
	user, err = store.ApproveUser(ctx, userID, 7)
	if err != nil || user.Status != StatusApproved || user.ApprovedBy == nil || *user.ApprovedBy != 7 {
		t.Fatalf("approved user=%#v error=%v", user, err)
	}
	if _, err := store.SetPeerLimit(ctx, userID, 3); err != nil {
		t.Fatal(err)
	}

	var relayID, peerID string
	name := fmt.Sprintf("VPN bot store test %d", userID)
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_relays(id,name,public_endpoint,client_cidr,client_dns,interface_name,agent_token_hash,created_at,updated_at) VALUES(gen_random_uuid(),$1,'203.0.113.10:51820','10.89.0.0/29','1.1.1.1','wg-users','hash',now(),now()) RETURNING id::text`, name).Scan(&relayID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relayID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relayID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peers(id,relay_id,name,public_key,private_key_ciphertext,private_key_nonce,assigned_ip,created_at,updated_at) VALUES(gen_random_uuid(),$1::uuid,'Phone',$2,'cipher','nonce','10.89.0.2',now(),now()) RETURNING id::text`, relayID, fmt.Sprintf("public-%d", userID)).Scan(&peerID); err != nil {
		t.Fatal(err)
	}
	if err := store.AddOwnership(ctx, userID, relayID, peerID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPeerLimit(ctx, userID, 1); err != nil {
		t.Fatal(err)
	}
	var secondPeerID string
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peers(id,relay_id,name,public_key,private_key_ciphertext,private_key_nonce,assigned_ip,created_at,updated_at) VALUES(gen_random_uuid(),$1::uuid,'Tablet',$2,'cipher','nonce','10.89.0.3',now(),now()) RETURNING id::text`, relayID, fmt.Sprintf("public-second-%d", userID)).Scan(&secondPeerID); err != nil {
		t.Fatal(err)
	}
	if err := store.AddOwnership(ctx, userID, relayID, secondPeerID, false); !errors.Is(err, ErrPeerLimitReached) {
		t.Fatalf("second ownership error = %v, want ErrPeerLimitReached", err)
	}
	if err := store.AddOwnership(ctx, userID, relayID, secondPeerID, true); err != nil {
		t.Fatalf("unlimited ownership error = %v", err)
	}
	owned, err := store.Ownership(ctx, userID, peerID)
	if err != nil || owned.RelayID != relayID {
		t.Fatalf("ownership=%#v error=%v", owned, err)
	}
	if _, err := store.Ownership(ctx, userID+1, peerID); err == nil {
		t.Fatal("another Telegram user must not resolve the peer ownership")
	}
	if _, err := store.BlockUser(ctx, userID, 7); err != nil {
		t.Fatal(err)
	}
	if err := store.AddOwnership(ctx, userID, relayID, secondPeerID, true); !errors.Is(err, ErrAccessNotApproved) {
		t.Fatalf("blocked ownership error = %v, want ErrAccessNotApproved", err)
	}
	if user, err = store.ApproveUser(ctx, userID, 7); err != nil || user.Status != StatusApproved {
		t.Fatalf("reapproved user=%#v error=%v", user, err)
	}
	var disabledPeers int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peers WHERE id=ANY($1::uuid[]) AND NOT enabled`, []string{peerID, secondPeerID}).Scan(&disabledPeers); err != nil {
		t.Fatal(err)
	}
	if disabledPeers != 0 {
		t.Fatalf("disabled owned peers after approval = %d", disabledPeers)
	}
	if user, err = store.RejectUser(ctx, userID, 7); err != nil || user.Status != StatusRejected {
		t.Fatalf("rejected user=%#v error=%v", user, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peers WHERE id=ANY($1::uuid[]) AND NOT enabled`, []string{peerID, secondPeerID}).Scan(&disabledPeers); err != nil {
		t.Fatal(err)
	}
	if disabledPeers != 2 {
		t.Fatalf("enabled owned peers after rejection; disabled=%d", disabledPeers)
	}
	if err := store.AddOwnership(ctx, userID, relayID, secondPeerID, true); !errors.Is(err, ErrAccessNotApproved) {
		t.Fatalf("rejected ownership error = %v, want ErrAccessNotApproved", err)
	}
	if user, err = store.ApproveUser(ctx, userID, 7); err != nil || user.Status != StatusApproved {
		t.Fatalf("approved after rejection user=%#v error=%v", user, err)
	}
	admin, err := store.EnsureAdmin(ctx, Identity{
		TelegramUserID: userID,
		ChatID:         userID,
		Username:       "vpn_admin",
		DisplayName:    "VPN Admin",
	})
	if err != nil || admin.Status != StatusApproved || admin.Username != "vpn_admin" {
		t.Fatalf("ensured admin=%#v error=%v", admin, err)
	}
}

func TestBlockUserSerializesWithOwnershipAndRejectsTheLatePeer(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	barrier := &blockRelayUpdateBarrier{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(barrier.unblock)
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 2
	config.ConnConfig.Tracer = barrier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := NewStore(pool)
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	identity := Identity{TelegramUserID: userID, ChatID: userID, Username: "vpn_block_race", DisplayName: "VPN Block Race"}
	if _, _, err := store.RequestAccess(ctx, identity); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, userID)
	})
	if _, err := store.ApproveUser(ctx, userID, 7); err != nil {
		t.Fatal(err)
	}

	var relayID, ownedPeerID, latePeerID string
	name := fmt.Sprintf("VPN bot block race %d", userID)
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_relays(id,name,public_endpoint,client_cidr,client_dns,interface_name,agent_token_hash,created_at,updated_at) VALUES(gen_random_uuid(),$1,'203.0.113.11:51820','10.90.0.0/29','1.1.1.1','wg-users','hash',now(),now()) RETURNING id::text`, name).Scan(&relayID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relayID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relayID)
	})
	for index, target := range []*string{&ownedPeerID, &latePeerID} {
		if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peers(id,relay_id,name,public_key,private_key_ciphertext,private_key_nonce,assigned_ip,created_at,updated_at) VALUES(gen_random_uuid(),$1::uuid,$2,$3,'cipher','nonce',$4,now(),now()) RETURNING id::text`, relayID, fmt.Sprintf("Peer %d", index), fmt.Sprintf("block-race-public-%d-%d", userID, index), fmt.Sprintf("10.90.0.%d", index+2)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AddOwnership(ctx, userID, relayID, ownedPeerID, false); err != nil {
		t.Fatal(err)
	}

	type blockResult struct {
		user User
		err  error
	}
	blockedResult := make(chan blockResult, 1)
	go func() {
		blocked, blockErr := store.BlockUser(ctx, userID, 7)
		blockedResult <- blockResult{user: blocked, err: blockErr}
	}()
	select {
	case <-barrier.entered:
	case result := <-blockedResult:
		t.Fatalf("BlockUser returned before the barrier: user=%#v error=%v", result.user, result.err)
	case <-time.After(3 * time.Second):
		t.Fatal("BlockUser did not reach the relay update barrier")
	}

	waitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	err = store.AddOwnership(waitCtx, userID, relayID, latePeerID, false)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent ownership error = %v, want bounded row-lock cancellation", err)
	}
	barrier.unblock()
	var result blockResult
	select {
	case result = <-blockedResult:
	case <-time.After(3 * time.Second):
		t.Fatal("BlockUser did not finish after releasing the barrier")
	}
	if result.err != nil || result.user.Status != StatusBlocked {
		t.Fatalf("blocked user=%#v error=%v", result.user, result.err)
	}
	if err := store.AddOwnership(ctx, userID, relayID, latePeerID, false); !errors.Is(err, ErrAccessNotApproved) {
		t.Fatalf("late ownership after block error = %v, want ErrAccessNotApproved", err)
	}
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM wireguard_peers WHERE id=$1::uuid`, ownedPeerID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("owned peer remained enabled after the account committed as BLOCKED")
	}
}

type blockRelayUpdateBarrier struct {
	once     sync.Once
	released sync.Once
	entered  chan struct{}
	release  chan struct{}
}

func (b *blockRelayUpdateBarrier) unblock() {
	b.released.Do(func() { close(b.release) })
}

func (b *blockRelayUpdateBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "UPDATE wireguard_relays SET desired_revision=desired_revision+1") {
		b.once.Do(func() {
			close(b.entered)
			<-b.release
		})
	}
	return ctx
}

func (*blockRelayUpdateBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestAddOwnershipRejectsNonApprovedUser(t *testing.T) {
	pool, ctx := vpnBotTestPool(t)
	store := NewStore(pool)
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	if _, _, err := store.RequestAccess(ctx, Identity{TelegramUserID: userID, ChatID: userID, DisplayName: "Pending"}); err != nil {
		t.Fatal(err)
	}
	relayID, peerID := insertVPNBotPeer(t, pool, userID, false)

	if err := store.AddOwnership(ctx, userID, relayID, peerID, false); err == nil {
		t.Fatal("AddOwnership() error = nil for a pending user")
	}
	var owners int
	var enabled bool
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_vpn_bot_peer_owners WHERE peer_id=$1::uuid`, peerID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT enabled FROM wireguard_peers WHERE id=$1::uuid`, peerID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT desired_revision FROM wireguard_relays WHERE id=$1::uuid`, relayID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if owners != 0 || enabled || revision != 0 {
		t.Fatalf("owners=%d enabled=%v revision=%d", owners, enabled, revision)
	}
}

func TestAddOwnershipAtomicallyActivatesApprovedPeer(t *testing.T) {
	pool, ctx := vpnBotTestPool(t)
	store := NewStore(pool)
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	if _, _, err := store.RequestAccess(ctx, Identity{TelegramUserID: userID, ChatID: userID, DisplayName: "Approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApproveUser(ctx, userID, 7); err != nil {
		t.Fatal(err)
	}
	relayID, peerID := insertVPNBotPeer(t, pool, userID, false)

	if err := store.AddOwnership(ctx, userID, relayID, peerID, false); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT enabled FROM wireguard_peers WHERE id=$1::uuid`, peerID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT desired_revision FROM wireguard_relays WHERE id=$1::uuid`, relayID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if !enabled || revision != 1 {
		t.Fatalf("enabled=%v revision=%d, want enabled peer at revision 1", enabled, revision)
	}
}

func TestSetStatusAtomicallyDisablesOwnedPeers(t *testing.T) {
	pool, ctx := vpnBotTestPool(t)
	store := NewStore(pool)
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	if _, _, err := store.RequestAccess(ctx, Identity{TelegramUserID: userID, ChatID: userID, DisplayName: "Blocked"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApproveUser(ctx, userID, 7); err != nil {
		t.Fatal(err)
	}
	relayID, peerID := insertVPNBotPeer(t, pool, userID, true)
	if _, err := pool.Exec(ctx, `INSERT INTO wireguard_vpn_bot_peer_owners(peer_id,telegram_user_id) VALUES($1::uuid,$2)`, peerID, userID); err != nil {
		t.Fatal(err)
	}

	user, err := store.BlockUser(ctx, userID, 7)
	if err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT enabled FROM wireguard_peers WHERE id=$1::uuid`, peerID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT desired_revision FROM wireguard_relays WHERE id=$1::uuid`, relayID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if user.Status != StatusBlocked || enabled || revision != 1 {
		t.Fatalf("user=%s enabled=%v revision=%d", user.Status, enabled, revision)
	}
}

func vpnBotTestPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
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
	return pool, ctx
}

func insertVPNBotPeer(t *testing.T, pool *pgxpool.Pool, userID int64, enabled bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var relayID, peerID string
	name := fmt.Sprintf("VPN bot atomic test %d", userID)
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_relays(id,name,public_endpoint,client_cidr,client_dns,interface_name,agent_token_hash,created_at,updated_at) VALUES(gen_random_uuid(),$1,'203.0.113.10:51820','10.89.0.0/29','1.1.1.1','wg-users','hash',now(),now()) RETURNING id::text`, name).Scan(&relayID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_peers WHERE relay_id=$1::uuid`, relayID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relayID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_audit_events WHERE actor_telegram_user_id=$1 OR target_telegram_user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, userID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peers(id,relay_id,name,public_key,private_key_ciphertext,private_key_nonce,assigned_ip,enabled,created_at,updated_at) VALUES(gen_random_uuid(),$1::uuid,'Phone',$2,'cipher','nonce','10.89.0.2',$3,now(),now()) RETURNING id::text`, relayID, fmt.Sprintf("public-%d", userID), enabled).Scan(&peerID); err != nil {
		t.Fatal(err)
	}
	return relayID, peerID
}
