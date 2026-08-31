package vpnbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/telegram"
	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/jackc/pgx/v5"
)

type fakeRepository struct {
	users           map[int64]User
	owners          map[int64][]PeerOwnership
	events          []string
	calls           []string
	addOwnershipErr error
	blockUserErr    error
	blockedPeers    map[string][]string
}

func (f *fakeRepository) User(_ context.Context, id int64) (User, error) {
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	return user, nil
}
func (f *fakeRepository) EnsureAdmin(_ context.Context, identity Identity) (User, error) {
	if f.users == nil {
		f.users = map[int64]User{}
	}
	user := f.users[identity.TelegramUserID]
	user.Identity = identity
	user.Status = StatusApproved
	if user.PeerLimit == 0 {
		user.PeerLimit = 1
	}
	f.users[identity.TelegramUserID] = user
	return user, nil
}
func (f *fakeRepository) RequestAccess(_ context.Context, identity Identity) (User, bool, error) {
	if f.users == nil {
		f.users = map[int64]User{}
	}
	user, exists := f.users[identity.TelegramUserID]
	notify := !exists || user.Status == StatusRejected
	if !exists {
		user = User{Identity: identity, Status: StatusPending, PeerLimit: 1}
	} else {
		user.Identity = identity
		if user.Status == StatusRejected {
			user.Status = StatusPending
		}
	}
	f.users[identity.TelegramUserID] = user
	return user, notify, nil
}
func (f *fakeRepository) TouchIdentity(_ context.Context, identity Identity) error {
	user := f.users[identity.TelegramUserID]
	user.Identity = identity
	f.users[identity.TelegramUserID] = user
	return nil
}
func (f *fakeRepository) ListUsers(context.Context, int) ([]User, error) {
	result := make([]User, 0, len(f.users))
	for _, user := range f.users {
		result = append(result, user)
	}
	return result, nil
}
func (f *fakeRepository) RejectUser(_ context.Context, id, adminID int64) (User, error) {
	f.calls = append(f.calls, "reject-transaction")
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	user.Status = StatusRejected
	user.ApprovedBy = &adminID
	f.users[id] = user
	return user, nil
}
func (f *fakeRepository) ApproveUser(_ context.Context, id, adminID int64) (User, error) {
	f.calls = append(f.calls, "approve-transaction")
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	user.Status = StatusApproved
	user.ApprovedBy = &adminID
	f.users[id] = user
	return user, nil
}
func (f *fakeRepository) BlockUser(_ context.Context, id, adminID int64) (User, error) {
	f.calls = append(f.calls, "block-transaction")
	if f.blockUserErr != nil {
		return User{}, f.blockUserErr
	}
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	f.blockedPeers = make(map[string][]string)
	for _, owner := range f.owners[id] {
		f.blockedPeers[owner.RelayID] = append(f.blockedPeers[owner.RelayID], owner.PeerID)
	}
	user.Status = StatusBlocked
	user.ApprovedBy = &adminID
	f.users[id] = user
	return user, nil
}
func (f *fakeRepository) SetPeerLimit(_ context.Context, id int64, limit int) (User, error) {
	user := f.users[id]
	user.PeerLimit = limit
	f.users[id] = user
	return user, nil
}
func (f *fakeRepository) OwnedPeers(_ context.Context, id int64) ([]PeerOwnership, error) {
	f.calls = append(f.calls, "owned-peers")
	return append([]PeerOwnership(nil), f.owners[id]...), nil
}
func (f *fakeRepository) AddOwnership(_ context.Context, id int64, relayID, peerID string, _ bool) error {
	if f.addOwnershipErr != nil {
		return f.addOwnershipErr
	}
	if f.owners == nil {
		f.owners = map[int64][]PeerOwnership{}
	}
	f.owners[id] = append(f.owners[id], PeerOwnership{PeerID: peerID, RelayID: relayID})
	return nil
}
func (f *fakeRepository) Ownership(_ context.Context, id int64, peerID string) (PeerOwnership, error) {
	for _, owner := range f.owners[id] {
		if owner.PeerID == peerID {
			return owner, nil
		}
	}
	return PeerOwnership{}, pgx.ErrNoRows
}
func (f *fakeRepository) RecordEvent(_ context.Context, _, _ int64, action, _ string, _ map[string]any) error {
	f.events = append(f.events, action)
	return nil
}

