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
	users            map[int64]User
	owners           map[int64][]PeerOwnership
	events           []string
	eventIndexes     map[string]int
	calls            []string
	addOwnershipErr  error
	blockUserErr     error
	blockedPeers     map[string][]string
	beginEventErr    error
	completeEventErr error
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
		user = User{Identity: identity, Status: StatusPending, AccessRevision: 1, PeerLimit: 1}
	} else {
		user.Identity = identity
		if user.Status == StatusRejected {
			user.Status = StatusPending
			user.AccessRevision++
		}
	}
	f.users[identity.TelegramUserID] = user
	if notify {
		f.events = append(f.events, "ACCESS_REQUESTED")
	}
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
func (f *fakeRepository) SetStatusIf(_ context.Context, id, adminID int64, expected Status, expectedRevision int64, status Status) (User, error) {
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	if user.Status != expected || user.AccessRevision != expectedRevision {
		return User{}, ErrStaleDecision
	}
	user.Status = status
	user.AccessRevision++
	if status == StatusBlocked {
		f.calls = append(f.calls, "block-transaction")
		if f.blockUserErr != nil {
			return User{}, f.blockUserErr
		}
		f.blockedPeers = make(map[string][]string)
		for _, owner := range f.owners[id] {
			f.blockedPeers[owner.RelayID] = append(f.blockedPeers[owner.RelayID], owner.PeerID)
		}
	} else if status == StatusApproved {
		f.calls = append(f.calls, "approve-transaction")
	} else if status == StatusRejected {
		f.calls = append(f.calls, "reject-transaction")
	}
	user.ApprovedBy = &adminID
	f.users[id] = user
	f.events = append(f.events, "ACCESS_"+string(status))
	return user, nil
}
func (f *fakeRepository) ApproveUser(ctx context.Context, id, adminID int64) (User, error) {
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	return f.setStatusDirect(ctx, id, adminID, user.Status, user.AccessRevision, StatusApproved)
}
func (f *fakeRepository) RejectUser(ctx context.Context, id, adminID int64) (User, error) {
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	return f.setStatusDirect(ctx, id, adminID, user.Status, user.AccessRevision, StatusRejected)
}
func (f *fakeRepository) BlockUser(ctx context.Context, id, adminID int64) (User, error) {
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	return f.setStatusDirect(ctx, id, adminID, user.Status, user.AccessRevision, StatusBlocked)
}
func (f *fakeRepository) setStatusDirect(_ context.Context, id, adminID int64, expected Status, revision int64, status Status) (User, error) {
	return f.SetStatusIf(context.Background(), id, adminID, expected, revision, status)
}
func (f *fakeRepository) SetPeerLimit(_ context.Context, id, adminID int64, limit int) (User, error) {
	user, ok := f.users[id]
	if !ok {
		return User{}, pgx.ErrNoRows
	}
	user.PeerLimit = limit
	user.AccessRevision++
	f.users[id] = user
	f.events = append(f.events, "PEER_LIMIT_CHANGED")
	return user, nil
}
func (f *fakeRepository) SetPeerLimitIf(_ context.Context, id, _ int64, expectedRevision int64, limit int) (User, error) {
	user := f.users[id]
	if user.AccessRevision != expectedRevision {
		return User{}, ErrStaleDecision
	}
	user.PeerLimit = limit
	user.AccessRevision++
	f.users[id] = user
	f.events = append(f.events, "PEER_LIMIT_CHANGED")
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
	f.events = append(f.events, "TUNNEL_CREATED")
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
func (f *fakeRepository) BeginApprovedDelivery(ctx context.Context, id int64, peerID, _ string) (PeerOwnership, string, error) {
	if f.beginEventErr != nil {
		return PeerOwnership{}, "", f.beginEventErr
	}
	if f.users[id].Status != StatusApproved {
		return PeerOwnership{}, "", ErrAccessNotApproved
	}
	owner, err := f.Ownership(ctx, id, peerID)
	if err != nil {
		return PeerOwnership{}, "", err
	}
	if f.eventIndexes == nil {
		f.eventIndexes = map[string]int{}
	}
	eventID := fmt.Sprintf("event-%d", len(f.events)+1)
	f.eventIndexes[eventID] = len(f.events)
	f.events = append(f.events, "CONFIG_DELIVERY_ATTEMPTED")
	return owner, eventID, nil
}
func (f *fakeRepository) CompleteEvent(_ context.Context, eventID, action string, _ map[string]any) error {
	if f.completeEventErr != nil {
		return f.completeEventErr
	}
	index, ok := f.eventIndexes[eventID]
	if !ok {
		return errors.New("fake audit event not found")
	}
	f.events[index] = action
	return nil
}

type fakeWireGuard struct {
	created       int
	createdNames  []string
	createdStates []bool
	renamed       []string
	renameUsers   []int64
	credentials   int
	reissued      int
	deleted       int
	deleteErr     error
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
	f.createdNames = append(f.createdNames, request.Name)
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	f.createdStates = append(f.createdStates, enabled)
	peer := wireguard.Peer{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", 100+f.created), Name: request.Name, AssignedIP: fmt.Sprintf("10.89.0.%d", 1+f.created), Enabled: enabled, CreatedAt: time.Now()}
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
func (f *fakeWireGuard) ReissuePeerCredentialsForVPNBot(ctx context.Context, relayID, peerID string, _ int64) (wireguard.PeerCredentials, error) {
	return f.ReissuePeerCredentials(ctx, relayID, peerID)
}
func (f *fakeWireGuard) RenamePeerForVPNBot(_ context.Context, _ string, peerID string, telegramUserID int64, name string) (wireguard.Peer, error) {
	f.renamed = append(f.renamed, name)
	f.renameUsers = append(f.renameUsers, telegramUserID)
	for index := range f.peers {
		if f.peers[index].ID == peerID {
			f.peers[index].Name = name
			return f.peers[index], nil
		}
	}
	return wireguard.Peer{}, errors.New("peer not found")
}
func (f *fakeWireGuard) DeletePeer(context.Context, string, string) error {
	f.deleted++
	return f.deleteErr
}
func (f *fakeWireGuard) DeletePeerForVPNBot(ctx context.Context, relayID, peerID string, _ int64) error {
	return f.DeletePeer(ctx, relayID, peerID)
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
	edits        []editedMessage
	photos       int
	documents    int
	photoErr     error
	documentErr  error
	commands     []telegram.BotCommand
	chatCommands map[int64][]telegram.BotCommand
}

type editedMessage struct {
	chatID    int64
	messageID int
	text      string
	buttons   string
}

func (f *fakeMessenger) SendHTMLMessage(_ context.Context, _ int64, text, buttons string) (int, error) {
	f.messages = append(f.messages, text)
	f.buttons = append(f.buttons, buttons)
	return len(f.messages), nil
}
func (f *fakeMessenger) EditHTMLMessageWithButtons(_ context.Context, chatID int64, messageID int, text, buttons string) error {
	f.edits = append(f.edits, editedMessage{chatID: chatID, messageID: messageID, text: text, buttons: buttons})
	return nil
}
func (f *fakeMessenger) SendProtectedPhoto(context.Context, int64, []byte, string) error {
	f.photos++
	return f.photoErr
}
func (f *fakeMessenger) SendProtectedDocument(context.Context, int64, []byte, string, string, string) error {
	f.documents++
	return f.documentErr
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
	if len(wg.createdStates) != 1 || wg.createdStates[0] {
		t.Fatalf("created states=%#v, bot peers must stay disabled until ownership is committed", wg.createdStates)
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

func TestCreateSurfacesCleanupFailureAfterOwnershipRejection(t *testing.T) {
	t.Parallel()
	cleanupErr := errors.New("cleanup unavailable")
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 2}},
		owners: map[int64][]PeerOwnership{}, addOwnershipErr: ErrPeerLimitReached,
	}
	wg := &fakeWireGuard{deleteErr: cleanupErr}
	service := NewService(Config{RelayID: "relay"}, repo, wg, &fakeMessenger{})
	err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:create"})
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("Dispatch() error = %v, want cleanup failure", err)
	}
	if wg.created != 1 || wg.deleted != 1 || len(repo.owners[42]) != 0 {
		t.Fatalf("created=%d deleted=%d owners=%#v", wg.created, wg.deleted, repo.owners[42])
	}
}

func TestTunnelNamesStayUniqueAfterDeletingANonLastTunnel(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42, DisplayName: "Bob"}, Status: StatusApproved, PeerLimit: 2}},
		owners: map[int64][]PeerOwnership{},
	}
	wg := &fakeWireGuard{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, &fakeMessenger{})
	suffixes := []string{"first", "second", "third"}
	service.newTunnelSuffix = func() (string, error) {
		value := suffixes[0]
		suffixes = suffixes[1:]
		return value, nil
	}
	message := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", FirstName: "Bob", Text: "vpn:create"}
	for range 2 {
		if err := service.Dispatch(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	// Model the database state after the first of two tunnels was deleted.
	repo.owners[42] = append([]PeerOwnership(nil), repo.owners[42][1:]...)
	if err := service.Dispatch(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(wg.createdNames) != 3 || wg.createdNames[1] == wg.createdNames[2] {
		t.Fatalf("created tunnel names = %#v", wg.createdNames)
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

func TestCredentialDeliveryRechecksAccessAfterApprovedSnapshot(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	staleApproved := User{Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: staleApproved.Identity, Status: StatusBlocked, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Phone"}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	if err := service.sendConfig(context.Background(), staleApproved, peerID); err != nil {
		t.Fatal(err)
	}
	if wg.credentials != 0 || bot.documents != 0 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "отозван") {
		t.Fatalf("credentials=%d documents=%d messages=%#v", wg.credentials, bot.documents, bot.messages)
	}
}

func TestFailedCredentialSendDoesNotClaimDeliveryInAudit(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Phone"}}}
	bot := &fakeMessenger{documentErr: errors.New("telegram unavailable")}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:config:" + peerID})
	if err != nil {
		t.Fatalf("Dispatch() error=%v after ambiguous Telegram result", err)
	}
	for _, event := range repo.events {
		if event == "CONFIG_DELIVERED" {
			t.Fatalf("events = %#v, failed delivery must not be recorded as successful", repo.events)
		}
	}
	if len(repo.events) != 1 || repo.events[0] != "CONFIG_DELIVERY_UNKNOWN" || len(bot.messages) == 0 || !strings.Contains(bot.messages[len(bot.messages)-1], "не подтвердил") {
		t.Fatalf("events=%#v messages=%#v", repo.events, bot.messages)
	}
}

func TestCredentialAuditIntentFailurePreventsDelivery(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:         map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners:        map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
		beginEventErr: errors.New("audit unavailable"),
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Phone"}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:config:" + peerID})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want durable audit intent failure")
	}
	if bot.documents != 0 || len(repo.events) != 0 {
		t.Fatalf("documents=%d events=%#v, credential escaped without durable intent", bot.documents, repo.events)
	}
}

