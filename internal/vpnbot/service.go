package vpnbot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/alexey-va/my-utils-api/internal/telegram"
	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"
)

type Messenger interface {
	SendHTMLMessage(context.Context, int64, string, string) (int, error)
	SendProtectedPhoto(context.Context, int64, []byte, string) error
	SendProtectedDocument(context.Context, int64, []byte, string, string, string) error
	SetMyCommands(context.Context, []telegram.BotCommand) error
	SetMyCommandsForChat(context.Context, int64, []telegram.BotCommand) error
}

type WireGuard interface {
	ListPeers(context.Context, string, string) ([]wireguard.Peer, error)
	CreatePeer(context.Context, string, wireguard.CreatePeerRequest) (wireguard.PeerCredentials, error)
	Credentials(context.Context, string, string) (wireguard.PeerCredentials, error)
	Metrics(context.Context, string, string, string) (wireguard.Metrics, error)
	ReissuePeerCredentials(context.Context, string, string) (wireguard.PeerCredentials, error)
	DeletePeer(context.Context, string, string) error
}

type Repository interface {
	User(context.Context, int64) (User, error)
	EnsureAdmin(context.Context, Identity) (User, error)
	RequestAccess(context.Context, Identity) (User, bool, error)
	TouchIdentity(context.Context, Identity) error
	ListUsers(context.Context, int) ([]User, error)
	ApproveUser(context.Context, int64, int64) (User, error)
	RejectUser(context.Context, int64, int64) (User, error)
	BlockUser(context.Context, int64, int64) (User, error)
	SetPeerLimit(context.Context, int64, int) (User, error)
	OwnedPeers(context.Context, int64) ([]PeerOwnership, error)
	AddOwnership(context.Context, int64, string, string, bool) error
	Ownership(context.Context, int64, string) (PeerOwnership, error)
	RecordEvent(context.Context, int64, int64, string, string, map[string]any) error
}

type Config struct {
	RelayID      string
	AdminUserIDs []int64
}

type Service struct {
	config                  Config
	store                   Repository
	wg                      WireGuard
	bot                     Messenger
	admins                  map[int64]bool
	commandMu               sync.Mutex
	adminCommandsConfigured map[int64]bool
}

func NewService(config Config, store Repository, wg WireGuard, bot Messenger) *Service {
	admins := make(map[int64]bool, len(config.AdminUserIDs))
	for _, id := range config.AdminUserIDs {
		if id > 0 {
			admins[id] = true
		}
	}
	config.RelayID = strings.TrimSpace(config.RelayID)
	return &Service{
		config: config, store: store, wg: wg, bot: bot, admins: admins,
		adminCommandsConfigured: make(map[int64]bool, len(admins)),
	}
}

func (s *Service) Name() string { return "vpn-telegram-menu" }

func (s *Service) Warm(ctx context.Context) error {
	userCommands := []telegram.BotCommand{
		{Command: "start", Description: "Открыть VPN-меню или отправить заявку"},
		{Command: "tunnels", Description: "Мои туннели"},
		{Command: "help", Description: "Инструкция по установке"},
	}
	if err := s.bot.SetMyCommands(ctx, userCommands); err != nil {
		return err
	}
	return nil
}

func (s *Service) Dispatch(ctx context.Context, message telegram.InboundMessage) error {
	if message.UserID <= 0 || message.ChatID == 0 {
		return nil
	}
	if message.ChatID != message.UserID || (message.ChatType != "" && message.ChatType != "private") {
		_, err := s.bot.SendHTMLMessage(ctx, message.ChatID, "Этот бот работает только в личном чате.", "")
		return err
	}
	text := normalizeCommand(message.Text)
	if s.admins[message.UserID] {
		s.ensureAdminCommands(ctx, message.UserID)
		if text == "/start" {
			text = "/admin"
		}
		return s.dispatchAdmin(ctx, message, text)
	}
	return s.dispatchUser(ctx, message, text)
}