type fakeWireGuard struct {
	created       int
	credentials   int
	reissued      int
	deleted       int
	enabledCalls  []bool
	enabledPeers  [][]string
	enabledRelays []string
	peers         []wireguard.Peer
}

func (f *fakeWireGuard) ListPeers(context.Context, string, string) ([]wireguard.Peer, error) {
	return append([]wireguard.Peer(nil), f.peers...), nil
}
func (f *fakeWireGuard) CreatePeer(_ context.Context, _ string, request wireguard.CreatePeerRequest) (wireguard.PeerCredentials, error) {
	f.created++
	peer := wireguard.Peer{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", 100+f.created), Name: request.Name, AssignedIP: fmt.Sprintf("10.89.0.%d", 1+f.created), Enabled: true, CreatedAt: time.Now()}
	f.peers = append(f.peers, peer)
	return wireguard.PeerCredentials{Peer: peer, ClientConfig: "[Interface]\nPrivateKey = secret", FileName: "phone.conf"}, nil
}
func (f *fakeWireGuard) Credentials(context.Context, string, string) (wireguard.PeerCredentials, error) {
	f.credentials++
	return wireguard.PeerCredentials{Peer: f.peers[0], ClientConfig: "[Interface]\nPrivateKey = secret", FileName: "phone.conf"}, nil
}
func (f *fakeWireGuard) Metrics(context.Context, string, string, string) (wireguard.Metrics, error) {
	return wireguard.Metrics{Summary: wireguard.TrafficTotals{DownloadBytes: 2_000_000_000, UploadBytes: 500_000_000}}, nil
}
func (f *fakeWireGuard) ReissuePeerCredentials(context.Context, string, string) (wireguard.PeerCredentials, error) {
	f.reissued++
	return wireguard.PeerCredentials{Peer: f.peers[0], ClientConfig: "[Interface]\nPrivateKey = rotated", FileName: "phone.conf"}, nil
}
func (f *fakeWireGuard) DeletePeer(context.Context, string, string) error {
	f.deleted++
	return nil
}
func (f *fakeWireGuard) SetPeerIDsEnabled(_ context.Context, relayID string, peerIDs []string, enabled bool) error {
	f.enabledCalls = append(f.enabledCalls, enabled)
	f.enabledRelays = append(f.enabledRelays, relayID)
	f.enabledPeers = append(f.enabledPeers, append([]string(nil), peerIDs...))
	return nil
}

type fakeMessenger struct {
	messages     []string
	buttons      []string
	photos       int
	documents    int
	commands     []telegram.BotCommand
	chatCommands map[int64][]telegram.BotCommand
}

func (f *fakeMessenger) SendHTMLMessage(_ context.Context, _ int64, text, buttons string) (int, error) {
	f.messages = append(f.messages, text)
	f.buttons = append(f.buttons, buttons)
	return len(f.messages), nil
}
func (f *fakeMessenger) SendProtectedPhoto(context.Context, int64, []byte, string) error {
	f.photos++
	return nil
}
func (f *fakeMessenger) SendProtectedDocument(context.Context, int64, []byte, string, string, string) error {
	f.documents++
	return nil
}
func (f *fakeMessenger) SetMyCommands(_ context.Context, commands []telegram.BotCommand) error {
	f.commands = append([]telegram.BotCommand(nil), commands...)
	return nil
}
func (f *fakeMessenger) SetMyCommandsForChat(_ context.Context, chatID int64, commands []telegram.BotCommand) error {
	if f.chatCommands == nil {
		f.chatCommands = map[int64][]telegram.BotCommand{}
	}
	f.chatCommands[chatID] = append([]telegram.BotCommand(nil), commands...)
	return nil
}

func TestAccessRequestRequiresAdminApproval(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)
	message := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Username: "bob", FirstName: "Bob", Text: "/start"}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusPending || len(bot.messages) != 2 {
		t.Fatalf("request state=%#v messages=%#v", repo.users[42], bot.messages)
	}
	if !strings.Contains(bot.buttons[0], "vpn:admin:approve:42") || !strings.Contains(bot.messages[1], "Заявка отправлена") {
		t.Fatalf("approval flow messages=%#v buttons=%#v", bot.messages, bot.buttons)
	}
}