func TestCombinedCredentialAuditFailureReportsTemporaryProblem(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users:         map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners:        map[int64][]PeerOwnership{},
		beginEventErr: errors.New("database unavailable"),
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:create"}); err != nil {
		t.Fatalf("Dispatch() error=%v after committed tunnel creation", err)
	}
	if bot.photos != 0 || bot.documents != 0 {
		t.Fatalf("photos=%d documents=%d, credentials escaped without a durable audit intent", bot.photos, bot.documents)
	}
	lastMessage := bot.messages[len(bot.messages)-1]
	if !strings.Contains(lastMessage, "временная ошибка") || strings.Contains(lastMessage, "доступ изменился") || strings.Contains(lastMessage, "отозван") {
		t.Fatalf("misleading credential failure message=%q", lastMessage)
	}
}

func TestCredentialCompletionFailureKeepsIntentWithoutRepeatingDelivery(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:            map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners:           map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
		completeEventErr: errors.New("audit completion unavailable"),
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Phone"}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:config:" + peerID}); err != nil {
		t.Fatalf("Dispatch() error=%v after successful Telegram delivery", err)
	}
	if bot.documents != 1 || len(repo.events) != 1 || repo.events[0] != "CONFIG_DELIVERY_ATTEMPTED" {
		t.Fatalf("documents=%d events=%#v", bot.documents, repo.events)
	}
}

