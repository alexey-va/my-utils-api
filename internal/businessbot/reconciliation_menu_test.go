package businessbot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/alexey-va/my-utils-api/internal/telegram"
)

type reconciliationMenuService struct {
	*business.Service
	workspace business.Workspace
	reads     [][3]string
	generated [][4]string
}

func (s *reconciliationMenuService) Workspace(_ context.Context, owner, period, party string) (business.Workspace, error) {
	s.reads = append(s.reads, [3]string{owner, period, party})
	return s.workspace, nil
}

func (s *reconciliationMenuService) ReconciliationPDF(_ context.Context, owner, party, from, to string) (business.File, error) {
	s.generated = append(s.generated, [4]string{owner, party, from, to})
	return business.File{
		Document: business.Document{Filename: "reconciliation.pdf", MIMEType: "application/pdf"},
		Data:     []byte("%PDF-synthetic"),
	}, nil
}

type reconciliationMenuMessage struct {
	id      int
	text    string
	buttons [][]telegram.InlineButton
}

type reconciliationMenuSender struct {
	t        *testing.T
	messages []reconciliationMenuMessage
	pdfs     int
}

func (s *reconciliationMenuSender) SendProtectedMessage(_ context.Context, chatID int64, text, buttons string) (int, error) {
	s.t.Helper()
	id := len(s.messages) + 1
	return id, s.record(chatID, id, text, buttons)
}

func (s *reconciliationMenuSender) EditHTMLMessageWithButtons(_ context.Context, chatID int64, messageID int, text, buttons string) error {
	s.t.Helper()
	return s.record(chatID, messageID, text, buttons)
}

func (s *reconciliationMenuSender) record(chatID int64, id int, text, buttons string) error {
	s.t.Helper()
	if chatID != 42 {
		s.t.Fatalf("unexpected chat %d", chatID)
	}
	keyboard, err := telegram.ParseButtons(buttons)
	if err != nil {
		s.t.Fatalf("invalid Telegram buttons %q: %v", buttons, err)
	}
	s.messages = append(s.messages, reconciliationMenuMessage{id, text, keyboard})
	return nil
}

func (s *reconciliationMenuSender) SendProtectedDocument(_ context.Context, chatID int64, data []byte, filename, mimeType, _ string) error {
	s.t.Helper()
	if chatID != 42 || string(data) != "%PDF-synthetic" || filename != "reconciliation.pdf" || mimeType != "application/pdf" {
		s.t.Fatalf("unexpected PDF delivery to %d, filename %q, mime %q", chatID, filename, mimeType)
	}
	s.pdfs++
	return nil
}

func (s *reconciliationMenuSender) button(label string) (string, int) {
	s.t.Helper()
	if len(s.messages) == 0 {
		s.t.Fatal("expected a Telegram message")
	}
	msg := s.messages[len(s.messages)-1]
	for _, row := range msg.buttons {
		for _, button := range row {
			if strings.Contains(button.Text, label) {
				return button.CallbackData, msg.id
			}
		}
	}
	s.t.Fatalf("button %q missing in %q with keyboard %#v", label, msg.text, msg.buttons)
	return "", 0
}