func TestRejectedUserCanSubmitAnotherRequest(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {
		Identity:  Identity{TelegramUserID: 42, ChatID: 42, DisplayName: "Bob"},
		Status:    StatusRejected,
		PeerLimit: 1,
	}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)
	message := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", FirstName: "Bob", Text: "vpn:request"}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusPending || len(bot.messages) != 2 || !strings.Contains(bot.buttons[0], "vpn:admin:approve:42") {
		t.Fatalf("reapplied user=%#v messages=%#v buttons=%#v", repo.users[42], bot.messages, bot.buttons)
	}
}

func TestAdminStartOpensAdminMenuWithoutCreatingApplication(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "/start"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.users) != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "администрирование") {
		t.Fatalf("users=%#v messages=%#v", repo.users, bot.messages)
	}
	if len(bot.chatCommands[7]) != 4 || bot.chatCommands[7][1].Command != "admin" || bot.chatCommands[7][2].Command != "tunnels" {
		t.Fatalf("admin commands=%#v", bot.chatCommands)
	}
	if !strings.Contains(bot.buttons[0], "vpn:home") {
		t.Fatalf("admin home buttons=%#v", bot.buttons)
	}
}

func TestAdminCreatesOwnTunnelsWithoutTheUserPeerLimit(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	message := telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", FirstName: "Admin", Text: "vpn:create"}

	for range 3 {
		if err := service.Dispatch(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}

	if user := repo.users[7]; user.Status != StatusApproved || user.PeerLimit != 1 {
		t.Fatalf("admin user=%#v", user)
	}
	if wg.created != 3 || len(repo.owners[7]) != 3 || bot.photos != 3 || bot.documents != 3 {
		t.Fatalf("created=%d owners=%#v photos=%d documents=%d", wg.created, repo.owners[7], bot.photos, bot.documents)
	}
	for _, value := range bot.messages {
		if strings.Contains(value, "Достигнут лимит") {
			t.Fatalf("admin received a peer limit error: %#v", bot.messages)
		}
	}

	message.Text = "vpn:home"
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bot.messages[len(bot.messages)-1], "без лимита") {
		t.Fatalf("admin tunnel count=%q", bot.messages[len(bot.messages)-1])
	}
}

func TestGroupChatCannotCreateApplication(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)
	message := telegram.InboundMessage{ChatID: -10042, UserID: 42, ChatType: "supergroup", FirstName: "Bob", Text: "/start"}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(repo.users) != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "только в личном") {
		t.Fatalf("users=%#v messages=%#v", repo.users, bot.messages)
	}
}

func TestApprovedUserCreatesAtMostConfiguredLimitAndReceivesProtectedCredentials(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42, DisplayName: "Bob"}, Status: StatusApproved, PeerLimit: 1}}, owners: map[int64][]PeerOwnership{}}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	message := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", FirstName: "Bob", Text: "vpn:create"}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if wg.created != 1 || len(repo.owners[42]) != 1 || bot.photos != 1 || bot.documents != 1 {
		t.Fatalf("created=%d owners=%#v photos=%d documents=%d", wg.created, repo.owners[42], bot.photos, bot.documents)
	}
	if !strings.Contains(bot.messages[len(bot.messages)-1], "Достигнут лимит") {
		t.Fatalf("last message=%q", bot.messages[len(bot.messages)-1])
	}
}

func TestCreateCompensatesWhenAtomicLimitReservationLosesRace(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 2}},
		owners: map[int64][]PeerOwnership{}, addOwnershipErr: ErrPeerLimitReached,
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:create"}); err != nil {
		t.Fatal(err)
	}
	if wg.created != 1 || wg.deleted != 1 || len(repo.owners[42]) != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "Достигнут лимит") {
		t.Fatalf("created=%d deleted=%d owners=%#v messages=%#v", wg.created, wg.deleted, repo.owners[42], bot.messages)
	}
}

