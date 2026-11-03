package business

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) Import(ctx context.Context, owner string, request ImportRequest) (ImportResult, error) {
	var result ImportResult
	if len(request.Documents) > 2000 || len(request.Invoices) > 2000 || len(request.Counterparties) > 250 || len(request.Payments) > 10000 {
		return result, invalid("Слишком большой пакет импорта")
	}
	err := s.mutate(ctx, owner, func(current *state, tx pgx.Tx) error {
		if request.Seller.Name != "" {
			if err := validateSeller(request.Seller); err != nil {
				return err
			}
			current.Seller = request.Seller
		}
		partyIDs := map[string]bool{}
		for _, party := range request.Counterparties {
			if partyIDs[party.ID] {
				return conflict("Повторяется идентификатор плательщика в импорте")
			}
			partyIDs[party.ID] = true
			if err := normalizeParty(&party); err != nil {
				return err
			}
			index := -1
			for i, old := range current.Counterparties {
				if old.ID == party.ID {
					index = i
					for _, template := range old.Templates {
						found := false
						for _, updated := range party.Templates {
							if updated.ID == template.ID {
								found = true
							}
						}
						if !found {
							party.Templates = append(party.Templates, template)
						}
					}
					break
				}
			}
			if index < 0 {
				current.Counterparties = append(current.Counterparties, party)
			} else {
				current.Counterparties[index] = party
			}
		}
		files := map[string]Document{}
		var importedBytes int
		for _, item := range request.Documents {
			if _, duplicate := files[item.ID]; duplicate {
				return conflict("Повторяется идентификатор документа в импорте")
			}
			document, err := importedFile(ctx, tx, owner, item)
			if err != nil {
				return err
			}
			importedBytes += document.Size
			if importedBytes > 64<<20 {
				return invalid("Документы пакета превышают 64 МБ")
			}
			files[item.ID] = document
		}
		invoiceIDs := map[string]bool{}
		for _, invoice := range request.Invoices {
			if invoiceIDs[invoice.ID] {
				return conflict("Повторяется идентификатор счёта в импорте")
			}
			invoiceIDs[invoice.ID] = true
			old, exists := findInvoice(*current, invoice.ID)
			if exists && invoice.CreatedAt == "" {
				invoice.CreatedAt = old.CreatedAt
			}
			if err := normalizeInvoice(&invoice, s.now()); err != nil {
				return err
			}
			if invoice.ProviderState == "" {
				invoice.ProviderState = "imported"
			}
			if invoice.Provider != "local" || invoice.BankDocumentID != "" {
				return invalid("Импорт не может создавать банковские операции")
			}
			for i, reference := range invoice.Documents {
				if reference.Kind != "invoice" && reference.Kind != "act" && reference.Kind != "other" {
					return invalid("Неизвестный вид документа")
				}
				metadata, found := files[reference.ID]
				if !found {
					err := tx.QueryRow(ctx, `SELECT id,filename,mime_type,sha256,octet_length(data) FROM business_files WHERE owner_id=$1::uuid AND id=$2`, owner, reference.ID).Scan(&metadata.ID, &metadata.Filename, &metadata.MIMEType, &metadata.SHA256, &metadata.Size)
					if errors.Is(err, pgx.ErrNoRows) {
						return invalid("В импорте отсутствует файл документа")
					}
					if err != nil {
						return err
					}
				}
				metadata.Kind = reference.Kind
				invoice.Documents[i] = metadata
			}
			if exists {
				if !reflect.DeepEqual(old, invoice) {
					return conflict("Сохранённый счёт неизменяем: используйте отдельную архивную версию")
				}
				continue
			}
			current.Invoices = append(current.Invoices, invoice)
		}
		paymentIDs := map[string]bool{}
		newPayments := []Payment{}
		for _, payment := range request.Payments {
			if paymentIDs[payment.ID] {
				return conflict("Повторяется идентификатор платежа в импорте")
			}
			paymentIDs[payment.ID] = true
			if err := normalizePayment(&payment); err != nil {
				return err
			}
			if exists, err := mergeExistingPayment(current, payment); err != nil {
				return err
			} else if exists {
				continue
			}
			current.Payments = append(current.Payments, payment)
			newPayments = append(newPayments, payment)
		}
		automaticAllocations(current, newPayments)
		if request.Bank != nil && (request.Bank.CoverageStart != "" || request.Bank.CoverageEnd != "") {
			if !validDate(request.Bank.CoverageStart) || !validDate(request.Bank.CoverageEnd) || request.Bank.CoverageStart > request.Bank.CoverageEnd {
				return invalid("Неверный период выписки")
			}
			addCoverage(current, request.Bank.CoverageStart, request.Bank.CoverageEnd)
			if request.Bank.AccountCount < 0 || request.Bank.AccountCount > 20 {
				return invalid("Неверное число банковских счетов")
			}
			current.Bank.AccountCount = max(current.Bank.AccountCount, request.Bank.AccountCount)
			if request.Bank.LastSyncedAt != "" {
				if _, err := time.Parse(time.RFC3339, request.Bank.LastSyncedAt); err != nil {
					return invalid("Неверное время выписки")
				}
				if request.Bank.LastSyncedAt > current.Bank.LastSyncedAt {
					current.Bank.LastSyncedAt = request.Bank.LastSyncedAt
				}
			}
		}
		result.CounterpartyCount, result.InvoiceCount, result.PaymentCount = len(current.Counterparties), len(current.Invoices), len(current.Payments)
		return tx.QueryRow(ctx, `SELECT count(*) FROM business_files WHERE owner_id=$1::uuid`, owner).Scan(&result.DocumentCount)
	})
	return result, err
}
