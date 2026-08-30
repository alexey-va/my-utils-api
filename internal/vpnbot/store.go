package vpnbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

var ErrPeerLimitReached = errors.New("VPN bot peer limit reached")

const (
	StatusPending  Status = "PENDING"
	StatusApproved Status = "APPROVED"
	StatusRejected Status = "REJECTED"
	StatusBlocked  Status = "BLOCKED"
)

type Identity struct {
	TelegramUserID int64
	ChatID         int64
	Username       string
	DisplayName    string
}

type User struct {
	Identity
	Status      Status
	PeerLimit   int
	ApprovedBy  *int64
	RequestedAt time.Time
	DecidedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PeerOwnership struct {
	PeerID    string
	RelayID   string
	CreatedAt time.Time
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const userColumns = `telegram_user_id,chat_id,username,display_name,status,peer_limit,approved_by,requested_at,decided_at,created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scanUser(source scanner) (User, error) {
	var user User
	err := source.Scan(
		&user.TelegramUserID, &user.ChatID, &user.Username, &user.DisplayName,
		&user.Status, &user.PeerLimit, &user.ApprovedBy, &user.RequestedAt,
		&user.DecidedAt, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func (s *Store) User(ctx context.Context, telegramUserID int64) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, telegramUserID))
}

// RequestAccess creates a pending application or reopens a rejected one.
// Existing approved and blocked users keep their status while identity fields
// are refreshed from Telegram.
func (s *Store) RequestAccess(ctx context.Context, identity Identity) (User, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var previous Status
	err = tx.QueryRow(ctx, `SELECT status FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1 FOR UPDATE`, identity.TelegramUserID).Scan(&previous)
	now := time.Now().UTC()
	notify := false
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO wireguard_vpn_bot_users(telegram_user_id,chat_id,username,display_name,status,requested_at,created_at,updated_at) VALUES($1,$2,$3,$4,'PENDING',$5,$5,$5)`, identity.TelegramUserID, identity.ChatID, clean(identity.Username, 64), clean(identity.DisplayName, 160), now)
		notify = err == nil
	} else if err == nil {
		status := previous
		requestedAt := now
		if previous == StatusRejected {
			status = StatusPending
			notify = true
		} else {
			requestedAt = time.Time{}
		}
		_, err = tx.Exec(ctx, `UPDATE wireguard_vpn_bot_users SET chat_id=$2,username=$3,display_name=$4,status=$5::varchar,requested_at=COALESCE($6::timestamptz,requested_at),decided_at=CASE WHEN $5::varchar='PENDING' THEN NULL ELSE decided_at END,approved_by=CASE WHEN $5::varchar='PENDING' THEN NULL ELSE approved_by END,updated_at=$7 WHERE telegram_user_id=$1`, identity.TelegramUserID, identity.ChatID, clean(identity.Username, 64), clean(identity.DisplayName, 160), status, nullableTime(requestedAt), now)
	}
	if err != nil {
		return User{}, false, err
	}
	user, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, identity.TelegramUserID))
	if err != nil {
		return User{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, false, err
	}
	return user, notify, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (s *Store) TouchIdentity(ctx context.Context, identity Identity) error {
	_, err := s.pool.Exec(ctx, `UPDATE wireguard_vpn_bot_users SET chat_id=$2,username=$3,display_name=$4,updated_at=now() WHERE telegram_user_id=$1`, identity.TelegramUserID, identity.ChatID, clean(identity.Username, 64), clean(identity.DisplayName, 160))
	return err
}

func (s *Store) ListUsers(ctx context.Context, limit int) ([]User, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users ORDER BY CASE status WHEN 'PENDING' THEN 0 WHEN 'APPROVED' THEN 1 WHEN 'REJECTED' THEN 2 ELSE 3 END,requested_at DESC,telegram_user_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) SetStatus(ctx context.Context, telegramUserID, adminUserID int64, status Status) (User, error) {
	if status != StatusApproved && status != StatusRejected && status != StatusBlocked {
		return User{}, errors.New("unsupported VPN bot user status")
	}
	return scanUser(s.pool.QueryRow(ctx, `UPDATE wireguard_vpn_bot_users SET status=$2,approved_by=$3,decided_at=now(),updated_at=now() WHERE telegram_user_id=$1 RETURNING `+userColumns, telegramUserID, status, adminUserID))
}

func (s *Store) SetPeerLimit(ctx context.Context, telegramUserID int64, limit int) (User, error) {
	if limit < 1 || limit > 10 {
		return User{}, errors.New("VPN bot peer limit must be between 1 and 10")
	}
	return scanUser(s.pool.QueryRow(ctx, `UPDATE wireguard_vpn_bot_users SET peer_limit=$2,updated_at=now() WHERE telegram_user_id=$1 RETURNING `+userColumns, telegramUserID, limit))
}

func (s *Store) OwnedPeers(ctx context.Context, telegramUserID int64) ([]PeerOwnership, error) {
	rows, err := s.pool.Query(ctx, `SELECT owner.peer_id::text,peer.relay_id::text,owner.created_at FROM wireguard_vpn_bot_peer_owners owner JOIN wireguard_peers peer ON peer.id=owner.peer_id WHERE owner.telegram_user_id=$1 ORDER BY owner.created_at,owner.peer_id`, telegramUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PeerOwnership, 0)
	for rows.Next() {
		var owner PeerOwnership
		if err := rows.Scan(&owner.PeerID, &owner.RelayID, &owner.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, owner)
	}
	return result, rows.Err()
}

func (s *Store) AddOwnership(ctx context.Context, telegramUserID int64, relayID, peerID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var peerLimit int
	if err := tx.QueryRow(ctx, `SELECT peer_limit FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1 FOR UPDATE`, telegramUserID).Scan(&peerLimit); err != nil {
		return err
	}
	var ownedCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM wireguard_vpn_bot_peer_owners WHERE telegram_user_id=$1`, telegramUserID).Scan(&ownedCount); err != nil {
		return err
	}
	if ownedCount >= peerLimit {
		return ErrPeerLimitReached
	}
	result, err := tx.Exec(ctx, `INSERT INTO wireguard_vpn_bot_peer_owners(peer_id,telegram_user_id) SELECT id,$2 FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$3::uuid ON CONFLICT(peer_id) DO NOTHING`, peerID, telegramUserID, relayID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("WireGuard peer %s already has an owner", peerID)
	}
	return tx.Commit(ctx)
}

func (s *Store) Ownership(ctx context.Context, telegramUserID int64, peerID string) (PeerOwnership, error) {
	var owner PeerOwnership
	err := s.pool.QueryRow(ctx, `SELECT owner.peer_id::text,peer.relay_id::text,owner.created_at FROM wireguard_vpn_bot_peer_owners owner JOIN wireguard_peers peer ON peer.id=owner.peer_id WHERE owner.telegram_user_id=$1 AND owner.peer_id=$2::uuid`, telegramUserID, peerID).Scan(&owner.PeerID, &owner.RelayID, &owner.CreatedAt)
	return owner, err
}

func (s *Store) RecordEvent(ctx context.Context, actorID, targetID int64, action, peerID string, details map[string]any) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var peer any
	if strings.TrimSpace(peerID) != "" {
		peer = peerID
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO wireguard_vpn_bot_audit_events(actor_telegram_user_id,target_telegram_user_id,action,peer_id,details) VALUES($1,$2,$3,$4::uuid,$5::jsonb)`, actorID, targetID, clean(action, 48), peer, encoded)
	return err
}

func clean(value string, max int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= max {
		return value
	}
	return string([]rune(value)[:max])
}