func TestCombinedCredentialDeliveryTreatsQRAsSuccessfulWhenDocumentFails(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{},
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{documentErr: errors.New("document unavailable")}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:create"}); err != nil {
		t.Fatalf("Dispatch() error=%v after QR was delivered", err)
	}
	if bot.photos != 1 || bot.documents != 1 || len(repo.events) != 3 || repo.events[0] != "TUNNEL_CREATED" || repo.events[1] != "CONFIG_DELIVERED" || repo.events[2] != "CONFIG_DELIVERY_UNKNOWN" {
		t.Fatalf("photos=%d documents=%d events=%#v", bot.photos, bot.documents, repo.events)
	}
	if !strings.Contains(bot.messages[len(bot.messages)-1], "QR уже отправлен") {
		t.Fatalf("messages=%#v", bot.messages)
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
	message.Callback = true
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

func TestUserRenamesOwnedTunnelAfterExplicitButtonPrompt(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Old name", AssignedIP: "10.89.0.2", Enabled: true}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)

	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, MessageID: 101, ChatType: "private", Text: "vpn:peer:" + peerID, Callback: true}); err != nil {
		t.Fatal(err)
	}
	if len(bot.edits) != 1 || !strings.Contains(bot.edits[0].buttons, "vpn:rename:"+peerID) {
		t.Fatalf("peer card edits=%#v messages=%#v buttons=%#v", bot.edits, bot.messages, bot.buttons)
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, MessageID: 101, ChatType: "private", Text: "vpn:rename:" + peerID, Callback: true}); err != nil {
		t.Fatal(err)
	}
	if len(wg.renamed) != 0 || len(bot.edits) != 2 || !strings.Contains(bot.edits[1].text, "новое название") {
		t.Fatalf("rename prompt edits=%#v renamed=%#v", bot.edits, wg.renamed)
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, MessageID: 202, ChatType: "private", Text: "My laptop"}); err != nil {
		t.Fatal(err)
	}
	if len(wg.renamed) != 1 || wg.renamed[0] != "My laptop" || len(wg.renameUsers) != 1 || wg.renameUsers[0] != 42 {
		t.Fatalf("renamed=%#v users=%#v", wg.renamed, wg.renameUsers)
	}
	if len(bot.edits) != 3 || bot.edits[2].messageID != 101 || !strings.Contains(bot.edits[2].text, "<b>My laptop</b>") {
		t.Fatalf("renamed card edits=%#v", bot.edits)
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: "Another name"}); err != nil {
		t.Fatal(err)
	}
	if len(wg.renamed) != 1 {
		t.Fatalf("free text repeated rename: %#v", wg.renamed)
	}
}

