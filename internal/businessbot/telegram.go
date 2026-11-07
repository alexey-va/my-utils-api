package businessbot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/telegram"
	"github.com/google/uuid"
)

type telegramSender interface {
	SendProtectedMessage(context.Context, int64, string, string) (int, error)
	SendProtectedDocument(context.Context, int64, []byte, string, string, string) error
}

// Telegram is used by one serialized, private Telegram chat. Pending
// confirmations are intentionally lost on restart and expire after 15 minutes.
type Telegram struct {
	owner        string
	userID       int64
	ready        func(context.Context) bool
	bot          telegramSender
	service      *business.Service
	assistant    *Assistant
	history      []openrouter.Message
	pending      *Draft
	pendingID    string
	pendingUntil time.Time
}

func NewTelegram(owner string, userID int64, ready func(context.Context) bool, bot telegramSender, service *business.Service, assistant *Assistant) *Telegram {
	return &Telegram{owner: owner, userID: userID, ready: ready, bot: bot, service: service, assistant: assistant}
}

func menuKeyboard() string {
	return "Счёт:invoice,Акт услуг:act,Акт сверки:reconciliation;Договор:contract,Состояние расчётов:balances"
}

func (t *Telegram) Send(ctx context.Context, text string) error {
	if !t.ready(ctx) {
		return errors.New("business bot owner is unavailable")
	}
	return t.reply(ctx, text, "")
}

func (t *Telegram) reply(ctx context.Context, text, buttons string) error {
	_, err := t.bot.SendProtectedMessage(ctx, t.userID, text, buttons)
	if err != nil {
		return errors.New("business Telegram message failed")
	}
	return nil
}

func (t *Telegram) Dispatch(ctx context.Context, msg telegram.InboundMessage) error {
	if msg.UserID != t.userID || msg.ChatID != t.userID || msg.ChatType != "private" || !t.ready(ctx) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if msg.Callback {
		switch msg.Text {
		case "invoice":
			t.history = nil
			msg.Text = "Хочу выставить счёт. Уточни нужные данные."
		case "act":
			t.history = nil
			msg.Text = "Хочу сформировать акт оказанных услуг. Уточни нужные данные."
		case "reconciliation":
			t.history = nil
			msg.Text = "Хочу получить акт сверки взаимных расчётов. Уточни нужные данные."
		case "contract":
			t.history = nil
			msg.Text = "Хочу подготовить договор аренды. Уточни нужные данные."
		case "balances":
			msg.Text = "Покажи текущее состояние расчётов по контрагентам."
		default:
			return t.confirm(ctx, msg.Text)
		}
	}
	if msg.Text == "/start" || msg.Text == "/menu" {
		t.history, t.pending = nil, nil
		return t.reply(ctx, "Бизнес · документы и оплаты\nВыберите действие или напишите задачу сообщением.", menuKeyboard())
	}
	if msg.Text == "" {
		return t.reply(ctx, "Напишите задачу текстом.", menuKeyboard())
	}
	t.pending = nil
	result, err := t.assistant.Answer(ctx, t.owner, t.history, msg.Text)
	if err != nil {
		return t.reply(ctx, "Не удалось получить ответ. Попробуйте повторить запрос.", menuKeyboard())
	}
	t.history = append(t.history, openrouter.Message{Role: "user", Content: msg.Text}, openrouter.Message{Role: "assistant", Content: result.Reply})
	if len(t.history) > 12 {
		t.history = append([]openrouter.Message(nil), t.history[len(t.history)-12:]...)
	}
	buttons := menuKeyboard()
	if draft := result.Draft; draft != nil && draft.Kind != "contract" {
		t.pending, t.pendingID, t.pendingUntil = draft, uuid.NewString(), time.Now().Add(15*time.Minute)
		buttons = "Сформировать PDF:confirm:" + t.pendingID + ",Отмена:cancel:" + t.pendingID
	}
	return t.reply(ctx, result.Reply, buttons)
}

func (t *Telegram) confirm(ctx context.Context, action string) error {
	draft := t.pending
	if draft == nil || time.Now().After(t.pendingUntil) {
		return t.reply(ctx, "Запрос истёк. Выберите действие снова.", menuKeyboard())
	}
	if action != "confirm:"+t.pendingID && action != "cancel:"+t.pendingID {
		return t.reply(ctx, "Эта кнопка относится к другому запросу.", "")
	}
	t.pending = nil
	if strings.HasPrefix(action, "cancel:") {
		return t.reply(ctx, "Запрос отменён.", menuKeyboard())
	}
	var file business.File
	var err error
	switch draft.Kind {
	case "invoice":
		if draft.InvoiceRequest == nil {
			return t.reply(ctx, "В запросе не хватает данных для счёта.", menuKeyboard())
		}
		file, err = t.invoice(ctx, *draft.InvoiceRequest, draft.ExistingInvoiceID)
	case "act":
		file, err = t.service.ServiceActPDF(ctx, t.owner, draft.CounterpartyID, draft.TemplateID, draft.Period)
	case "reconciliation":
		file, err = t.service.ReconciliationPDF(ctx, t.owner, draft.CounterpartyID, draft.From, draft.To)
	default:
		return t.reply(ctx, "Выберите вид документа.", menuKeyboard())
	}
	if err != nil {
		return t.reply(ctx, "Не удалось подготовить документ. Проверьте данные в «Счетах и оплатах».", menuKeyboard())
	}
	if err := t.bot.SendProtectedDocument(ctx, t.userID, file.Data, file.Filename, file.MIMEType, ""); err != nil {
		return t.reply(ctx, "Документ подготовлен, но Telegram не принял файл. Повторите запрос.", menuKeyboard())
	}
	return nil
}

func (t *Telegram) invoice(ctx context.Context, request business.CreateInvoiceRequest, existingID string) (business.File, error) {
	if existingID != "" {
		workspace, err := t.service.Workspace(ctx, t.owner, request.Period, request.CounterpartyID)
		if err != nil {
			return business.File{}, err
		}
		for _, invoice := range workspace.Invoices {
			if invoice.IsCurrent && invoice.ID == existingID && invoice.TemplateID == request.TemplateID {
				return t.invoiceFile(ctx, invoice)
			}
		}
		return business.File{}, errors.New("current invoice changed before confirmation")
	}
	request.Provider = "local"
	invoice, err := t.service.CreateInvoice(ctx, t.owner, request)
	if err != nil {
		return business.File{}, err
	}
	return t.invoiceFile(ctx, invoice)
}

func (t *Telegram) invoiceFile(ctx context.Context, invoice business.InvoiceView) (business.File, error) {
	for _, document := range invoice.Documents {
		if document.Kind == "invoice" && document.MIMEType == "application/pdf" {
			return t.service.File(ctx, t.owner, document.ID)
		}
	}
	return business.File{}, errors.New("invoice PDF not found")
}
