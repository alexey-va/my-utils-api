package vpnbot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

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
	user, err = store.SetStatus(ctx, userID, 7, StatusApproved)
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
	if err := store.AddOwnership(ctx, userID, relayID, peerID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPeerLimit(ctx, userID, 1); err != nil {
		t.Fatal(err)
	}
	var secondPeerID string
	if err := pool.QueryRow(ctx, `INSERT INTO wireguard_peers(id,relay_id,name,public_key,private_key_ciphertext,private_key_nonce,assigned_ip,created_at,updated_at) VALUES(gen_random_uuid(),$1::uuid,'Tablet',$2,'cipher','nonce','10.89.0.3',now(),now()) RETURNING id::text`, relayID, fmt.Sprintf("public-second-%d", userID)).Scan(&secondPeerID); err != nil {
		t.Fatal(err)
	}
	if err := store.AddOwnership(ctx, userID, relayID, secondPeerID); !errors.Is(err, ErrPeerLimitReached) {
		t.Fatalf("second ownership error = %v, want ErrPeerLimitReached", err)
	}
	owned, err := store.Ownership(ctx, userID, peerID)
	if err != nil || owned.RelayID != relayID {
		t.Fatalf("ownership=%#v error=%v", owned, err)
	}
	if _, err := store.Ownership(ctx, userID+1, peerID); err == nil {
		t.Fatal("another Telegram user must not resolve the peer ownership")
	}
}
