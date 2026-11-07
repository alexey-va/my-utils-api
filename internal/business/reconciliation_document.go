package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

type reconciliationRow struct {
	date, id, description string
	debit, credit         int64
}

type reconciliationReceipt struct {
	date, description string
	amount            int64
}

type reconciliationStatement struct {
	seller                          Seller
	buyer                           Counterparty
	from, to                        string
	generatedAt                     time.Time
	opening, debit, credit, closing int64
	rows                            []reconciliationRow
	excluded                        []reconciliationReceipt
	notes                           []string
}

// ReconciliationPDF is a read-only projection of this owner's current ledger.
// Payment dates determine the statement period; allocations supply the evidence
// of settlement. An invoice and its attached service act are one receivable.
func (s *Service) ReconciliationPDF(ctx context.Context, owner, party, from, to string) (File, error) {
	if !validID(party) || !validDate(from) || !validDate(to) || from > to {
		return File{}, invalid("Выберите контрагента и корректные даты сверки")
	}
	current, err := s.load(ctx, owner)
	if err != nil {
		return File{}, err
	}
	statement, err := buildReconciliation(current, party, from, to, s.now())
	if err != nil {
		return File{}, err
	}
	data, err := renderReconciliation(statement)
	if err != nil {
		return File{}, err
	}
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	return File{Document: Document{
		ID: "reconciliation-" + sha, Kind: "other", Filename: "Акт сверки " + from + "_" + to + ".pdf",
		MIMEType: "application/pdf", Size: len(data), SHA256: sha,
	}, Data: data}, nil
}