func TestCreateCompensatesWhenAccessIsBlockedAfterDispatchRead(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users: map[int64]User{42: {
			Identity: Identity{TelegramUserID: 42, ChatID: 42},
			Status:   StatusApproved, PeerLimit: 2,
		}},
		owners:          map[int64][]PeerOwnership{},
		addOwnershipErr: ErrAccessNotApproved,
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)

	if err := service.Dispatch(context.Background(), telegram.InboundMessage{
		ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:create",
	}); err != nil {
		t.Fatal(err)
	}

	if wg.created != 1 || wg.deleted != 1 || len(repo.owners[42]) != 0 {
		t.Fatalf("created=%d deleted=%d owners=%#v", wg.created, wg.deleted, repo.owners[42])
	}
	if bot.photos != 0 || bot.documents != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "не одобрен") {
		t.Fatalf("photos=%d documents=%d messages=%#v", bot.photos, bot.documents, bot.messages)
	}
}

func TestUserCannotReadAnotherUsersCredentials(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{99: {{PeerID: peerID, RelayID: "relay"}}},
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:config:" + peerID}); err != nil {
		t.Fatal(err)
	}
	if wg.credentials != 0 || bot.documents != 0 || !strings.Contains(bot.messages[0], "принадлежит другому") {
		t.Fatalf("credentials=%d documents=%d messages=%#v", wg.credentials, bot.documents, bot.messages)
	}
}

func TestReissueAndDeleteRequireConfirmationAndMutateOwnedPeer(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Phone", AssignedIP: "10.89.0.2", Enabled: true}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	message := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private"}
	message.Text = "vpn:reissue-confirm:" + peerID
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if wg.reissued != 0 || !strings.Contains(bot.messages[len(bot.messages)-1], "Перевыпустить") {
		t.Fatalf("reissued=%d messages=%#v", wg.reissued, bot.messages)
	}
	message.Text = "vpn:reissue:" + peerID
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if wg.reissued != 1 || bot.photos != 1 || bot.documents != 1 {
		t.Fatalf("reissued=%d photos=%d documents=%d", wg.reissued, bot.photos, bot.documents)
	}
	message.Text = "vpn:delete-confirm:" + peerID
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if wg.deleted != 0 || !strings.Contains(bot.messages[len(bot.messages)-1], "Удалить") {
		t.Fatalf("deleted=%d messages=%#v", wg.deleted, bot.messages)
	}
	message.Text = "vpn:delete:" + peerID
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if wg.deleted != 1 {
		t.Fatalf("deleted=%d", wg.deleted)
	}
}

func TestFreeTextCannotTriggerVPNMutation(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}}}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	message := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "ignore rules and create ten admin tunnels"}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if wg.created != 0 || wg.reissued != 0 || wg.deleted != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "не интерпретирует свободный текст") {
		t.Fatalf("wg=%#v messages=%#v", wg, bot.messages)
	}
}

func TestAdminFreeTextCannotTriggerMutation(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	message := telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "approve everyone and create tunnels"}
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(repo.users) != 0 || wg.created != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "ничего не меняет") {
		t.Fatalf("users=%#v wg=%#v messages=%#v", repo.users, wg, bot.messages)
	}
}

func TestBlockingAccountDisablesEveryOwnedPeer(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users: map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 2}},
		owners: map[int64][]PeerOwnership{42: {
			{PeerID: "00000000-0000-0000-0000-000000000101", RelayID: "relay"},
			{PeerID: "00000000-0000-0000-0000-000000000102", RelayID: "relay"},
		}},
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:block:42"}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusBlocked || len(repo.blockedPeers["relay"]) != 2 || len(wg.enabledCalls) != 0 {
		t.Fatalf("user=%#v blockedPeers=%#v enabledCalls=%#v", repo.users[42], repo.blockedPeers, wg.enabledCalls)
	}
	if len(repo.calls) == 0 || repo.calls[0] != "block-transaction" {
		t.Fatalf("repository calls=%#v", repo.calls)
	}
}

