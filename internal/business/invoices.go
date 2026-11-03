package business

import (
	"context"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func validateSeller(seller Seller) error {
	if strings.TrimSpace(seller.Name) == "" || !validINN(seller.INN, false) || len(seller.Name) > 1000 || len(seller.Address) > 4000 || len(seller.BankName) > 1000 {
		return invalid("Заполните реквизиты продавца")
	}
	for _, value := range []struct {
		text   string
		length int
	}{{seller.BankBIK, 9}, {seller.BankAccount, 20}, {seller.BankCorrespondentAccount, 20}} {
		if value.text != "" && (len(value.text) != value.length || !digits.MatchString(value.text)) {
			return invalid("Неверные банковские реквизиты продавца")
		}
	}
	return nil
}

func (s *Service) CreateInvoice(ctx context.Context, owner string, request CreateInvoiceRequest) (InvoiceView, error) {
	var created Invoice
	var createdView InvoiceView
	var party Counterparty
	var seller Seller
	var token string
	provider := request.Provider
	if provider == "" {
		provider = "local"
	}
	if provider != "local" && provider != "tochka" {
		return InvoiceView{}, invalid("Неизвестный способ создания счёта")
	}
	err := s.mutate(ctx, owner, func(current *state, tx pgx.Tx) error {
		var found bool
		party, found = findParty(*current, request.CounterpartyID)
		if !found {
			return missing("Плательщик не найден")
		}
		var template *InvoiceTemplate
		for i := range party.Templates {
			if party.Templates[i].ID == request.TemplateID {
				template = &party.Templates[i]
				break
			}
		}
		if template == nil {
			return missing("Шаблон не найден")
		}
		for _, invoice := range current.Invoices {
			if invoice.IsCurrent && invoice.CounterpartyID == party.ID && invoice.TemplateID == template.ID && invoice.Period == request.Period {
				return conflict("Текущий счёт для этого шаблона и периода уже существует")
			}
		}
		if err := validateSeller(current.Seller); err != nil {
			return err
		}
		seller = current.Seller
		if provider == "tochka" {
			if party.EntityType == "person" {
				return invalid("Банковское создание доступно только для компаний и ИП")
			}
			if s.bank == nil {
				return unavailable("Банк не настроен")
			}
			var err error
			token, err = s.tokenTx(ctx, tx, owner)
			if err != nil {
				return err
			}
		}
		number := strings.TrimSpace(request.Number)
		if number == "" {
			number = nextNumber(*current)
		}
		basis := template.Basis
		if request.Basis != nil {
			basis = *request.Basis
		}
		created = Invoice{ID: uuid.NewString(), CounterpartyID: party.ID, TemplateID: template.ID, Number: number, IssueDate: request.IssueDate, DueDate: request.DueDate, Period: request.Period, Basis: basis, Items: request.Items, IsCurrent: true, Revision: 1, Source: "created", Provider: provider, ProviderState: "created", CreatedAt: s.now().UTC().Format(time.RFC3339)}
		if provider == "tochka" {
			created.ProviderState = "unknown"
		}
		if err := normalizeInvoice(&created, s.now()); err != nil {
			return err
		}
		data, err := renderInvoice(seller, party, created)
		if err != nil {
			return err
		}
		document, err := saveFile(ctx, tx, owner, uuid.NewString(), "Счёт "+created.Number+" от "+created.IssueDate+".pdf", "application/pdf", data)
		if err != nil {
			return err
		}
		document.Kind = "invoice"
		created.Documents = []Document{document}
		current.Invoices = append(current.Invoices, created)
		createdView = viewInvoice(*current, created)
		return nil
	})
	if err != nil {
		return InvoiceView{}, err
	}
	if provider == "local" {
		return createdView, nil
	}
	// The durable pending intent above blocks concurrent/repeated writes even if
	// the process dies after the bank accepts the invoice but before its reply.
	documentID, createErr := s.bank.CreateInvoice(ctx, token, seller, party, created)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	err = s.mutate(finishCtx, owner, func(current *state, _ pgx.Tx) error {
		for i := range current.Invoices {
			if current.Invoices[i].ID == created.ID {
				current.Invoices[i].ProviderState = "unknown"
				if createErr == nil {
					current.Invoices[i].ProviderState = "created"
					current.Invoices[i].BankDocumentID = documentID
				} else if rejectedCreate(createErr) {
					current.Invoices[i].ProviderState = "rejected"
					current.Invoices[i].IsCurrent = false
				}
				created = current.Invoices[i]
				createdView = viewInvoice(*current, created)
				return nil
			}
		}
		return missing("Счёт не найден")
	})
	if err != nil {
		return InvoiceView{}, err
	}
	if createErr != nil && rejectedCreate(createErr) {
		return InvoiceView{}, &workout.Error{Status: 400, Message: "Точка отклонила счёт. Проверьте ключ/реквизиты; можно создать PDF в Utils."}
	}
	// Return the persisted invoice so the UI can show the unknown state; a retry
	// with the same payer/template/period is rejected before another bank call.
	return createdView, nil
}

func (s *Service) Allocate(ctx context.Context, owner string, request AllocateRequest) (Allocation, error) {
	allocation := Allocation{ID: uuid.NewString(), InvoiceID: request.InvoiceID, PaymentID: request.PaymentID, AmountMinor: request.AmountMinor}
	err := s.mutate(ctx, owner, func(current *state, _ pgx.Tx) error {
		invoice, ok := findInvoice(*current, request.InvoiceID)
		if !ok {
			return missing("Счёт не найден")
		}
		if !invoice.IsCurrent {
			return conflict("Архивный счёт не создаёт отдельную задолженность")
		}
		payment, ok := findPayment(*current, request.PaymentID)
		if !ok {
			return missing("Платёж не найден")
		}
		classification := matchPayment(*current, payment).classification
		if ownPayment(*current, payment) || classification == "historical" || classification == "outside_period" {
			return conflict("Этот платёж не доступен для текущих начислений")
		}
		current.Allocations = append(current.Allocations, allocation)
		return nil
	})
	return allocation, err
}
func (s *Service) ReverseAllocation(ctx context.Context, owner, id string) error {
	return s.mutate(ctx, owner, func(current *state, _ pgx.Tx) error {
		for i, allocation := range current.Allocations {
			if allocation.ID == id {
				current.Allocations = append(current.Allocations[:i], current.Allocations[i+1:]...)
				return nil
			}
		}
		return missing("Распределение не найдено")
	})
}
