package business

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errServiceActAlreadyStored = errors.New("service act already stored")

func (s *Service) ServiceActPDF(ctx context.Context, owner, partyID, templateID, period string) (File, error) {
	if _, _, err := periodDates(period); err != nil {
		return File{}, err
	}
	var file File
	err := s.mutate(ctx, owner, func(current *state, tx pgx.Tx) error {
		party, ok := findParty(*current, partyID)
		if !ok {
			return missing("Плательщик не найден")
		}
		for i := range current.Invoices {
			invoice := &current.Invoices[i]
			if !invoice.IsCurrent || invoice.CounterpartyID != partyID || invoice.TemplateID != templateID || invoice.Period != period {
				continue
			}
			for _, document := range invoice.Documents {
				if document.Kind == "act" && document.MIMEType == "application/pdf" {
					file.Document = document
					return errServiceActAlreadyStored
				}
			}
			today := s.now().In(s.calendarZone).Format(time.DateOnly)
			if invoice.PeriodEnd > today || !confirmedInvoice(*invoice) {
				return conflict("Проверьте период и статус счёта")
			}
			data, err := renderBillingPDF(current.Seller, party, *invoice, true)
			if err != nil {
				return err
			}
			document, err := saveFile(ctx, tx, owner, uuid.NewString(), "Акт услуг "+period+".pdf", "application/pdf", data)
			if err != nil {
				return err
			}
			document.Kind = "act"
			document.GeneratedServiceAct = true
			invoice.Documents = append(invoice.Documents, document)
			file = File{Document: document, Data: data}
			return nil
		}
		return missing("Счёт за указанный период не найден")
	})
	if errors.Is(err, errServiceActAlreadyStored) {
		// The transaction released the workspace lock without rewriting the ledger.
		stored, err := s.File(ctx, owner, file.ID)
		if err != nil {
			return File{}, err
		}
		file.Data = stored.Data
		return file, nil
	}
	if err != nil {
		return File{}, err
	}
	return file, nil
}