func (s *Service) ensureAdminCommands(ctx context.Context, adminID int64) {
	s.commandMu.Lock()
	if s.adminCommandsConfigured[adminID] {
		s.commandMu.Unlock()
		return
	}
	s.commandMu.Unlock()
	commands := []telegram.BotCommand{
		{Command: "start", Description: "Открыть админ-меню VPN"},
		{Command: "admin", Description: "Администрирование доступа"},
		{Command: "tunnels", Description: "Мои туннели без лимита"},
		{Command: "help", Description: "Инструкция по установке"},
	}
	if err := s.bot.SetMyCommandsForChat(ctx, adminID, commands); err != nil {
		slog.WarnContext(ctx, "VPN bot admin command menu setup failed", "admin_id", adminID, "error", err)
		return
	}
	s.commandMu.Lock()
	s.adminCommandsConfigured[adminID] = true
	s.commandMu.Unlock()
}

func (s *Service) dispatchUser(ctx context.Context, message telegram.InboundMessage, text string) error {
	identity := identityFrom(message)
	user, err := s.store.User(ctx, message.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		if text != "/start" && text != "vpn:request" {
			_, sendErr := s.bot.SendHTMLMessage(ctx, message.ChatID, "Нажми /start, чтобы отправить заявку на VPN.", "")
			return sendErr
		}
		return s.requestAccess(ctx, identity)
	}
	if err != nil {
		return err
	}
	if err := s.store.TouchIdentity(ctx, identity); err != nil {
		return err
	}
	user.Identity = identity
	if (text == "/start" || text == "vpn:request") && user.Status == StatusRejected {
		return s.requestAccess(ctx, identity)
	}
	switch user.Status {
	case StatusPending:
		return s.sendPending(ctx, user.ChatID)
	case StatusRejected:
		_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, "Заявка отклонена. Можно отправить новую заявку.", "Отправить заявку:vpn:request")
		return err
	case StatusBlocked:
		_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, "Доступ к VPN-боту заблокирован администратором.", "")
		return err
	case StatusApproved:
		return s.dispatchApproved(ctx, user, text)
	default:
		return fmt.Errorf("unsupported VPN bot status %q", user.Status)
	}
}

func (s *Service) requestAccess(ctx context.Context, identity Identity) error {
	user, notify, err := s.store.RequestAccess(ctx, identity)
	if err != nil {
		return err
	}
	if notify {
		s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "ACCESS_REQUESTED", "", nil)
		for adminID := range s.admins {
			text := fmt.Sprintf("<b>Новая заявка на VPN</b>\n%s\n<code>%d</code>", userLabel(user), user.TelegramUserID)
			buttons := fmt.Sprintf("✅ Одобрить:vpn:admin:approve:%d,❌ Отклонить:vpn:admin:reject:%d", user.TelegramUserID, user.TelegramUserID)
			if _, sendErr := s.bot.SendHTMLMessage(ctx, adminID, text, buttons); sendErr != nil {
				slog.WarnContext(ctx, "VPN bot admin notification failed", "admin_id", adminID, "error", sendErr)
			}
		}
	}
	return s.sendPending(ctx, user.ChatID)
}

func (s *Service) sendPending(ctx context.Context, chatID int64) error {
	_, err := s.bot.SendHTMLMessage(ctx, chatID, "<b>Заявка отправлена</b>\nАдминистратор должен одобрить доступ. Бот напишет сюда после решения.", "Обновить статус:vpn:home")
	return err
}