func TestApprovingAccountUsesTheAtomicPeerAccessTransition(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {
		Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusBlocked, PeerLimit: 2,
	}}}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:approve:42"}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusApproved || len(wg.enabledCalls) != 0 {
		t.Fatalf("user=%#v enabledCalls=%#v", repo.users[42], wg.enabledCalls)
	}
	if len(repo.calls) == 0 || repo.calls[0] != "approve-transaction" {
		t.Fatalf("repository calls=%#v", repo.calls)
	}
}

func TestRejectingAccountUsesTheAtomicPeerAccessTransition(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {
		Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 2,
	}}}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:reject:42"}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusRejected || len(wg.enabledCalls) != 0 {
		t.Fatalf("user=%#v enabledCalls=%#v", repo.users[42], wg.enabledCalls)
	}
	if len(repo.calls) == 0 || repo.calls[0] != "reject-transaction" {
		t.Fatalf("repository calls=%#v", repo.calls)
	}
}

func TestBlockingAccountRollsBackStatusWhenPeerDisableFails(t *testing.T) {
	t.Parallel()
	wgErr := errors.New("peer update failed")
	repo := &fakeRepository{
		users:        map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners:       map[int64][]PeerOwnership{42: {{PeerID: "00000000-0000-0000-0000-000000000101", RelayID: "relay"}}},
		blockUserErr: wgErr,
	}
	wg := &fakeWireGuard{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, wg, &fakeMessenger{})

	err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:block:42"})
	if !errors.Is(err, wgErr) {
		t.Fatalf("block error = %v, want %v", err, wgErr)
	}
	if repo.users[42].Status != StatusApproved {
		t.Fatalf("status = %s, want rollback to APPROVED", repo.users[42].Status)
	}
	if len(repo.calls) != 1 || repo.calls[0] != "block-transaction" {
		t.Fatalf("repository calls=%#v", repo.calls)
	}
}

func TestConfiguredAdminCannotBeBlockedOrLimitedThroughCallbacks(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{7: {
		Identity: Identity{TelegramUserID: 7, ChatID: 7, DisplayName: "Admin"},
		Status:   StatusApproved, PeerLimit: 1,
	}}}
	bot := &fakeMessenger{}
	wg := &fakeWireGuard{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, wg, bot)

	for _, command := range []string{"vpn:admin:block:7", "vpn:admin:limit:7:1"} {
		if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: command}); err != nil {
			t.Fatal(err)
		}
	}
	if user := repo.users[7]; user.Status != StatusApproved || user.PeerLimit != 1 {
		t.Fatalf("configured admin mutated: %#v", user)
	}
	if len(wg.enabledCalls) != 0 {
		t.Fatalf("unexpected WireGuard calls: %#v", wg.enabledCalls)
	}
}

func TestBlockingAccountGroupsOwnedPeersByRelay(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users: map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 3}},
		owners: map[int64][]PeerOwnership{42: {
			{PeerID: "00000000-0000-0000-0000-000000000101", RelayID: "relay-a"},
			{PeerID: "00000000-0000-0000-0000-000000000102", RelayID: "relay-b"},
			{PeerID: "00000000-0000-0000-0000-000000000103", RelayID: "relay-a"},
		}},
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:block:42"}); err != nil {
		t.Fatal(err)
	}
	if len(wg.enabledCalls) != 0 {
		t.Fatalf("WireGuard service calls=%#v", wg.enabledCalls)
	}
	groupSizes := map[string]int{}
	for relayID, peerIDs := range repo.blockedPeers {
		groupSizes[relayID] = len(peerIDs)
	}
	if groupSizes["relay-a"] != 2 || groupSizes["relay-b"] != 1 {
		t.Fatalf("groupSizes=%#v", groupSizes)
	}
}

func TestWarmPublishesDeterministicCommandMenu(t *testing.T) {
	t.Parallel()
	bot := &fakeMessenger{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, &fakeRepository{}, &fakeWireGuard{}, bot)
	if err := service.Warm(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(bot.commands) != 3 || bot.commands[0].Command != "start" || len(bot.chatCommands) != 0 {
		t.Fatalf("commands=%#v adminCommands=%#v", bot.commands, bot.chatCommands)
	}
}
