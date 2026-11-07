package businessbot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const reconciliationPageSize = 8

// The snapshot binds short button indices to this owner's actual counterparties.
// Each screen has a new token, so old buttons cannot change a newer selection.
type reconciliationMenu struct {
	id        string
	until     time.Time
	messageID int
	parties   []catalogChoice
	page      int
	selected  int
	step      string
}

func (m *reconciliationMenu) screen(step string) string {
	m.id, m.until, m.step = uuid.NewString(), time.Now().Add(15*time.Minute), step
	return "recon:" + m.id + ":"
}

func (t *Telegram) startReconciliation(ctx context.Context) error {
	t.history, t.pending, t.reconciliation = nil, nil, nil
	workspace, err := t.service.Workspace(ctx, t.owner, "", "")
	if err != nil {
		return t.reply(ctx, "Не удалось загрузить контрагентов. Попробуйте ещё раз.", "Повторить:reconciliation;Меню:menu")
	}
	if len(workspace.Counterparties) == 0 {
		return t.reply(ctx, "В рабочей области пока нет контрагентов.", menuKeyboard())
	}
	menu := &reconciliationMenu{selected: -1}
	for _, party := range workspace.Counterparties {
		menu.parties = append(menu.parties, catalogChoice{ID: party.ID, Name: party.Name})
	}
	t.reconciliation = menu
	return t.reconciliationParties(ctx)
}

func (t *Telegram) reconciliationParties(ctx context.Context) error {
	menu := t.reconciliation
	prefix := menu.screen("parties")
	menu.selected, t.pending = -1, nil
	start := menu.page * reconciliationPageSize
	end := min(start+reconciliationPageSize, len(menu.parties))
	rows := make([]string, 0, reconciliationPageSize+2)
	for i := start; i < end; i++ {
		rows = append(rows, reconciliationButtonLabel(menu.parties[i].Name)+":"+prefix+"party:"+strconv.FormatInt(int64(i), 36))
	}
	var navigation []string
	if menu.page > 0 {
		navigation = append(navigation, "Предыдущие:"+prefix+"page:"+strconv.FormatInt(int64(menu.page-1), 36))
	}
	if end < len(menu.parties) {
		navigation = append(navigation, "Следующие:"+prefix+"page:"+strconv.FormatInt(int64(menu.page+1), 36))
	}
	text := "Акт сверки\nВыберите контрагента."
	if len(navigation) > 0 {
		text += fmt.Sprintf("\nСтраница %d из %d.", menu.page+1, (len(menu.parties)+reconciliationPageSize-1)/reconciliationPageSize)
		rows = append(rows, strings.Join(navigation, ","))
	}
	rows = append(rows, "Отмена:"+prefix+"cancel")
	return t.reconciliationMessage(ctx, menu, text, strings.Join(rows, ";"))
}

func (t *Telegram) reconciliationPeriods(ctx context.Context) error {
	menu := t.reconciliation
	prefix := menu.screen("periods")
	t.pending = nil
	text := "Акт сверки\n" + limitText(menu.parties[menu.selected].Name, 160) + "\n\nВыберите период. Текущий месяц и год — по сегодня."
	buttons := "Прошлый месяц:" + prefix + "period:previous;Текущий месяц:" + prefix + "period:current;С начала года:" + prefix + "period:year;Свой период:" + prefix + "period:custom;Назад:" + prefix + "back,Отмена:" + prefix + "cancel"
	return t.reconciliationMessage(ctx, menu, text, buttons)
}

func (t *Telegram) reconciliationCustomPeriod(ctx context.Context, problem string) error {
	menu := t.reconciliation
	prefix := menu.screen("range")
	t.pending = nil
	text := "Акт сверки\n" + limitText(menu.parties[menu.selected].Name, 160) + "\n\n"
	if problem != "" {
		text += problem + "\n\n"
	}
	from, to, _ := reconciliationPeriod("previous", time.Now())
	text += "Пришлите начальную и конечную даты одним сообщением в формате ДД.ММ.ГГГГ ДД.ММ.ГГГГ.\nПример периода — " + reconciliationDisplayDate(from) + " " + reconciliationDisplayDate(to) + ".\nОбе даты входят в период."
	return t.reconciliationMessage(ctx, menu, text, "Назад:"+prefix+"back,Отмена:"+prefix+"cancel")
}

func (t *Telegram) reconciliationCallback(ctx context.Context, callback string) error {
	menu := t.reconciliation
	parts := strings.Split(callback, ":")
	if menu == nil || time.Now().After(menu.until) || len(parts) < 3 || parts[1] != menu.id {
		return t.staleReconciliation(ctx)
	}
	switch {
	case len(parts) == 3 && parts[2] == "cancel":
		t.pending, t.reconciliation = nil, nil
		return t.reconciliationMessage(ctx, menu, "Запрос отменён.", menuKeyboard())
	case len(parts) == 3 && parts[2] == "back":
		switch menu.step {
		case "periods":
			return t.reconciliationParties(ctx)
		case "range", "confirm":
			return t.reconciliationPeriods(ctx)
		}
	case len(parts) == 4 && parts[2] == "page" && menu.step == "parties":
		page, err := strconv.ParseInt(parts[3], 36, 32)
		if err == nil && page >= 0 && page <= int64((len(menu.parties)-1)/reconciliationPageSize) {
			menu.page = int(page)
			return t.reconciliationParties(ctx)
		}
	case len(parts) == 4 && parts[2] == "party" && menu.step == "parties":
		index, err := strconv.ParseInt(parts[3], 36, 32)
		start := menu.page * reconciliationPageSize
		if err == nil && index >= int64(start) && index < int64(min(start+reconciliationPageSize, len(menu.parties))) {
			menu.selected = int(index)
			return t.reconciliationPeriods(ctx)
		}
	case len(parts) == 4 && parts[2] == "period" && menu.step == "periods":
		if parts[3] == "custom" {
			return t.reconciliationCustomPeriod(ctx, "")
		}
		if from, to, ok := reconciliationPeriod(parts[3], time.Now()); ok {
			return t.prepareReconciliation(ctx, from, to)
		}
	}
	return t.staleReconciliation(ctx)
}