func (s *Service) dispatchApproved(ctx context.Context, user User, text string) error {
	switch {
	case text == "/start", text == "/menu", text == "vpn:home", text == "":
		return s.sendHome(ctx, user)
	case text == "/help", text == "vpn:help":
		return s.sendHelp(ctx, user.ChatID)
	case text == "/tunnels", text == "vpn:list":
		return s.sendTunnelList(ctx, user)
	case text == "vpn:create":
		return s.createTunnel(ctx, user)
	case strings.HasPrefix(text, "vpn:peer:"):
		return s.sendPeer(ctx, user, strings.TrimPrefix(text, "vpn:peer:"))
	case strings.HasPrefix(text, "vpn:config:"):
		return s.sendConfig(ctx, user, strings.TrimPrefix(text, "vpn:config:"))
	case strings.HasPrefix(text, "vpn:qr:"):
		return s.sendQR(ctx, user, strings.TrimPrefix(text, "vpn:qr:"))
	case strings.HasPrefix(text, "vpn:stats:"):
		return s.sendStats(ctx, user, strings.TrimPrefix(text, "vpn:stats:"))
	case strings.HasPrefix(text, "vpn:reissue-confirm:"):
		peerID := strings.TrimPrefix(text, "vpn:reissue-confirm:")
		return s.confirmReissue(ctx, user, peerID)
	case strings.HasPrefix(text, "vpn:reissue:"):
		return s.reissue(ctx, user, strings.TrimPrefix(text, "vpn:reissue:"))
	case strings.HasPrefix(text, "vpn:delete-confirm:"):
		peerID := strings.TrimPrefix(text, "vpn:delete-confirm:")
		return s.confirmDelete(ctx, user, peerID)
	case strings.HasPrefix(text, "vpn:delete:"):
		return s.deleteTunnel(ctx, user, strings.TrimPrefix(text, "vpn:delete:"))
	default:
		_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, "Используй кнопки меню — VPN-бот не интерпретирует свободный текст.", "Меню:vpn:home")
		return err
	}
}

func (s *Service) sendHome(ctx context.Context, user User) error {
	owned, err := s.store.OwnedPeers(ctx, user.TelegramUserID)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("<b>VPN</b>\nТуннелей: <code>%s</code>\n\nЗдесь нет ИИ: все операции выполняются только по кнопкам.", s.tunnelCountLabel(user, len(owned)))
	buttons := "Мои туннели:vpn:list,➕ Новый туннель:vpn:create;📖 Установка:vpn:help"
	_, err = s.bot.SendHTMLMessage(ctx, user.ChatID, text, buttons)
	return err
}

func (s *Service) sendHelp(ctx context.Context, chatID int64) error {
	text := `<b>Как подключить WireGuard</b>

<b>iPhone / iPad</b>
1. Установи <a href="https://apps.apple.com/app/wireguard/id1441195209">WireGuard</a>.
2. Нажми «Добавить туннель» → «Создать из QR-кода» и отсканируй QR с другого экрана.
3. Либо открой присланный файл <code>.conf</code> через WireGuard.

<b>Android</b>
1. Установи <a href="https://play.google.com/store/apps/details?id=com.wireguard.android">WireGuard</a>.
2. Нажми <code>+</code> → «Сканировать QR-код» или импортируй файл <code>.conf</code>.

<b>Компьютер</b>
Скачай официальный клиент на <a href="https://www.wireguard.com/install/">wireguard.com/install</a> и импортируй <code>.conf</code>.

Конфигурация содержит приватный ключ. Не пересылай файл и QR другим людям.`
	_, err := s.bot.SendHTMLMessage(ctx, chatID, text, "Мои туннели:vpn:list,Меню:vpn:home")
	return err
}

func (s *Service) sendTunnelList(ctx context.Context, user User) error {
	owned, peers, err := s.ownedPeers(ctx, user.TelegramUserID)
	if err != nil {
		return err
	}
	if len(owned) == 0 {
		_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, "Туннелей пока нет.", "➕ Создать:vpn:create,Меню:vpn:home")
		return err
	}
	rows := make([]string, 0, len(owned)+1)
	for _, ownership := range owned {
		if peer, ok := peers[ownership.PeerID]; ok {
			rows = append(rows, fmt.Sprintf("%s:vpn:peer:%s", buttonText(peer.Name), peer.ID))
		}
	}
	rows = append(rows, "Меню:vpn:home")
	_, err = s.bot.SendHTMLMessage(ctx, user.ChatID, fmt.Sprintf("<b>Мои туннели</b>\n<code>%s</code>", s.tunnelCountLabel(user, len(owned))), strings.Join(rows, ";"))
	return err
}