func TestAdminRenamesOwnTunnelThroughSamePrompt(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000102"
	repo := &fakeRepository{owners: map[int64][]PeerOwnership{7: {{PeerID: peerID, RelayID: "relay"}}}}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Admin tunnel", AssignedIP: "10.89.0.3", Enabled: true}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)

	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, MessageID: 303, ChatType: "private", Text: "vpn:rename:" + peerID, Callback: true}); err != nil {
		t.Fatal(err)
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "Unlimited admin tunnel"}); err != nil {
		t.Fatal(err)
	}
	if len(wg.renamed) != 1 || wg.renamed[0] != "Unlimited admin tunnel" || len(bot.edits) != 2 || !strings.Contains(bot.edits[1].text, "Unlimited admin tunnel") {
		t.Fatalf("renamed=%#v edits=%#v", wg.renamed, bot.edits)
	}
}

func TestDeleteActionRequiresCallbackOrigin(t *testing.T) {
	t.Parallel()
	peerID := "00000000-0000-0000-0000-000000000101"
	repo := &fakeRepository{
		users:  map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, PeerLimit: 1}},
		owners: map[int64][]PeerOwnership{42: {{PeerID: peerID, RelayID: "relay"}}},
	}
	wg := &fakeWireGuard{peers: []wireguard.Peer{{ID: peerID, Name: "Phone"}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay"}, repo, wg, bot)
	err := service.Dispatch(context.Background(), telegram.InboundMessage{
		ChatID: 42, UserID: 42, ChatType: "private", Text: "vpn:delete:" + peerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if wg.deleted != 0 {
		t.Fatalf("deleted=%d, ordinary text bypassed the confirmation button", wg.deleted)
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

func TestChangingAdminUserSettingsUpdatesExistingCard(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {
		Identity: Identity{TelegramUserID: 42, ChatID: 42, DisplayName: "Bob"},
		Status:   StatusApproved, AccessRevision: 1, PeerLimit: 1,
	}}}
	bot := &fakeMessenger{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)

	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:user:42"}); err != nil {
		t.Fatal(err)
	}
	if len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "0 из 1") {
		t.Fatalf("initial card=%#v", bot.messages)
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, MessageID: 1, ChatType: "private", Text: adminLimitCallback(42, 5, 1), Callback: true}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].PeerLimit != 5 {
		t.Fatalf("peer limit=%d", repo.users[42].PeerLimit)
	}
	if len(bot.messages) != 1 {
		t.Fatalf("settings change sent a duplicate card: %#v", bot.messages)
	}
	if len(bot.edits) != 1 || bot.edits[0].chatID != 7 || bot.edits[0].messageID != 1 || !strings.Contains(bot.edits[0].text, "0 из 5") || !strings.Contains(bot.edits[0].buttons, adminLimitCallback(42, 1, 2)) {
		t.Fatalf("updated card=%#v", bot.edits)
	}
}

