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
			if payment, ok := findPayment(s, allocation.PaymentID); ok && ownPayment(s, payment) {
				continue
			}
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

func paymentViews(s state) []PaymentView {
	payments := append([]Payment{}, s.Payments...)
	sort.SliceStable(payments, func(i, j int) bool {
		if payments[i].Date == payments[j].Date {
			return payments[i].ID < payments[j].ID
		}
		return payments[i].Date < payments[j].Date
	})
	remainingNeed := map[string]int64{}
	for _, invoice := range s.Invoices {
		if invoice.IsCurrent {
			remainingNeed[invoice.ID] = viewInvoice(s, invoice).OutstandingMinor
		}
	}
	views := []PaymentView{}
	for _, payment := range payments {
		if ownPayment(s, payment) {
			continue
		}
		matched := matchPayment(s, payment)
		pv := PaymentView{Payment: payment, Classification: matched.classification, Allocations: []Allocation{}}
		for _, allocation := range s.Allocations {
			if allocation.PaymentID == payment.ID {
				pv.AllocatedMinor += allocation.AmountMinor
				pv.Allocations = append(pv.Allocations, allocation)
			}
		}
		if pv.Classification != "historical" && pv.Classification != "outside_period" {
			pv.UnallocatedMinor = payment.AmountMinor - pv.AllocatedMinor
		}
		if pv.Classification == "rental" {
			available := pv.UnallocatedMinor
			for _, invoice := range matched.targets {
				reserved := min(available, remainingNeed[invoice.ID])
				available -= reserved
				remainingNeed[invoice.ID] -= reserved
			}
			if matched.canAdvance {
				pv.AdvanceMinor = available
			}
		}
		views = append(views, pv)
	}
	linkHistoricalInvoices(s, views)
	return views
}

// Attach evidence to historical credits without creating current allocations.
// A dated reference and exact total across its payments distinguish, for example,
// a monthly original from a cumulative document with the same invoice number.
func linkHistoricalInvoices(s state, payments []PaymentView) {
	type key struct {
		inn string
		invoiceReference
	}
	totals := map[key]int64{}
	groups := map[key][]int{}
	for i, payment := range payments {
		if payment.Classification != "historical" || payment.PayerINN == "" {
			continue
		}
		ref := referenceIn(payment.Description)
		if ref.number != "" {
			k := key{payment.PayerINN, ref}
			totals[k] = min(maxMinor+1, totals[k]+payment.AmountMinor)
			groups[k] = append(groups[k], i)
		}
	}
	matches := map[key][]Invoice{}
	for _, invoice := range s.Invoices {
		party, ok := findParty(s, invoice.CounterpartyID)
		if !ok || party.INN == "" {
			continue
		}
		k := key{party.INN, invoiceReference{invoice.Number, invoice.IssueDate}}
		if totals[k] == invoice.AmountMinor {
			matches[k] = append(matches[k], invoice)
		}
	}
	for k, candidates := range matches {
		if len(candidates) != 1 || candidates[0].IsCurrent {
			continue
		}
		for _, index := range groups[k] {
			payment := &payments[index]
			if payment.CounterpartyID == "" || payment.CounterpartyID == candidates[0].CounterpartyID {
				payment.HistoricalInvoiceID = candidates[0].ID
			}
		}
	}
}

func workspaceView(s state, period, counterpartyID string) Workspace {
	v := Workspace{Seller: s.Seller, Counterparties: []CounterpartyView{}, Invoices: []InvoiceView{}, Payments: []PaymentView{}, Bank: s.Bank, Periods: []string{}, AccountingPeriods: accountingPeriods(s), NextInvoiceNumber: nextNumber(s)}
	periods := map[string]bool{}
	accountingMonths := map[string]bool{}
	for _, p := range v.AccountingPeriods {
		accountingMonths[p] = true
	}
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
	for _, pv := range paymentViews(s) {
		payment := pv.Payment
		if counterpartyID != "" && payment.CounterpartyID != counterpartyID {
			continue
		}
		v.Payments = append(v.Payments, pv)
		inCalendarPeriod := accountingMonths[payment.Date[:7]]
		if period != "" && period != "all" {
			inCalendarPeriod = payment.Date[:7] == period
		}
		if inCalendarPeriod {
			if pv.Classification == "historical" || pv.Classification == "outside_period" {
				v.Summary.HistoricalReceivedMinor += payment.AmountMinor
			} else {
				v.Summary.ReceivedMinor += payment.AmountMinor
			}
		}
		v.Summary.UnallocatedMinor += pv.UnallocatedMinor
		v.Summary.AdvanceMinor += pv.AdvanceMinor
		if index, ok := parties[payment.CounterpartyID]; ok {
			p := &v.Counterparties[index]
			p.AdvanceMinor += pv.AdvanceMinor
			currentReceipt := pv.Classification != "historical" && pv.Classification != "outside_period"
			if inCalendarPeriod && currentReceipt {
				p.ReceivedMinor += payment.AmountMinor
			}
			if currentReceipt && payment.Date > p.LastPaymentAt {
				p.LastPaymentAt = payment.Date
			}
		}
	}
	for _, party := range v.Counterparties {
		if counterpartyID == "" || party.ID == counterpartyID {
			v.Summary.CounterpartyCount++
		}
	}
	v.Summary.ReconciliationComplete = covered && v.Summary.InvoiceCount > 0 && v.Summary.UnallocatedMinor-v.Summary.AdvanceMinor == 0
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