func (t *Telegram) reconciliationRange(ctx context.Context, input string) error {
	if time.Now().After(t.reconciliation.until) {
		t.pending, t.reconciliation = nil, nil
		return t.staleReconciliation(ctx)
	}
	values := strings.Fields(input)
	if len(values) == 3 && (values[1] == "—" || values[1] == "–" || values[1] == "-") {
		values = []string{values[0], values[2]}
	}
	if len(values) != 2 {
		return t.reconciliationCustomPeriod(ctx, "Укажите обе даты периода.")
	}
	for i, value := range values {
		if validCalendarValue("02.01.2006", value) {
			date, _ := time.Parse("02.01.2006", value)
			values[i] = date.Format(time.DateOnly)
		} else if !validCalendarValue(time.DateOnly, value) {
			return t.reconciliationCustomPeriod(ctx, "Проверьте даты. Например, 31 февраля не существует.")
		}
	}
	if values[0] > values[1] {
		return t.reconciliationCustomPeriod(ctx, "Конечная дата должна быть не раньше начальной.")
	}
	return t.prepareReconciliation(ctx, values[0], values[1])
}

func (t *Telegram) prepareReconciliation(ctx context.Context, from, to string) error {
	menu := t.reconciliation
	workspace, err := t.service.Workspace(ctx, t.owner, "", "")
	if err != nil {
		return t.reply(ctx, "Не удалось проверить данные контрагента. Выберите период ещё раз или откройте меню.", "Акт сверки:reconciliation;Меню:menu")
	}
	party := findParty(workspace, menu.parties[menu.selected].ID)
	if party == nil {
		t.pending, t.reconciliation = nil, nil
		return t.reconciliationMessage(ctx, menu, "Контрагент больше не найден в рабочей области. Откройте список заново.", "Акт сверки:reconciliation;Меню:menu")
	}
	draft := &Draft{Kind: "reconciliation", CounterpartyID: party.ID, CounterpartyName: party.Name, From: from, To: to}
	prefix := menu.screen("confirm")
	t.pending, t.pendingID, t.pendingUntil = draft, uuid.NewString(), menu.until
	buttons := "Сформировать PDF:confirm:" + t.pendingID + ";Назад:" + prefix + "back,Отмена:cancel:" + t.pendingID
	return t.reconciliationMessage(ctx, menu, reconciliationSummary(draft)+"\n\nПодтвердите формирование неподписанного PDF.", buttons)
}

func (t *Telegram) staleReconciliation(ctx context.Context) error {
	return t.reply(ctx, "Эта кнопка устарела. Продолжите в последнем сообщении или откройте акт сверки заново.", "Акт сверки:reconciliation;Меню:menu")
}

func (t *Telegram) reconciliationMessage(ctx context.Context, menu *reconciliationMenu, text, buttons string) error {
	if menu == nil {
		return t.reply(ctx, text, buttons)
	}
	if menu.messageID != 0 {
		// Edits retain the original message's content protection. Escape all ledger
		// text because this existing edit API uses HTML, unlike protected sends.
		if err := t.bot.EditHTMLMessageWithButtons(ctx, t.userID, menu.messageID, html.EscapeString(text), buttons); err == nil {
			return nil
		}
	}
	id, err := t.bot.SendProtectedMessage(ctx, t.userID, text, buttons)
	if err != nil {
		return errors.New("business Telegram message failed")
	}
	menu.messageID = id
	return nil
}

func reconciliationPeriod(period string, now time.Time) (string, string, bool) {
	today := now.In(time.FixedZone("MSK", 3*3600))
	from := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	to := today
	switch period {
	case "previous":
		to, from = from.AddDate(0, 0, -1), from.AddDate(0, -1, 0)
	case "current":
	case "year":
		from = time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, today.Location())
	default:
		return "", "", false
	}
	return from.Format(time.DateOnly), to.Format(time.DateOnly), true
}

func reconciliationSummary(draft *Draft) string {
	return "Акт сверки\n" + limitText(draft.CounterpartyName, 160) + "\nС " + reconciliationDisplayDate(draft.From) + " по " + reconciliationDisplayDate(draft.To) + " включительно."
}

func reconciliationDisplayDate(value string) string {
	date, _ := time.Parse(time.DateOnly, value)
	return date.Format("02.01.2006")
}

func reconciliationButtonLabel(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case ':':
			return '：'
		case ',':
			return '，'
		case ';':
			return '；'
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.Join(strings.Fields(name), " "))
	if name == "" {
		return "Контрагент без названия"
	}
	if utf8.RuneCountInString(name) > 56 {
		return limitText(name, 55) + "…"
	}
	return name
}
