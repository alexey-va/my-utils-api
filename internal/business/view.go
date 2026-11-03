package business

import (
	"sort"
	"strconv"
)

func nextNumber(s state) string {
	var largest int64
	for _, invoice := range s.Invoices {
		if n, err := strconv.ParseInt(invoice.Number, 10, 64); err == nil && n > largest {
			largest = n
		}
	}
	if largest == 1<<63-1 {
		return ""
	}
	return strconv.FormatInt(largest+1, 10)
}
func invoiceCovered(s state, invoice Invoice) bool {
	for _, coverage := range s.Coverage {
		if coverage.From <= invoice.PeriodStart && coverage.To >= invoice.PeriodEnd {
			return true
		}
	}
	return false
}
func viewInvoice(s state, invoice Invoice) InvoiceView {
	v := InvoiceView{Invoice: invoice, Allocations: []Allocation{}}
	for _, allocation := range s.Allocations {
		if allocation.InvoiceID == invoice.ID {
			v.PaidMinor += allocation.AmountMinor
			v.Allocations = append(v.Allocations, allocation)
		}
	}
	v.OutstandingMinor = invoice.AmountMinor - v.PaidMinor
	switch {
	case !invoice.IsCurrent:
		v.Status = "archived"
	case v.OutstandingMinor == 0:
		v.Status = "paid"
	case v.PaidMinor > 0:
		v.Status = "partial"
	case invoiceCovered(s, invoice):
		v.Status = "unmatched"
	default:
		v.Status = "unreviewed"
	}
	return v
}
func workspaceView(s state, period, counterpartyID string) Workspace {
	v := Workspace{Seller: s.Seller, Counterparties: []CounterpartyView{}, Invoices: []InvoiceView{}, Payments: []PaymentView{}, Bank: s.Bank, Periods: []string{}, NextInvoiceNumber: nextNumber(s)}
	periods := map[string]bool{}
	parties := map[string]int{}
	for _, party := range s.Counterparties {
		parties[party.ID] = len(v.Counterparties)
		v.Counterparties = append(v.Counterparties, CounterpartyView{Counterparty: party})
	}
	covered := true
	for _, invoice := range s.Invoices {
		periods[invoice.Period] = true
		if period != "" && period != "all" && invoice.Period != period || counterpartyID != "" && invoice.CounterpartyID != counterpartyID {
			continue
		}
		iv := viewInvoice(s, invoice)
		v.Invoices = append(v.Invoices, iv)
		if !invoice.IsCurrent {
			continue
		}
		v.Summary.InvoicedMinor += invoice.AmountMinor
		v.Summary.PaidMinor += iv.PaidMinor
		v.Summary.OutstandingMinor += iv.OutstandingMinor
		v.Summary.InvoiceCount++
		if !invoiceCovered(s, invoice) || invoice.ProviderState == "unknown" || invoice.ProviderState == "pending" {
			covered = false
		}
		switch iv.Status {
		case "paid":
			v.Summary.PaidInvoiceCount++
		case "partial":
			v.Summary.PartialInvoiceCount++
		case "unreviewed":
			v.Summary.UnreviewedInvoiceCount++
		}
		if index, ok := parties[invoice.CounterpartyID]; ok {
			p := &v.Counterparties[index]
			p.InvoicedMinor += invoice.AmountMinor
			p.PaidMinor += iv.PaidMinor
			p.OutstandingMinor += iv.OutstandingMinor
			p.InvoiceCount++
			if iv.Status == "unreviewed" {
				p.UnreviewedCount++
			}
		}
	}
	for _, payment := range s.Payments {
		if counterpartyID != "" && payment.CounterpartyID != counterpartyID {
			continue
		}
		pv := PaymentView{Payment: payment, Allocations: []Allocation{}}
		for _, allocation := range s.Allocations {
			if allocation.PaymentID == payment.ID {
				pv.AllocatedMinor += allocation.AmountMinor
				pv.Allocations = append(pv.Allocations, allocation)
			}
		}
		pv.UnallocatedMinor = payment.AmountMinor - pv.AllocatedMinor
		v.Payments = append(v.Payments, pv)
		inCalendarPeriod := period == "" || period == "all" || payment.Date[:7] == period
		if inCalendarPeriod {
			v.Summary.ReceivedMinor += payment.AmountMinor
		}
		v.Summary.UnallocatedMinor += pv.UnallocatedMinor
		if index, ok := parties[payment.CounterpartyID]; ok {
			p := &v.Counterparties[index]
			if inCalendarPeriod {
				p.ReceivedMinor += payment.AmountMinor
			}
			if payment.Date > p.LastPaymentAt {
				p.LastPaymentAt = payment.Date
			}
		}
	}
	for _, party := range v.Counterparties {
		if counterpartyID == "" || party.ID == counterpartyID {
			v.Summary.CounterpartyCount++
		}
	}
	v.Summary.ReconciliationComplete = covered && v.Summary.InvoiceCount > 0 && v.Summary.UnallocatedMinor == 0
	for period := range periods {
		v.Periods = append(v.Periods, period)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(v.Periods)))
	sort.SliceStable(v.Invoices, func(i, j int) bool {
		if v.Invoices[i].IssueDate == v.Invoices[j].IssueDate {
			return v.Invoices[i].Number > v.Invoices[j].Number
		}
		return v.Invoices[i].IssueDate > v.Invoices[j].IssueDate
	})
	sort.SliceStable(v.Payments, func(i, j int) bool { return v.Payments[i].Date > v.Payments[j].Date })
	return v
}