func buildReconciliation(s state, party, from, to string, generatedAt time.Time) (reconciliationStatement, error) {
	r := reconciliationStatement{seller: s.Seller, from: from, to: to, generatedAt: generatedAt}
	if !validID(party) || !validDate(from) || !validDate(to) || from > to {
		return r, invalid("Выберите контрагента и корректные даты сверки")
	}
	var ok bool
	r.buyer, ok = findParty(s, party)
	if !ok {
		return r, missing("Контрагент не найден")
	}
	if strings.TrimSpace(s.Seller.Name) == "" {
		return r, invalid("Сначала заполните реквизиты поставщика")
	}
	add := func(row reconciliationRow) {
		if row.date < from {
			r.opening += row.debit - row.credit
		} else if row.date <= to {
			r.rows = append(r.rows, row)
			r.debit += row.debit
			r.credit += row.credit
		}
	}
	invoices := map[string]Invoice{}
	coverageFrom, uncertain := from, false
	for _, invoice := range s.Invoices {
		if !invoice.IsCurrent || invoice.CounterpartyID != party {
			continue
		}
		invoices[invoice.ID] = invoice
		if invoice.IssueDate > to {
			continue
		}
		coverageFrom = min(coverageFrom, invoice.IssueDate)
		description := "Начисление: " + reconciliationInvoiceLabel(invoice)
		if invoice.PeriodStart != "" && invoice.PeriodEnd != "" {
			description += "\nУслуги: " + reconciliationDate(invoice.PeriodStart) + " - " + reconciliationDate(invoice.PeriodEnd)
		}
		if invoice.Basis != "" {
			description += "\n" + reconciliationShort(invoice.Basis, 150)
		}
		if invoice.ProviderState == "unknown" || invoice.ProviderState == "pending" {
			uncertain = true
			description += "\nВыставление счёта требует проверки"
		}
		add(reconciliationRow{date: invoice.IssueDate, id: "0-" + invoice.ID, description: description, debit: invoice.AmountMinor})
	}
	allocations := map[string][]Allocation{}
	for _, allocation := range s.Allocations {
		if _, current := invoices[allocation.InvoiceID]; current {
			allocations[allocation.PaymentID] = append(allocations[allocation.PaymentID], allocation)
		}
	}
	unidentified, unresolved := false, false
	for _, payment := range paymentViews(s) {
		if payment.Date > to {
			continue
		}
		belongs := payment.CounterpartyID == party || payment.CounterpartyID == "" && uniquePayerID(s, payment.PayerINN) == party
		if payment.Classification == "historical" || payment.Classification == "outside_period" {
			linkedInvoice, linked := findInvoice(s, payment.HistoricalInvoiceID)
			belongs = belongs || linked && linkedInvoice.CounterpartyID == party
			if belongs && payment.Date >= from {
				description := "Историческое поступление / вне текущего учёта"
				if linked {
					description += "\n" + reconciliationInvoiceLabel(linkedInvoice)
				}
				r.excluded = append(r.excluded, reconciliationReceipt{payment.Date, description, payment.AmountMinor})
			}
			continue
		}
		var credited int64
		labels, seen := []string{}, map[string]bool{}
		for _, allocation := range allocations[payment.ID] {
			credited += allocation.AmountMinor
			if !seen[allocation.InvoiceID] {
				seen[allocation.InvoiceID] = true
				labels = append(labels, reconciliationInvoiceLabel(invoices[allocation.InvoiceID]))
			}
		}
		if credited > 0 {
			sort.Strings(labels)
			description := "Поступление оплаты\n" + strings.Join(labels[:min(3, len(labels))], "; ")
			if len(labels) > 3 {
				description += fmt.Sprintf("; ещё счетов: %d", len(labels)-3)
			}
			if credited != payment.AmountMinor {
				description += "\nЧасть поступления " + reconciliationMoney(payment.AmountMinor) + " руб."
			}
			add(reconciliationRow{date: payment.Date, id: "1-" + payment.ID, description: description, credit: credited})
			coverageFrom = min(coverageFrom, payment.Date)
		}
		if elsewhere := payment.AllocatedMinor - credited; belongs && payment.Date >= from && elsewhere > 0 {
			r.excluded = append(r.excluded, reconciliationReceipt{payment.Date, "Распределено по счетам другого контрагента", elsewhere})
		}
		if belongs && payment.UnallocatedMinor > 0 {
			unresolved = unresolved || payment.UnallocatedMinor > payment.AdvanceMinor
			prefix := ""
			if payment.Date < from {
				prefix = "До начала периода. "
			}
			if payment.AdvanceMinor > 0 {
				r.excluded = append(r.excluded, reconciliationReceipt{payment.Date, prefix + "Аванс; не зачтён в сальдо по начислениям", payment.AdvanceMinor})
			}
			if remaining := payment.UnallocatedMinor - payment.AdvanceMinor; remaining > 0 {
				r.excluded = append(r.excluded, reconciliationReceipt{payment.Date, prefix + "Не распределено по счетам; требуется проверка", remaining})
			}
		}
		if payment.CounterpartyID == "" && uniquePayerID(s, payment.PayerINN) == "" && payment.UnallocatedMinor > payment.AdvanceMinor {
			unidentified = true
		}
	}
	sort.Slice(r.rows, func(i, j int) bool {
		if r.rows[i].date == r.rows[j].date {
			return r.rows[i].id < r.rows[j].id
		}
		return r.rows[i].date < r.rows[j].date
	})
	r.closing = r.opening + r.debit - r.credit
	covered := false
	for _, span := range s.Coverage {
		if span.From <= coverageFrom && span.To >= to {
			covered = true
		}
	}
	if !covered {
		r.notes = append(r.notes, "Полная банковская выписка за "+reconciliationDate(coverageFrom)+" - "+reconciliationDate(to)+" не подтверждена; возможны отсутствующие поступления.")
	}
	if unresolved {
		r.notes = append(r.notes, "Есть нераспределённые поступления контрагента на дату окончания периода, в том числе до его начала. До проверки сальдо не является окончательным.")
	}
	if unidentified {
		r.notes = append(r.notes, "В реестре есть поступления по дату окончания периода без установленного контрагента. Их принадлежность требует проверки.")
	}
	if uncertain {
		r.notes = append(r.notes, "В расчёт включены текущие счета, выставление которых ещё не подтверждено; суммы требуют проверки.")
	}
	return r, nil
}

func reconciliationInvoiceLabel(invoice Invoice) string {
	return "счёт № " + invoice.Number + " от " + reconciliationDate(invoice.IssueDate)
}

func reconciliationDate(value string) string {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return value
	}
	return date.Format("02.01.2006")
}

func reconciliationShort(value string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > limit {
		return string(runes[:limit-3]) + "..."
	}
	return string(runes)
}

func reconciliationMoney(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	parts := strings.Split(moneyText(minor), ",")
	for i := len(parts[0]) - 3; i > 0; i -= 3 {
		parts[0] = parts[0][:i] + " " + parts[0][i:]
	}
	return sign + strings.Join(parts, ",")
}