func (s *Service) createTunnel(ctx context.Context, user User) error {
	owned, err := s.store.OwnedPeers(ctx, user.TelegramUserID)
	if err != nil {
		return err
	}
	unlimited := s.admins[user.TelegramUserID]
	if !unlimited && len(owned) >= user.PeerLimit {
		_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, "Достигнут лимит туннелей. Администратор может увеличить его.", "Мои туннели:vpn:list")
		return err
	}
	name := clean(fmt.Sprintf("%s · %d-%d", displayName(user.Identity), user.TelegramUserID, len(owned)+1), 120)
	credentials, err := s.wg.CreatePeer(ctx, s.config.RelayID, wireguard.CreatePeerRequest{Name: name, Category: "VPN bot"})
	if err != nil {
		return err
	}
	if err := s.store.AddOwnership(ctx, user.TelegramUserID, s.config.RelayID, credentials.Peer.ID, unlimited); err != nil {
		_ = s.wg.DeletePeer(context.WithoutCancel(ctx), s.config.RelayID, credentials.Peer.ID)
		if errors.Is(err, ErrPeerLimitReached) {
			_, sendErr := s.bot.SendHTMLMessage(ctx, user.ChatID, "Достигнут лимит туннелей. Администратор может увеличить его.", "Мои туннели:vpn:list")
			return sendErr
		}
		if errors.Is(err, ErrAccessNotApproved) {
			_, sendErr := s.bot.SendHTMLMessage(ctx, user.ChatID, "Доступ к VPN уже не одобрен. Новый туннель не создан.", "Меню:vpn:home")
			return sendErr
		}
		return err
	}
	s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "TUNNEL_CREATED", credentials.Peer.ID, nil)
	if _, err := s.bot.SendHTMLMessage(ctx, user.ChatID, fmt.Sprintf("<b>Туннель создан</b>\n%s · <code>%s</code>", html.EscapeString(credentials.Peer.Name), html.EscapeString(credentials.Peer.AssignedIP)), ""); err != nil {
		return err
	}
	return s.deliverCredentials(ctx, user, credentials)
}

func (s *Service) sendPeer(ctx context.Context, user User, peerID string) error {
	owner, err := s.store.Ownership(ctx, user.TelegramUserID, peerID)
	if err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	peers, err := s.wg.ListPeers(ctx, owner.RelayID, "MONTH")
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if peer.ID != peerID {
			continue
		}
		handshake := "ещё не подключался"
		if peer.LatestHandshakeAt != nil {
			handshake = peer.LatestHandshakeAt.Local().Format("02.01 15:04")
		}
		text := fmt.Sprintf("<b>%s</b>\nIP: <code>%s</code>\nСтатус: <code>%s</code>\nПоследнее подключение: %s", html.EscapeString(peer.Name), html.EscapeString(peer.AssignedIP), enabledLabel(peer.Enabled), handshake)
		buttons := fmt.Sprintf("QR:vpn:qr:%s,Файл .conf:vpn:config:%s;📊 Статистика:vpn:stats:%s;♻️ Перевыпустить:vpn:reissue-confirm:%s,🗑 Удалить:vpn:delete-confirm:%s;Назад:vpn:list", peerID, peerID, peerID, peerID, peerID)
		_, err = s.bot.SendHTMLMessage(ctx, user.ChatID, text, buttons)
		return err
	}
	return s.notOwned(ctx, user.ChatID)
}

func (s *Service) sendConfig(ctx context.Context, user User, peerID string) error {
	owner, err := s.store.Ownership(ctx, user.TelegramUserID, peerID)
	if err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	credentials, err := s.wg.Credentials(ctx, owner.RelayID, peerID)
	if err != nil {
		return err
	}
	s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "CONFIG_DELIVERED", peerID, map[string]any{"format": "conf"})
	return s.bot.SendProtectedDocument(ctx, user.ChatID, []byte(credentials.ClientConfig), credentials.FileName, "text/plain", "WireGuard-конфигурация. Не пересылай её другим людям.")
}