func TestBlockingAccountUsesAtomicRepositoryTransition(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users: map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, AccessRevision: 1, PeerLimit: 2}},
		owners: map[int64][]PeerOwnership{42: {
			{PeerID: "00000000-0000-0000-0000-000000000101", RelayID: "relay"},
			{PeerID: "00000000-0000-0000-0000-000000000102", RelayID: "relay"},
		}},
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:block:42:A:1", Callback: true}); err != nil {
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
		Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusBlocked, AccessRevision: 1, PeerLimit: 2,
	}}}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: adminDecisionCallback("approve", 42, StatusBlocked, 1), Callback: true}); err != nil {
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
		Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, AccessRevision: 1, PeerLimit: 2,
	}}}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: adminDecisionCallback("reject", 42, StatusApproved, 1), Callback: true}); err != nil {
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
		users:        map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, AccessRevision: 1, PeerLimit: 1}},
		owners:       map[int64][]PeerOwnership{42: {{PeerID: "00000000-0000-0000-0000-000000000101", RelayID: "relay"}}},
		blockUserErr: wgErr,
	}
	wg := &fakeWireGuard{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, wg, &fakeMessenger{})

	err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: adminDecisionCallback("block", 42, StatusApproved, 1), Callback: true})
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
		Status:   StatusApproved, AccessRevision: 1, PeerLimit: 1,
	}}}
	bot := &fakeMessenger{}
	wg := &fakeWireGuard{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, wg, bot)

	for _, command := range []string{adminDecisionCallback("block", 7, StatusApproved, 1), adminLimitCallback(7, 1, 1)} {
		if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: command, Callback: true}); err != nil {
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

func TestBlockingAccountNeverPerformsPartialWireGuardSideEffects(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users: map[int64]User{42: {Identity: Identity{TelegramUserID: 42, ChatID: 42}, Status: StatusApproved, AccessRevision: 1, PeerLimit: 3}},
		owners: map[int64][]PeerOwnership{42: {
			{PeerID: "00000000-0000-0000-0000-000000000101", RelayID: "relay-a"},
			{PeerID: "00000000-0000-0000-0000-000000000102", RelayID: "relay-b"},
			{PeerID: "00000000-0000-0000-0000-000000000103", RelayID: "relay-a"},
		}},
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: "vpn:admin:block:42:A:1", Callback: true}); err != nil {
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
	if repo.users[42].Status != StatusBlocked || len(wg.enabledCalls) != 0 {
		t.Fatalf("relays=%#v peerGroups=%#v enabled=%#v", wg.enabledRelays, wg.enabledPeers, wg.enabledCalls)
	}
}