func dispatchReconciliationMenu(t *testing.T, bot *Telegram, text string, callback bool, messageID int) {
	t.Helper()
	msg := telegram.InboundMessage{ChatID: 42, UserID: 42, ChatType: "private", Text: text, Callback: callback, MessageID: messageID}
	if err := bot.Dispatch(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func pressReconciliationMenu(t *testing.T, bot *Telegram, sender *reconciliationMenuSender, label string) {
	t.Helper()
	data, id := sender.button(label)
	dispatchReconciliationMenu(t, bot, data, true, id)
}

func TestReconciliationButtonShowsWorkspacePartiesAndDeliversOnlyAfterConfirmation(t *testing.T) {
	service := &reconciliationMenuService{workspace: assistantFixture()}
	service.workspace.Counterparties[0].Name = "ООО «Альфа, К: Бизнес; услуги»"
	sender := &reconciliationMenuSender{t: t}
	// No Assistant is installed here: the entire button flow must work without an LLM.
	bot := NewTelegram("workspace-owner", 42, func(context.Context) bool { return true }, sender, service, nil)

	dispatchReconciliationMenu(t, bot, "reconciliation", true, 100)
	sender.button("Бета")
	pressReconciliationMenu(t, bot, sender, "Альфа")
	pressReconciliationMenu(t, bot, sender, "Прошлый месяц")
	if len(service.generated) != 0 || sender.pdfs != 0 {
		t.Fatal("PDF generated before confirmation")
	}
	confirm, id := sender.button("Сформировать PDF")
	dispatchReconciliationMenu(t, bot, confirm, true, id)
	dispatchReconciliationMenu(t, bot, confirm, true, id)

	today := time.Now().In(time.FixedZone("MSK", 3*3600))
	first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	want := [4]string{"workspace-owner", "alpha", first.AddDate(0, -1, 0).Format(time.DateOnly), first.AddDate(0, 0, -1).Format(time.DateOnly)}
	if len(service.generated) != 1 || service.generated[0] != want || sender.pdfs != 1 {
		t.Fatalf("generated %#v, sent %d PDF(s), wanted one generation %#v", service.generated, sender.pdfs, want)
	}
	if len(service.reads) == 0 {
		t.Fatal("no scoped lookup")
	}
	for _, read := range service.reads {
		if read[0] != "workspace-owner" {
			t.Fatalf("read for wrong owner: %#v", read)
		}
	}
}

func TestReconciliationMenuValidatesCustomRangeAndAllowsCancellation(t *testing.T) {
	service := &reconciliationMenuService{workspace: assistantFixture()}
	sender := &reconciliationMenuSender{t: t}
	bot := NewTelegram("workspace-owner", 42, func(context.Context) bool { return true }, sender, service, nil)

	dispatchReconciliationMenu(t, bot, "reconciliation", true, 100)
	pressReconciliationMenu(t, bot, sender, "Бета")
	pressReconciliationMenu(t, bot, sender, "Назад")
	pressReconciliationMenu(t, bot, sender, "Бета")
	pressReconciliationMenu(t, bot, sender, "Свой период")
	dispatchReconciliationMenu(t, bot, "2026-10-31 2026-09-01", false, 101)
	if len(service.generated) != 0 {
		t.Fatal("invalid range generated a PDF")
	}
	dispatchReconciliationMenu(t, bot, "2026-09-01 2026-10-31", false, 102)
	confirm, id := sender.button("Сформировать PDF")
	pressReconciliationMenu(t, bot, sender, "Отмена")
	dispatchReconciliationMenu(t, bot, confirm, true, id)
	if len(service.generated) != 0 || sender.pdfs != 0 {
		t.Fatal("cancelled confirmation generated a PDF")
	}
}

func TestReconciliationMenuPagesCounterpartiesAndRejectsOldButtons(t *testing.T) {
	service := &reconciliationMenuService{}
	for i := 1; i <= 9; i++ {
		service.workspace.Counterparties = append(service.workspace.Counterparties, business.CounterpartyView{
			Counterparty: business.Counterparty{ID: fmt.Sprintf("party-%d", i), Name: fmt.Sprintf("Контрагент %02d", i)},
		})
	}
	sender := &reconciliationMenuSender{t: t}
	bot := NewTelegram("workspace-owner", 42, func(context.Context) bool { return true }, sender, service, nil)
	dispatchReconciliationMenu(t, bot, "reconciliation", true, 100)
	old, pickerID := sender.button("Контрагент 01")
	pressReconciliationMenu(t, bot, sender, "Следующие")
	last, samePickerID := sender.button("Контрагент 09")
	if pickerID != samePickerID {
		t.Fatal("paging should edit the picker message")
	}
	dispatchReconciliationMenu(t, bot, old, true, pickerID)
	dispatchReconciliationMenu(t, bot, last, true, pickerID)
	pressReconciliationMenu(t, bot, sender, "Прошлый месяц")
	pressReconciliationMenu(t, bot, sender, "Сформировать PDF")
	if len(service.generated) != 1 || service.generated[0][1] != "party-9" {
		t.Fatalf("wanted a PDF only for the current page selection, got %#v", service.generated)
	}
}