func (s *Service) sendQR(ctx context.Context, user User, peerID string) error {
	owner, err := s.store.Ownership(ctx, user.TelegramUserID, peerID)
	if err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	credentials, err := s.wg.Credentials(ctx, owner.RelayID, peerID)
	if err != nil {
		return err
	}
	png, err := qrcode.Encode(credentials.ClientConfig, qrcode.Medium, 768)
	if err != nil {
		return err
	}
	s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "CONFIG_DELIVERED", peerID, map[string]any{"format": "qr"})
	return s.bot.SendProtectedPhoto(ctx, user.ChatID, png, "Сканируй в WireGuard. QR содержит приватный ключ — не пересылай его.")
}

func (s *Service) sendStats(ctx context.Context, user User, peerID string) error {
	owner, err := s.store.Ownership(ctx, user.TelegramUserID, peerID)
	if err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	metrics, err := s.wg.Metrics(ctx, owner.RelayID, peerID, "MONTH")
	if err != nil {
		return err
	}
	total := metrics.Summary.DownloadBytes + metrics.Summary.UploadBytes
	text := fmt.Sprintf("<b>Трафик за 30 дней</b>\nСкачано: <code>%s</code>\nОтправлено: <code>%s</code>\nВсего: <code>%s</code>", formatBytes(metrics.Summary.DownloadBytes), formatBytes(metrics.Summary.UploadBytes), formatBytes(total))
	_, err = s.bot.SendHTMLMessage(ctx, user.ChatID, text, fmt.Sprintf("Назад:vpn:peer:%s", peerID))
	return err
}

func (s *Service) confirmReissue(ctx context.Context, user User, peerID string) error {
	if _, err := s.store.Ownership(ctx, user.TelegramUserID, peerID); err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	text := "<b>Перевыпустить туннель?</b>\nСтарый QR и файл перестанут работать после синхронизации сервера."
	buttons := fmt.Sprintf("Да, перевыпустить:vpn:reissue:%s;Отмена:vpn:peer:%s", peerID, peerID)
	_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, text, buttons)
	return err
}

func (s *Service) reissue(ctx context.Context, user User, peerID string) error {
	owner, err := s.store.Ownership(ctx, user.TelegramUserID, peerID)
	if err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	credentials, err := s.wg.ReissuePeerCredentials(ctx, owner.RelayID, peerID)
	if err != nil {
		return err
	}
	s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "TUNNEL_REISSUED", peerID, nil)
	if _, err := s.bot.SendHTMLMessage(ctx, user.ChatID, "Туннель перевыпущен. Отправляю новый QR и файл.", ""); err != nil {
		return err
	}
	return s.deliverCredentials(ctx, user, credentials)
}

func (s *Service) confirmDelete(ctx context.Context, user User, peerID string) error {
	if _, err := s.store.Ownership(ctx, user.TelegramUserID, peerID); err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	text := "<b>Удалить туннель?</b>\nПодключение перестанет работать. Это действие нельзя отменить."
	buttons := fmt.Sprintf("Да, удалить:vpn:delete:%s;Отмена:vpn:peer:%s", peerID, peerID)
	_, err := s.bot.SendHTMLMessage(ctx, user.ChatID, text, buttons)
	return err
}

func (s *Service) deleteTunnel(ctx context.Context, user User, peerID string) error {
	owner, err := s.store.Ownership(ctx, user.TelegramUserID, peerID)
	if err != nil {
		return s.notOwned(ctx, user.ChatID)
	}
	if err := s.wg.DeletePeer(ctx, owner.RelayID, peerID); err != nil {
		return err
	}
	s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "TUNNEL_DELETED", peerID, nil)
	_, err = s.bot.SendHTMLMessage(ctx, user.ChatID, "Туннель удалён.", "Мои туннели:vpn:list,Создать новый:vpn:create")
	return err
}

