package business

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

func findParty(s state, id string) (Counterparty, bool) {
	for _, p := range s.Counterparties {
		if p.ID == id {
			return p, true
		}
	}
	return Counterparty{}, false
}
func findInvoice(s state, id string) (Invoice, bool) {
	for _, invoice := range s.Invoices {
		if invoice.ID == id {
			return invoice, true
		}
	}
	return Invoice{}, false
}
func findPayment(s state, id string) (Payment, bool) {
	for _, payment := range s.Payments {
		if payment.ID == id {
			return payment, true
		}
	}
	return Payment{}, false
}
func normalizeParty(p *Counterparty) error {
	p.Name = strings.TrimSpace(p.Name)
	p.INN = strings.TrimSpace(p.INN)
	if !validID(p.ID) || p.Name == "" || utf8.RuneCountInString(p.Name) > 500 || !validINN(p.INN, p.EntityType == "person") || (p.EntityType != "person" && p.EntityType != "company" && p.EntityType != "ip") {
		return invalid("Неверные реквизиты плательщика")
	}
	if p.Contacts == nil {
		p.Contacts = []Contact{}
	}
	if p.Templates == nil {
		p.Templates = []InvoiceTemplate{}
	}
	if len(p.Contacts) > 100 || len(p.Templates) > 100 || len(p.Address) > 4000 {
		return invalid("Слишком много реквизитов плательщика")
	}
	for _, contact := range p.Contacts {
		if contact.Email != "" {
			address, err := mail.ParseAddress(contact.Email)
			if err != nil || address.Address != contact.Email {
				return invalid("Неверный email плательщика")
			}
		}
		if len(contact.Note) > 4000 {
			return invalid("Слишком длинное примечание контакта")
		}
	}
	ids := map[string]bool{}
	for index := range p.Templates {
		template := &p.Templates[index]
		if !validID(template.ID) || ids[template.ID] || template.Label == "" || len(template.Basis) > 8000 {
			return invalid("Неверный шаблон счёта")
		}
		ids[template.ID] = true
		items, _, err := lineAmounts(template.Items)
		if err != nil {
			return err
		}
		template.Items = items
	}
	return nil
}
func normalizeInvoice(invoice *Invoice, now time.Time) error {
	if !validID(invoice.ID) || !validID(invoice.CounterpartyID) || !validID(invoice.TemplateID) || strings.TrimSpace(invoice.Number) == "" || len(invoice.Number) > 64 || strings.ContainsAny(invoice.Number, "\x00\r\n") || !validDate(invoice.IssueDate) {
		return invalid("Неверные реквизиты счёта")
	}
	start, end, err := periodDates(invoice.Period)
	if err != nil {
		return err
	}
	if invoice.DueDate != "" && (!validDate(invoice.DueDate) || invoice.DueDate < invoice.IssueDate) {
		return invalid("Срок оплаты не может быть раньше даты счёта")
	}
	if invoice.PeriodStart == "" && invoice.PeriodEnd == "" {
		invoice.PeriodStart, invoice.PeriodEnd = start, end
	} else if !validDate(invoice.PeriodStart) || !validDate(invoice.PeriodEnd) || invoice.PeriodStart > invoice.PeriodEnd {
		return invalid("Неверный период оказания услуг")
	}
	items, total, err := lineAmounts(invoice.Items)
	if err != nil {
		return err
	}
	invoice.Items, invoice.AmountMinor = items, total
	if invoice.Currency != "" && invoice.Currency != "RUB" {
		return invalid("Поддерживаются только рубли")
	}
	invoice.Currency = "RUB"
	if invoice.VAT == "" {
		invoice.VAT = "Без НДС"
	}
	if len(invoice.VAT) > 200 || len(invoice.Basis) > 8000 || len(invoice.Source) > 2000 {
		return invalid("Слишком длинные реквизиты счёта")
	}
	if invoice.Provider == "" {
		invoice.Provider = "local"
	}
	if invoice.Provider != "local" && invoice.Provider != "tochka" {
		return invalid("Неизвестный способ создания счёта")
	}
	if invoice.Revision < 1 {
		invoice.Revision = 1
	}
	if invoice.CreatedAt == "" {
		invoice.CreatedAt = now.UTC().Format(time.RFC3339)
	}
	if _, err := time.Parse(time.RFC3339, invoice.CreatedAt); err != nil {
		return invalid("Неверная дата создания счёта")
	}
	if invoice.Documents == nil {
		invoice.Documents = []Document{}
	}
	return nil
}
func normalizePayment(payment *Payment) error {
	if !validID(payment.ID) || !validDate(payment.Date) || payment.AmountMinor <= 0 || payment.AmountMinor > maxMinor || !validINN(payment.PayerINN, true) {
		return invalid("Неверные реквизиты платежа")
	}
	if payment.Currency != "" && payment.Currency != "RUB" {
		return invalid("Поддерживаются только рубли")
	}
	payment.Currency = "RUB"
	if len(payment.Description) > 10000 || len(payment.CounterpartyName) > 1000 || len(payment.AccountLabel) > 500 || len(payment.Source) > 2000 {
		return invalid("Слишком длинные реквизиты платежа")
	}
	return nil
}
func validateState(s state) error {
	if len(s.Counterparties) > 250 || len(s.Invoices) > 2000 || len(s.Payments) > 10000 || len(s.Allocations) > 20000 {
		return invalid("Достигнут предел размера личного архива")
	}
	parties, invoices, payments, allocationIDs, current := map[string]bool{}, map[string]Invoice{}, map[string]Payment{}, map[string]bool{}, map[string]bool{}
	var invoiceTotal, paymentTotal int64
	for _, p := range s.Counterparties {
		if parties[p.ID] {
			return conflict("Повторяется идентификатор плательщика")
		}
		parties[p.ID] = true
	}
	for _, invoice := range s.Invoices {
		if !parties[invoice.CounterpartyID] {
			return invalid("Счёт ссылается на неизвестного плательщика")
		}
		if _, exists := invoices[invoice.ID]; exists {
			return conflict("Повторяется идентификатор счёта")
		}
		invoices[invoice.ID] = invoice
		party, _ := findParty(s, invoice.CounterpartyID)
		templateExists := false
		for _, template := range party.Templates {
			if template.ID == invoice.TemplateID {
				templateExists = true
			}
		}
		if !templateExists {
			return invalid("Счёт ссылается на неизвестный шаблон")
		}
		if invoice.IsCurrent {
			key := invoice.CounterpartyID + "\x00" + invoice.TemplateID + "\x00" + invoice.Period
			if current[key] {
				return conflict("Текущий счёт для этого шаблона и периода уже существует")
			}
			current[key] = true
			if invoice.AmountMinor > maxMinor-invoiceTotal {
				return invalid("Общая сумма архива слишком велика")
			}
			invoiceTotal += invoice.AmountMinor
		}
	}
	for _, p := range s.Payments {
		if _, exists := payments[p.ID]; exists {
			return conflict("Повторяется идентификатор платежа")
		}
		if p.CounterpartyID != "" && !parties[p.CounterpartyID] {
			return invalid("Платёж ссылается на неизвестного плательщика")
		}
		payments[p.ID] = p
		if p.AmountMinor > maxMinor-paymentTotal {
			return invalid("Общая сумма платежей слишком велика")
		}
		paymentTotal += p.AmountMinor
	}
	invoiced, paid := map[string]int64{}, map[string]int64{}
	for _, a := range s.Allocations {
		invoice, iok := invoices[a.InvoiceID]
		payment, pok := payments[a.PaymentID]
		if !validID(a.ID) || allocationIDs[a.ID] || !iok || !pok || !invoice.IsCurrent || a.AmountMinor <= 0 || a.AmountMinor > invoice.AmountMinor-invoiced[a.InvoiceID] || a.AmountMinor > payment.AmountMinor-paid[a.PaymentID] {
			return conflict("Распределение превышает остаток счёта или платежа")
		}
		allocationIDs[a.ID] = true
		invoiced[a.InvoiceID] += a.AmountMinor
		paid[a.PaymentID] += a.AmountMinor
	}
	return nil
}
