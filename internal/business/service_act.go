package business

import (
	"context"
	"time"
)

func (s *Service) ServiceActPDF(ctx context.Context, owner, partyID, templateID, period string) (File, error) {
	if _, _, err := periodDates(period); err != nil {
		return File{}, err
	}
	current, err := s.load(ctx, owner)
	if err != nil {
		return File{}, err
	}
	party, ok := findParty(current, partyID)
	if !ok {
		return File{}, missing("Плательщик не найден")
	}
	for _, invoice := range current.Invoices {
		if !invoice.IsCurrent || invoice.CounterpartyID != partyID || invoice.TemplateID != templateID || invoice.Period != period {
			continue
		}
		for _, doc := range invoice.Documents {
			if doc.Kind == "act" && doc.MIMEType == "application/pdf" {
				return s.File(ctx, owner, doc.ID)
			}
		}
		today := s.now().In(time.FixedZone("MSK", 3*3600)).Format(time.DateOnly)
		if invoice.PeriodEnd > today || invoice.ProviderState == "pending" || invoice.ProviderState == "unknown" {
			return File{}, conflict("Проверьте период и статус счёта")
		}
		data, err := renderBillingPDF(current.Seller, party, invoice, true)
		if err != nil {
			return File{}, err
		}
		return File{Document: Document{Filename: "Акт услуг " + period + ".pdf", MIMEType: "application/pdf", Size: len(data)}, Data: data}, nil
	}
	return File{}, missing("Счёт за указанный период не найден")
}