func TestStalePendingDecisionCannotOverwriteNewerApprovedStatus(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		users: map[int64]User{42: {
			Identity: Identity{TelegramUserID: 42, ChatID: 42, DisplayName: "Bob"}, Status: StatusApproved, AccessRevision: 2, PeerLimit: 1,
		}},
	}
	wg := &fakeWireGuard{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, wg, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, MessageID: 44, ChatType: "private", Text: "vpn:admin:reject:42:P:1", Callback: true}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusApproved {
		t.Fatalf("status=%s, stale pending decision overwrote a newer approval", repo.users[42].Status)
	}
	if len(bot.messages) != 0 || len(bot.edits) != 1 || bot.edits[0].messageID != 44 || !strings.Contains(bot.edits[0].text, "одобрен") {
		t.Fatalf("stale decision messages=%#v edits=%#v", bot.messages, bot.edits)
	}
}

func TestOldDecisionCannotMutateAReopenedApplication(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)
	userMessage := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", FirstName: "Bob", Text: "/start"}
	if err := service.Dispatch(context.Background(), userMessage); err != nil {
		t.Fatal(err)
	}
	staleApprove := adminDecisionCallback("approve", 42, StatusPending, 1)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: adminDecisionCallback("reject", 42, StatusPending, 1), Callback: true}); err != nil {
		t.Fatal(err)
	}
	userMessage.Text = "vpn:request"
	if err := service.Dispatch(context.Background(), userMessage); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusPending || repo.users[42].AccessRevision != 3 {
		t.Fatalf("reopened application=%#v", repo.users[42])
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, ChatType: "private", Text: staleApprove, Callback: true}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].Status != StatusPending || repo.users[42].AccessRevision != 3 {
		t.Fatalf("stale callback changed reopened application=%#v", repo.users[42])
	}
}

func TestStaleAdminLimitCallbackCannotOverwriteNewerLimit(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{users: map[int64]User{42: {
		Identity: Identity{TelegramUserID: 42, ChatID: 42, DisplayName: "Bob"}, Status: StatusApproved, AccessRevision: 1, PeerLimit: 1,
	}}}
	bot := &fakeMessenger{}
	service := NewService(Config{RelayID: "relay", AdminUserIDs: []int64{7}}, repo, &fakeWireGuard{}, bot)
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, MessageID: 55, ChatType: "private", Text: adminLimitCallback(42, 5, 1), Callback: true}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].PeerLimit != 5 || repo.users[42].AccessRevision != 2 {
		t.Fatalf("fresh limit callback user=%#v", repo.users[42])
	}
	if err := service.Dispatch(context.Background(), telegram.InboundMessage{ChatID: 7, UserID: 7, MessageID: 55, ChatType: "private", Text: adminLimitCallback(42, 1, 1), Callback: true}); err != nil {
		t.Fatal(err)
	}
	if repo.users[42].PeerLimit != 5 || repo.users[42].AccessRevision != 2 {
		t.Fatalf("stale limit callback changed user=%#v", repo.users[42])
	}
	if len(bot.messages) != 0 || len(bot.edits) != 2 || bot.edits[1].messageID != 55 || !strings.Contains(bot.edits[1].text, "0 из 5") {
		t.Fatalf("messages=%#v edits=%#v", bot.messages, bot.edits)
	}
}

func TestAdminDecisionCallbackRoundTripsWithinTelegramLimit(t *testing.T) {
	t.Parallel()
	const maxID int64 = 9223372036854775807
	callback := adminDecisionCallback("approve", maxID, StatusBlocked, maxID)
	if len(callback) > 64 {
		t.Fatalf("callback length=%d, Telegram limit is 64: %q", len(callback), callback)
	}
	userID, status, revision, ok := parseAdminDecision(strings.TrimPrefix(callback, "vpn:admin:approve:"))
	if !ok || userID != maxID || status != StatusBlocked || revision != maxID {
		t.Fatalf("callback=%q parsed=%d/%s/%d ok=%v", callback, userID, status, revision, ok)
	}
	limitCallback := adminLimitCallback(maxID, 10, maxID)
	if len(limitCallback) > 64 {
		t.Fatalf("limit callback length=%d, Telegram limit is 64: %q", len(limitCallback), limitCallback)
	}
	limitUserID, limit, limitRevision, ok := parseAdminLimit(strings.TrimPrefix(limitCallback, "vpn:admin:limit:"))
	if !ok || limitUserID != maxID || limit != 10 || limitRevision != maxID {
		t.Fatalf("limit callback=%q parsed=%d/%d/%d ok=%v", limitCallback, limitUserID, limit, limitRevision, ok)
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