func renderReconciliation(r reconciliationStatement) ([]byte, error) {
	const margin, width, bottom = 14.0, 182.0, 277.0
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(margin, 15, margin)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Go", "B", gobold.TTF)
	pdf.SetTitle("Акт сверки взаимных расчётов", true)
	pdf.SetAuthor(r.seller.Name, true)
	pdf.SetCreationDate(r.generatedAt)
	pdf.SetCatalogSort(true)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(150, 150, 150)
	pdf.SetTextColor(25, 25, 25)
	pdf.SetHeaderFunc(func() {
		if pdf.PageNo() > 1 {
			pdf.SetFont("Go", "B", 9)
			pdf.CellFormat(width, 5, "Акт сверки  |  "+reconciliationDate(r.from)+" - "+reconciliationDate(r.to), "", 1, "L", false, 0, "")
			pdf.SetFont("Go", "", 8)
			pdf.CellFormat(width, 5, reconciliationShort(r.buyer.Name, 90), "", 1, "L", false, 0, "")
			pdf.Ln(4)
		}
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("Go", "", 7.5)
		pdf.CellFormat(110, 5, "Сформировано "+r.generatedAt.In(time.FixedZone("MSK", 3*3600)).Format("02.01.2006 15:04")+" МСК", "", 0, "L", false, 0, "")
		pdf.CellFormat(width-110, 5, fmt.Sprintf("Страница %d/{nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AliasNbPages("")
	pdf.AddPage()
	pdf.SetFont("Go", "B", 17)
	pdf.CellFormat(width, 9, "АКТ СВЕРКИ", "", 1, "C", false, 0, "")
	pdf.SetFont("Go", "", 12)
	pdf.CellFormat(width, 7, "взаимных расчётов", "", 1, "C", false, 0, "")
	pdf.SetFont("Go", "", 10)
	pdf.CellFormat(width, 7, "за период с "+reconciliationDate(r.from)+" по "+reconciliationDate(r.to), "", 1, "C", false, 0, "")
	pdf.Ln(6)
	for _, party := range []struct{ label, name, inn string }{{"Поставщик", r.seller.Name, r.seller.INN}, {"Контрагент", r.buyer.Name, r.buyer.INN}} {
		pdf.SetFont("Go", "B", 9.5)
		pdf.MultiCell(width, 5, party.label+": "+party.name, "", "L", false)
		if party.inn != "" {
			pdf.SetFont("Go", "", 9)
			pdf.MultiCell(width, 5, "ИНН "+party.inn, "", "L", false)
		}
		pdf.Ln(2)
	}
	pdf.SetFont("Go", "", 9)
	pdf.MultiCell(width, 4.6, "По данным поставщика. Валюта: российский рубль.\nДебет - начисления; кредит - поступления, распределённые по текущим счетам.", "", "L", false)
	pdf.Ln(4)
	// A fixed four-column grid leaves room for readable money and long references.
	widths := []float64{23, 85, 37, 37}
	row := func(values []string, columns []float64, style string, fill bool) {
		pdf.SetFont("Go", style, 9)
		height := 9.0
		for i, value := range values {
			if i < 2 {
				height = max(height, float64(len(pdf.SplitText(value, columns[i]-4)))*4.5+4)
			}
		}
		x, y := margin, pdf.GetY()
		for i, value := range values {
			pdf.SetFillColor(241, 243, 245)
			if fill {
				pdf.Rect(x, y, columns[i], height, "DF")
			} else {
				pdf.Rect(x, y, columns[i], height, "D")
			}
			pdf.SetXY(x+2, y+2)
			align := "L"
			pdf.SetFont("Go", style, 9)
			if i >= 2 {
				align = "R"
				if textWidth := pdf.GetStringWidth(value); textWidth > columns[i]-4 {
					pdf.SetFont("Go", style, 9*(columns[i]-4)/textWidth)
				}
			}
			pdf.MultiCell(columns[i]-4, 4.5, value, "", align, false)
			x += columns[i]
		}
		pdf.SetXY(margin, y+height)
	}
	header := func() {
		row([]string{"Дата", "Документ / операция", "Дебет, руб.", "Кредит, руб."}, widths, "B", true)
	}
	ensure := func(height float64, repeat func()) {
		if pdf.GetY()+height > bottom {
			pdf.AddPage()
			if repeat != nil {
				repeat()
			}
		}
	}
	ensure(30, nil)
	header()
	balanceRow := func(label string, amount int64) {
		debit, credit := "", ""
		if amount >= 0 {
			debit = reconciliationMoney(amount)
		} else {
			credit = reconciliationMoney(-amount)
		}
		ensure(15, header)
		row([]string{"", label, debit, credit}, widths, "B", true)
	}
	balanceRow("Сальдо на начало периода", r.opening)
	for _, entry := range r.rows {
		debit, credit := "", ""
		if entry.debit != 0 {
			debit = reconciliationMoney(entry.debit)
		}
		if entry.credit != 0 {
			credit = reconciliationMoney(entry.credit)
		}
		pdf.SetFont("Go", "", 9)
		height := max(9.0, float64(len(pdf.SplitText(entry.description, widths[1]-4)))*4.5+4)
		ensure(height, header)
		row([]string{reconciliationDate(entry.date), entry.description, debit, credit}, widths, "", false)
	}
	ensure(30, header)
	row([]string{"", "Обороты за период", reconciliationMoney(r.debit), reconciliationMoney(r.credit)}, widths, "B", true)
	balanceRow("Сальдо на конец периода", r.closing)
	pdf.Ln(4)
	pdf.SetFont("Go", "B", 9.5)
	result := "Сальдо на " + reconciliationDate(r.to) + " по данным реестра: "
	switch {
	case r.closing > 0:
		result += reconciliationMoney(r.closing) + " руб. в пользу поставщика."
	case r.closing < 0:
		result += reconciliationMoney(-r.closing) + " руб. в пользу контрагента."
	default:
		result += "0,00 руб."
	}
	pdf.MultiCell(width, 5, result, "", "L", false)
	pdf.Ln(3)
	pdf.SetFont("Go", "", 8)
	pdf.MultiCell(width, 4.2, "Расчёт выполнен по текущим записям реестра на дату формирования. Начальное сальдо рассчитано по доступным операциям до начала периода; полнота расчётов до начала реестра не подтверждается. Счёт и акт учитываются один раз. Архивные версии, исторические поступления и нераспределённые суммы в сальдо не включены; авансы отдельно и между договорами не зачтены.", "", "L", false)
	if len(r.excluded) > 0 {
		ensure(38, nil)
		pdf.Ln(5)
		pdf.SetFont("Go", "B", 10)
		pdf.MultiCell(width, 5, "Поступления, не включённые в сальдо", "", "L", false)
		pdf.SetFont("Go", "", 8)
		pdf.MultiCell(width, 4.2, "Поступления периода и нераспределённые остатки до его начала.", "", "L", false)
		pdf.Ln(2)
		extraWidths := []float64{23, 122, 37}
		extraHeader := func() {
			row([]string{"Дата", "Основание исключения", "Сумма, руб."}, extraWidths, "B", true)
		}
		extraHeader()
		for _, receipt := range r.excluded {
			pdf.SetFont("Go", "", 9)
			height := max(9.0, float64(len(pdf.SplitText(receipt.description, extraWidths[1]-4)))*4.5+4)
			ensure(height, extraHeader)
			row([]string{reconciliationDate(receipt.date), receipt.description, reconciliationMoney(receipt.amount)}, extraWidths, "", false)
		}
	}
	if len(r.notes) > 0 {
		ensure(24, nil)
		pdf.Ln(5)
		pdf.SetFont("Go", "B", 9.5)
		pdf.MultiCell(width, 5, "Перед согласованием требуется проверка", "", "L", false)
		pdf.SetFont("Go", "", 8)
		for _, note := range r.notes {
			pdf.MultiCell(width, 4.2, "- "+note, "", "L", false)
		}
	}
	ensure(45, nil)
	pdf.Ln(7)
	y := pdf.GetY()
	for i, party := range []struct{ label, name string }{{"От поставщика", r.seller.Name}, {"От контрагента", r.buyer.Name}} {
		x := margin + float64(i)*96
		pdf.SetXY(x, y)
		pdf.SetFont("Go", "B", 9)
		pdf.MultiCell(86, 5, party.label, "", "L", false)
		pdf.SetFont("Go", "", 8)
		pdf.SetX(x)
		pdf.MultiCell(86, 4.2, reconciliationShort(party.name, 80), "", "L", false)
		pdf.SetXY(x, y+20)
		pdf.CellFormat(86, 6, "________________ / ____________________", "", 1, "L", false, 0, "")
		pdf.SetXY(x, y+26)
		pdf.CellFormat(86, 5, "подпись                         расшифровка", "", 1, "L", false, 0, "")
		pdf.SetXY(x, y+32)
		pdf.CellFormat(86, 5, "Дата: __________________", "", 1, "L", false, 0, "")
	}
	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