func (s *Service) deliverCredentials(ctx context.Context, user User, credentials wireguard.PeerCredentials) error {
	png, err := qrcode.Encode(credentials.ClientConfig, qrcode.Medium, 768)
	if err != nil {
		return err
	}
	if err := s.bot.SendProtectedPhoto(ctx, user.ChatID, png, "QR содержит приватный ключ. Не пересылай его."); err != nil {
		return err
	}
	if err := s.bot.SendProtectedDocument(ctx, user.ChatID, []byte(credentials.ClientConfig), credentials.FileName, "text/plain", "WireGuard-конфигурация"); err != nil {
		return err
	}
	s.recordEvent(ctx, user.TelegramUserID, user.TelegramUserID, "CONFIG_DELIVERED", credentials.Peer.ID, map[string]any{"format": "qr+conf"})
	return s.sendHelp(ctx, user.ChatID)
}

func (s *Service) ownedPeers(ctx context.Context, telegramUserID int64) ([]PeerOwnership, map[string]wireguard.Peer, error) {
	owned, err := s.store.OwnedPeers(ctx, telegramUserID)
	if err != nil {
		return nil, nil, err
	}
	byRelay := map[string][]PeerOwnership{}
	for _, owner := range owned {
		byRelay[owner.RelayID] = append(byRelay[owner.RelayID], owner)
	}
	result := map[string]wireguard.Peer{}
	for relayID := range byRelay {
		peers, err := s.wg.ListPeers(ctx, relayID, "MONTH")
		if err != nil {
			return nil, nil, err
		}
		for _, peer := range peers {
			result[peer.ID] = peer
		}
	}
	return owned, result, nil
}

func (s *Service) dispatchAdmin(ctx context.Context, message telegram.InboundMessage, text string) error {
	if adminTunnelCommand(text) {
		user, err := s.store.EnsureAdmin(ctx, identityFrom(message))
		if err != nil {
			return err
		}
		return s.dispatchApproved(ctx, user, text)
	}
	switch {
	case text == "/admin", text == "vpn:admin:home":
		_, err := s.bot.SendHTMLMessage(ctx, message.ChatID, "<b>VPN · администрирование</b>\nДоступ выдаётся только после твоего решения.", "Мои туннели:vpn:home;Новые заявки:vpn:admin:pending;Все пользователи:vpn:admin:users")
		return err
	case text == "vpn:admin:pending":
		return s.sendAdminUsers(ctx, message.ChatID, true)
	case text == "vpn:admin:users":
		return s.sendAdminUsers(ctx, message.ChatID, false)
	case strings.HasPrefix(text, "vpn:admin:user:"):
		return s.sendAdminUser(ctx, message.ChatID, parseID(strings.TrimPrefix(text, "vpn:admin:user:")))
	case strings.HasPrefix(text, "vpn:admin:approve:"):
		return s.adminSetStatus(ctx, message.UserID, parseID(strings.TrimPrefix(text, "vpn:admin:approve:")), StatusApproved)
	case strings.HasPrefix(text, "vpn:admin:reject:"):
		return s.adminSetStatus(ctx, message.UserID, parseID(strings.TrimPrefix(text, "vpn:admin:reject:")), StatusRejected)
	case strings.HasPrefix(text, "vpn:admin:block:"):
		return s.adminSetStatus(ctx, message.UserID, parseID(strings.TrimPrefix(text, "vpn:admin:block:")), StatusBlocked)
	case strings.HasPrefix(text, "vpn:admin:limit:"):
		return s.adminSetLimit(ctx, message.UserID, strings.TrimPrefix(text, "vpn:admin:limit:"))
	default:
		_, err := s.bot.SendHTMLMessage(ctx, message.ChatID, "Используй кнопки админ-меню — свободный текст ничего не меняет.", "Админ-меню:vpn:admin:home")
		return err
	}
}

