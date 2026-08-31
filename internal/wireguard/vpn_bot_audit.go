package wireguard

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func lockApprovedVPNBotOwnerTx(ctx context.Context, tx pgx.Tx, telegramUserID int64, peerID string) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT bot_user.status
		FROM wireguard_vpn_bot_peer_owners owner
		JOIN wireguard_vpn_bot_users bot_user ON bot_user.telegram_user_id=owner.telegram_user_id
		WHERE owner.peer_id=$1::uuid AND owner.telegram_user_id=$2
		FOR UPDATE OF bot_user`, peerID, telegramUserID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound("VPN bot peer ownership changed")
	}
	if err != nil {
		return err
	}
	if status != "APPROVED" {
		return conflict("VPN bot access is not approved")
	}
	return nil
}

func recordVPNBotAuditTx(ctx context.Context, tx pgx.Tx, telegramUserID int64, action, peerID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO wireguard_vpn_bot_audit_events(
			actor_telegram_user_id,target_telegram_user_id,action,peer_id,details
		) VALUES($1,$1,$2,$3::uuid,'{}'::jsonb)`, telegramUserID, action, peerID)
	return err
}
