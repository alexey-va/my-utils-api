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

var (
	ErrPeerLimitReached  = errors.New("VPN bot peer limit reached")
	ErrAccessNotApproved = errors.New("VPN bot access is not approved")
	ErrStaleDecision     = errors.New("VPN bot access decision is stale")
)

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
	Status         Status
	AccessRevision int64
	PeerLimit      int
	ApprovedBy     *int64
	RequestedAt    time.Time
	DecidedAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PeerOwnership struct {
	PeerID    string
	RelayID   string
	CreatedAt time.Time
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const userColumns = `telegram_user_id,chat_id,username,display_name,status,access_revision,peer_limit,approved_by,requested_at,decided_at,created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scanUser(source scanner) (User, error) {
	var user User
	err := source.Scan(
		&user.TelegramUserID, &user.ChatID, &user.Username, &user.DisplayName,
		&user.Status, &user.AccessRevision, &user.PeerLimit, &user.ApprovedBy, &user.RequestedAt,
		&user.DecidedAt, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func (s *Store) User(ctx context.Context, telegramUserID int64) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, telegramUserID))
}

// EnsureAdmin provisions a configured bot administrator as an approved VPN
// owner through the same audited access transition as regular approvals.
// Configured admin IDs are already the service's trust boundary, so they can
// always recover their own access and owned peers.
func (s *Store) EnsureAdmin(ctx context.Context, identity Identity) (User, error) {
	for range 3 {
		user, _, err := s.RequestAccess(ctx, identity)
		if err != nil {
			return User{}, err
		}
		if user.Status == StatusApproved {
			return user, nil
		}
		approved, err := s.SetStatusIf(ctx, user.TelegramUserID, user.TelegramUserID, user.Status, user.AccessRevision, StatusApproved)
		if errors.Is(err, ErrStaleDecision) {
			continue
		}
		return approved, err
	}
	return User{}, ErrStaleDecision
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
		revisionIncrement := 0
		if previous == StatusRejected {
			status = StatusPending
			notify = true
			revisionIncrement = 1
		} else {
			requestedAt = time.Time{}
		}
		_, err = tx.Exec(ctx, `UPDATE wireguard_vpn_bot_users SET chat_id=$2,username=$3,display_name=$4,status=$5::varchar,access_revision=access_revision+$8,requested_at=COALESCE($6::timestamptz,requested_at),decided_at=CASE WHEN $5::varchar='PENDING' THEN NULL ELSE decided_at END,approved_by=CASE WHEN $5::varchar='PENDING' THEN NULL ELSE approved_by END,updated_at=$7 WHERE telegram_user_id=$1`, identity.TelegramUserID, identity.ChatID, clean(identity.Username, 64), clean(identity.DisplayName, 160), status, nullableTime(requestedAt), now, revisionIncrement)
	}
	if err != nil {
		return User{}, false, err
	}
	user, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1`, identity.TelegramUserID))
	if err != nil {
		return User{}, false, err
	}
	if notify {
		if _, err := createAuditEvent(ctx, tx, user.TelegramUserID, user.TelegramUserID, "ACCESS_REQUESTED", "", nil); err != nil {
			return User{}, false, err
		}
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

// ApproveUser serializes the access decision with AddOwnership and the opposite
// block transition, then enables every peer owned by the account atomically.
func (s *Store) ApproveUser(ctx context.Context, telegramUserID, adminUserID int64) (User, error) {
	return s.SetStatus(ctx, telegramUserID, adminUserID, StatusApproved)
}

// RejectUser revokes any access that may have been granted since the original
// application, making stale or replayed administrator callbacks safe.
func (s *Store) RejectUser(ctx context.Context, telegramUserID, adminUserID int64) (User, error) {
	return s.SetStatus(ctx, telegramUserID, adminUserID, StatusRejected)
}

// BlockUser serializes the access decision with AddOwnership and the opposite
// approval transition, then disables every peer owned by the account atomically.
func (s *Store) BlockUser(ctx context.Context, telegramUserID, adminUserID int64) (User, error) {
	return s.SetStatus(ctx, telegramUserID, adminUserID, StatusBlocked)
}

func (s *Store) SetStatus(ctx context.Context, telegramUserID, adminUserID int64, status Status) (User, error) {
	return s.setStatus(ctx, telegramUserID, adminUserID, nil, nil, status)
}

func (s *Store) SetStatusIf(ctx context.Context, telegramUserID, adminUserID int64, expectedStatus Status, expectedRevision int64, status Status) (User, error) {
	return s.setStatus(ctx, telegramUserID, adminUserID, &expectedStatus, &expectedRevision, status)
}

func (s *Store) setStatus(ctx context.Context, telegramUserID, adminUserID int64, expectedStatus *Status, expectedRevision *int64, status Status) (User, error) {
	if status != StatusApproved && status != StatusRejected && status != StatusBlocked {
		return User{}, errors.New("unsupported VPN bot user status")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	current, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1 FOR UPDATE`, telegramUserID))
	if err != nil {
		return User{}, err
	}
	if expectedStatus != nil && expectedRevision != nil && (current.Status != *expectedStatus || current.AccessRevision != *expectedRevision) {
		return User{}, ErrStaleDecision
	}

	// All bot ownership mutations lock the user first, then the affected relay
	// rows in UUID order, and only then peer rows. The common order keeps access
	// decisions and tunnel creation serializable without deadlocking the regular
	// WireGuard relay -> peer mutation path.
	rows, err := tx.Query(ctx, `
		SELECT relay.id::text
		FROM wireguard_relays relay
		WHERE relay.id IN (
			SELECT peer.relay_id
			FROM wireguard_vpn_bot_peer_owners owner
			JOIN wireguard_peers peer ON peer.id=owner.peer_id
			WHERE owner.telegram_user_id=$1
		)
		ORDER BY relay.id
		FOR UPDATE`, telegramUserID)
	if err != nil {
		return User{}, err
	}
	for rows.Next() {
		var relayID string
		if err := rows.Scan(&relayID); err != nil {
			rows.Close()
			return User{}, err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return User{}, err
	}
	rows.Close()

	enabled := status == StatusApproved
	changedRows, err := tx.Query(ctx, `
		UPDATE wireguard_peers peer
		SET enabled=$2,updated_at=$3
		FROM wireguard_vpn_bot_peer_owners owner
		WHERE owner.telegram_user_id=$1
		  AND owner.peer_id=peer.id
		  AND peer.enabled IS DISTINCT FROM $2
		RETURNING peer.relay_id::text`, telegramUserID, enabled, time.Now().UTC())
	if err != nil {
		return User{}, err
	}
	changedRelays := make(map[string]struct{})
	for changedRows.Next() {
		var relayID string
		if err := changedRows.Scan(&relayID); err != nil {
			changedRows.Close()
			return User{}, err
		}
		changedRelays[relayID] = struct{}{}
	}
	if err := changedRows.Err(); err != nil {
		changedRows.Close()
		return User{}, err
	}
	changedRows.Close()
	if len(changedRelays) > 0 {
		relayIDs := make([]string, 0, len(changedRelays))
		for relayID := range changedRelays {
			relayIDs = append(relayIDs, relayID)
		}
		if _, err := tx.Exec(ctx, `UPDATE wireguard_relays SET desired_revision=desired_revision+1,updated_at=now() WHERE id=ANY($1::uuid[])`, relayIDs); err != nil {
			return User{}, err
		}
	}
	updated, err := scanUser(tx.QueryRow(ctx, `UPDATE wireguard_vpn_bot_users SET status=$2,access_revision=access_revision+1,approved_by=$3,decided_at=now(),updated_at=now() WHERE telegram_user_id=$1 RETURNING `+userColumns, telegramUserID, status, adminUserID))
	if err != nil {
		return User{}, err
	}
	if _, err := createAuditEvent(ctx, tx, adminUserID, telegramUserID, "ACCESS_"+string(status), "", nil); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return updated, nil
}

func (s *Store) SetPeerLimit(ctx context.Context, telegramUserID, adminUserID int64, limit int) (User, error) {
	return s.setPeerLimit(ctx, telegramUserID, adminUserID, nil, limit)
}

func (s *Store) SetPeerLimitIf(ctx context.Context, telegramUserID, adminUserID, expectedRevision int64, limit int) (User, error) {
	return s.setPeerLimit(ctx, telegramUserID, adminUserID, &expectedRevision, limit)
}

func (s *Store) setPeerLimit(ctx context.Context, telegramUserID, adminUserID int64, expectedRevision *int64, limit int) (User, error) {
	if limit < 1 || limit > 10 {
		return User{}, errors.New("VPN bot peer limit must be between 1 and 10")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	current, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1 FOR UPDATE`, telegramUserID))
	if err != nil {
		return User{}, err
	}
	if expectedRevision != nil && current.AccessRevision != *expectedRevision {
		return User{}, ErrStaleDecision
	}
	updated, err := scanUser(tx.QueryRow(ctx, `UPDATE wireguard_vpn_bot_users SET peer_limit=$2,access_revision=access_revision+1,updated_at=now() WHERE telegram_user_id=$1 RETURNING `+userColumns, telegramUserID, limit))
	if err != nil {
		return User{}, err
	}
	if _, err := createAuditEvent(ctx, tx, adminUserID, telegramUserID, "PEER_LIMIT_CHANGED", "", map[string]any{"limit": limit}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return updated, nil
}

func (s *Store) OwnedPeers(ctx context.Context, telegramUserID int64) ([]PeerOwnership, error) {
	return queryOwnedPeers(ctx, s.pool, telegramUserID)
}

type rowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func queryOwnedPeers(ctx context.Context, source rowsQuerier, telegramUserID int64) ([]PeerOwnership, error) {
	rows, err := source.Query(ctx, `SELECT owner.peer_id::text,peer.relay_id::text,owner.created_at FROM wireguard_vpn_bot_peer_owners owner JOIN wireguard_peers peer ON peer.id=owner.peer_id WHERE owner.telegram_user_id=$1 ORDER BY owner.created_at,owner.peer_id`, telegramUserID)
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

func (s *Store) AddOwnership(ctx context.Context, telegramUserID int64, relayID, peerID string, ignorePeerLimit bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var status Status
	var peerLimit int
	if err := tx.QueryRow(ctx, `SELECT status,peer_limit FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1 FOR UPDATE`, telegramUserID).Scan(&status, &peerLimit); err != nil {
		return err
	}
	if status != StatusApproved {
		return ErrAccessNotApproved
	}
	var ownedCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM wireguard_vpn_bot_peer_owners WHERE telegram_user_id=$1`, telegramUserID).Scan(&ownedCount); err != nil {
		return err
	}
	if !ignorePeerLimit && ownedCount >= peerLimit {
		return ErrPeerLimitReached
	}
	var lockedRelayID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM wireguard_relays WHERE id=$1::uuid FOR UPDATE`, relayID).Scan(&lockedRelayID); err != nil {
		return err
	}
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$2::uuid FOR UPDATE`, peerID, relayID).Scan(&enabled); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `INSERT INTO wireguard_vpn_bot_peer_owners(peer_id,telegram_user_id) SELECT id,$2 FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$3::uuid ON CONFLICT(peer_id) DO NOTHING`, peerID, telegramUserID, relayID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("WireGuard peer %s already has an owner", peerID)
	}
	if !enabled {
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `UPDATE wireguard_peers SET enabled=TRUE,updated_at=$2 WHERE id=$1::uuid`, peerID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE wireguard_relays SET desired_revision=desired_revision+1,updated_at=$2 WHERE id=$1::uuid`, lockedRelayID, now); err != nil {
			return err
		}
	}
	if _, err := createAuditEvent(ctx, tx, telegramUserID, telegramUserID, "TUNNEL_CREATED", peerID, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Ownership(ctx context.Context, telegramUserID int64, peerID string) (PeerOwnership, error) {
	var owner PeerOwnership
	err := s.pool.QueryRow(ctx, `SELECT owner.peer_id::text,peer.relay_id::text,owner.created_at FROM wireguard_vpn_bot_peer_owners owner JOIN wireguard_peers peer ON peer.id=owner.peer_id WHERE owner.telegram_user_id=$1 AND owner.peer_id=$2::uuid`, telegramUserID, peerID).Scan(&owner.PeerID, &owner.RelayID, &owner.CreatedAt)
	return owner, err
}

// BeginApprovedDelivery records a credential-delivery attempt at the same
// linearization point as the approval and ownership check. The transaction is
// committed before returning so callers can safely perform database-backed
// reads or Telegram I/O without holding a pool connection or user lock.
func (s *Store) BeginApprovedDelivery(ctx context.Context, telegramUserID int64, peerID, format string) (PeerOwnership, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeerOwnership{}, "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var status Status
	if err := tx.QueryRow(ctx, `SELECT status FROM wireguard_vpn_bot_users WHERE telegram_user_id=$1 FOR UPDATE`, telegramUserID).Scan(&status); err != nil {
		return PeerOwnership{}, "", err
	}
	if status != StatusApproved {
		return PeerOwnership{}, "", ErrAccessNotApproved
	}
	var owner PeerOwnership
	if err := tx.QueryRow(ctx, `SELECT owner.peer_id::text,peer.relay_id::text,owner.created_at FROM wireguard_vpn_bot_peer_owners owner JOIN wireguard_peers peer ON peer.id=owner.peer_id WHERE owner.telegram_user_id=$1 AND owner.peer_id=$2::uuid`, telegramUserID, peerID).Scan(&owner.PeerID, &owner.RelayID, &owner.CreatedAt); err != nil {
		return PeerOwnership{}, "", err
	}
	eventID, err := createAuditEvent(ctx, tx, telegramUserID, telegramUserID, "CONFIG_DELIVERY_ATTEMPTED", peerID, map[string]any{"format": clean(format, 16)})
	if err != nil {
		return PeerOwnership{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return PeerOwnership{}, "", err
	}
	return owner, eventID, nil
}

type auditQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func createAuditEvent(ctx context.Context, source auditQueryer, actorID, targetID int64, action, peerID string, details map[string]any) (string, error) {
	if details == nil {
		details = map[string]any{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return "", err
	}
	var peer any
	if strings.TrimSpace(peerID) != "" {
		peer = peerID
	}
	var eventID string
	err = source.QueryRow(ctx, `INSERT INTO wireguard_vpn_bot_audit_events(actor_telegram_user_id,target_telegram_user_id,action,peer_id,details) VALUES($1,$2,$3,$4::uuid,$5::jsonb) RETURNING id::text`, actorID, targetID, clean(action, 48), peer, encoded).Scan(&eventID)
	return eventID, err
}

func (s *Store) CompleteEvent(ctx context.Context, eventID, action string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE wireguard_vpn_bot_audit_events SET action=$2,details=$3::jsonb WHERE id=$1::uuid`, eventID, clean(action, 48), encoded)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("VPN bot audit event %s not found", eventID)
	}
	return nil
}

func clean(value string, max int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= max {
		return value
	}
	return string([]rune(value)[:max])
}