func (s *Service) sendAdminUsers(ctx context.Context, chatID int64, pendingOnly bool) error {
	users, err := s.store.ListUsers(ctx, 100)
	if err != nil {
		return err
	}
	rows := make([]string, 0)
	for _, user := range users {
		if pendingOnly && user.Status != StatusPending {
			continue
		}
		label := fmt.Sprintf("%s · %s", buttonText(displayName(user.Identity)), statusLabel(user.Status))
		rows = append(rows, fmt.Sprintf("%s:vpn:admin:user:%d", label, user.TelegramUserID))
	}
	count := len(rows)
	rows = append(rows, "Назад:vpn:admin:home")
	title := "Все пользователи"
	if pendingOnly {
		title = "Новые заявки"
	}
	_, err = s.bot.SendHTMLMessage(ctx, chatID, fmt.Sprintf("<b>%s</b>\nНайдено: <code>%d</code>", title, count), strings.Join(rows, ";"))
	return err
}

func (s *Service) sendAdminUser(ctx context.Context, chatID, userID int64) error {
	if userID <= 0 {
		return nil
	}
	user, err := s.store.User(ctx, userID)
	if err != nil {
		return err
	}
	owned, err := s.store.OwnedPeers(ctx, userID)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("<b>%s</b>\nID: <code>%d</code>\nСтатус: <code>%s</code>\nТуннели: <code>%s</code>", userLabel(user), user.TelegramUserID, statusLabel(user.Status), s.tunnelCountLabel(user, len(owned)))
	rows := []string{}
	if !s.admins[userID] {
		if user.Status != StatusApproved {
			rows = append(rows, fmt.Sprintf("✅ Одобрить:vpn:admin:approve:%d", userID))
		}
		if user.Status == StatusPending {
			rows = append(rows, fmt.Sprintf("❌ Отклонить:vpn:admin:reject:%d", userID))
		}
		if user.Status != StatusBlocked {
			rows = append(rows, fmt.Sprintf("⛔ Заблокировать:vpn:admin:block:%d", userID))
		}
		rows = append(rows, fmt.Sprintf("Лимит 1:vpn:admin:limit:%d:1,Лимит 2:vpn:admin:limit:%d:2;Лимит 3:vpn:admin:limit:%d:3,Лимит 5:vpn:admin:limit:%d:5", userID, userID, userID, userID))
	}
	rows = append(rows, "Все пользователи:vpn:admin:users")
	_, err = s.bot.SendHTMLMessage(ctx, chatID, text, strings.Join(rows, ";"))
	return err
}

func (s *Service) adminSetStatus(ctx context.Context, adminID, userID int64, status Status) error {
	if userID <= 0 {
		return nil
	}
	if s.admins[userID] {
		// Configured administrators are the bot trust boundary: their own VPN
		// access is always approved and unlimited, so crafted callbacks must not
		// be able to block or downgrade them.
		return s.sendAdminUser(ctx, adminID, userID)
	}
	var user User
	var err error
	if status == StatusBlocked {
		// Access transitions update the bot account, every owned peer and the
		// affected relay revisions in one database transaction while holding the
		// same user row lock as AddOwnership. Concurrent creates and opposite
		// access decisions therefore cannot leave status and peer state divergent.
		user, err = s.store.BlockUser(ctx, userID, adminID)
	} else if status == StatusApproved {
		user, err = s.store.ApproveUser(ctx, userID, adminID)
	} else if status == StatusRejected {
		user, err = s.store.RejectUser(ctx, userID, adminID)
	} else {
		return errors.New("unsupported VPN bot user status")
	}
	if err != nil {
		return err
	}
	s.recordEvent(ctx, adminID, userID, "ACCESS_"+string(status), "", nil)
	message := "Заявка отклонена."
	buttons := "Отправить новую заявку:vpn:request"
	if status == StatusApproved {
		message = "<b>Доступ к VPN одобрен</b>\nТеперь можно создать туннель."
		buttons = "Открыть меню:vpn:home"
	} else if status == StatusBlocked {
		message = "Доступ к VPN-боту заблокирован администратором."
		buttons = ""
	}
	if _, sendErr := s.bot.SendHTMLMessage(ctx, user.ChatID, message, buttons); sendErr != nil {
		return sendErr
	}
	return s.sendAdminUser(ctx, adminID, userID)
}

func (s *Service) adminSetLimit(ctx context.Context, adminID int64, raw string) error {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return nil
	}
	userID := parseID(parts[0])
	limit, err := strconv.Atoi(parts[1])
	if err != nil || userID <= 0 {
		return nil
	}
	if s.admins[userID] {
		return s.sendAdminUser(ctx, adminID, userID)
	}
	if _, err := s.store.SetPeerLimit(ctx, userID, limit); err != nil {
		return err
	}
	s.recordEvent(ctx, adminID, userID, "PEER_LIMIT_CHANGED", "", map[string]any{"limit": limit})
	return s.sendAdminUser(ctx, adminID, userID)
}

func (s *Service) notOwned(ctx context.Context, chatID int64) error {
	_, err := s.bot.SendHTMLMessage(ctx, chatID, "Туннель не найден или принадлежит другому пользователю.", "Мои туннели:vpn:list")
	return err
}

func (s *Service) tunnelCountLabel(user User, count int) string {
	if s.admins[user.TelegramUserID] {
		return fmt.Sprintf("%d · без лимита", count)
	}
	return fmt.Sprintf("%d из %d", count, user.PeerLimit)
}

func adminTunnelCommand(text string) bool {
	switch text {
	case "/tunnels", "/help", "/menu", "vpn:home", "vpn:list", "vpn:create", "vpn:help":
		return true
	}
	for _, prefix := range []string{
		"vpn:peer:", "vpn:config:", "vpn:qr:", "vpn:stats:",
		"vpn:reissue-confirm:", "vpn:reissue:", "vpn:delete-confirm:", "vpn:delete:",
	} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func (s *Service) recordEvent(ctx context.Context, actorID, targetID int64, action, peerID string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	if err := s.store.RecordEvent(context.WithoutCancel(ctx), actorID, targetID, action, peerID, details); err != nil {
		slog.WarnContext(ctx, "VPN bot audit write failed", "action", action, "error", err)
	}
}

func identityFrom(message telegram.InboundMessage) Identity {
	name := strings.TrimSpace(strings.Join([]string{message.FirstName, message.LastName}, " "))
	if name == "" {
		name = strings.TrimSpace(message.Username)
	}
	if name == "" {
		name = fmt.Sprintf("Telegram %d", message.UserID)
	}
	return Identity{TelegramUserID: message.UserID, ChatID: message.ChatID, Username: message.Username, DisplayName: name}
}

func displayName(identity Identity) string {
	if value := strings.TrimSpace(identity.DisplayName); value != "" {
		return value
	}
	if value := strings.TrimSpace(identity.Username); value != "" {
		return "@" + value
	}
	return fmt.Sprintf("Telegram %d", identity.TelegramUserID)
}

func userLabel(user User) string {
	label := html.EscapeString(displayName(user.Identity))
	if username := strings.TrimSpace(user.Username); username != "" {
		label += " (@" + html.EscapeString(username) + ")"
	}
	return label
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "активен"
	}
	return "отключён"
}

func statusLabel(status Status) string {
	switch status {
	case StatusPending:
		return "ожидает"
	case StatusApproved:
		return "одобрен"
	case StatusRejected:
		return "отклонён"
	case StatusBlocked:
		return "заблокирован"
	default:
		return string(status)
	}
}

func buttonText(value string) string {
	value = strings.NewReplacer(":", " ", ",", " ", ";", " ", "\n", " ").Replace(strings.TrimSpace(value))
	if len([]rune(value)) > 36 {
		value = string([]rune(value)[:36]) + "…"
	}
	return value
}

func normalizeCommand(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/") {
		if space := strings.IndexByte(value, ' '); space >= 0 {
			value = value[:space]
		}
		if at := strings.IndexByte(value, '@'); at >= 0 {
			value = value[:at]
		}
	}
	return value
}

func parseID(value string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id
}

func formatBytes(value int64) string {
	if value < 0 {
		value = 0
	}
	const gb = 1_000_000_000
	if value < gb {
		return fmt.Sprintf("%.1f МБ", float64(value)/1_000_000)
	}
	return fmt.Sprintf("%.2f ГБ", float64(value)/gb)
}
