package businessbot

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

func assistantFixture() business.Workspace {
	item := business.LineItem{Description: "fixture-private-item", Quantity: "1", Unit: "усл.", PriceMinor: 12345, AmountMinor: 12345}
	return business.Workspace{
		Seller: business.Seller{Name: "fixture-private-seller", BankAccount: "fixture-private-account"},
		Counterparties: []business.CounterpartyView{
			{Counterparty: business.Counterparty{
				ID: "alpha", Name: "Альфа", INN: "fixture-private-inn", Address: "fixture-private-address",
				Contacts:  []business.Contact{{Email: "fixture-private-contact"}},
				Templates: []business.InvoiceTemplate{{ID: "rent", Label: "Аренда", Basis: "fixture-private-basis", Items: []business.LineItem{item}}},
			}},
			{Counterparty: business.Counterparty{ID: "beta", Name: "Бета", Templates: []business.InvoiceTemplate{{ID: "other", Label: "Бета аренда", Items: []business.LineItem{item}}}}},
		},
		Invoices: []business.InvoiceView{{Invoice: business.Invoice{
			ID: "fixture-private-invoice", CounterpartyID: "alpha", TemplateID: "rent", Period: "2026-09", IsCurrent: true,
			Documents: []business.Document{{ID: "fixture-private-document"}},
		}}},
		Payments: []business.PaymentView{{Payment: business.Payment{Description: "fixture-private-payment"}}},
		Bank:     business.Bank{Error: "fixture-private-bank"},
		Summary:  business.Summary{InvoicedMinor: 12345, PaidMinor: 1000, ReceivedMinor: 2000, OutstandingMinor: 11345, InvoiceCount: 1},
	}
}

func assistantCall(id, name, arguments string) openrouter.Response {
	return openrouter.Response{Message: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{
		ID: id, Type: "function", Function: openrouter.ToolFunction{Name: name, Arguments: arguments},
	}}}}
}

func TestAssistantProjectsOnlyAllowedFieldsAndUsesCanonicalInvoiceTemplate(t *testing.T) {
	workspace := assistantFixture()
	var reads [][3]string
	var requests []openrouter.Request
	a := Assistant{
		workspace: func(_ context.Context, owner, period, party string) (business.Workspace, error) {
			reads = append(reads, [3]string{owner, period, party})
			return workspace, nil
		},
		complete: func(_ context.Context, request openrouter.Request) (openrouter.Response, error) {
			requests = append(requests, request)
			switch len(requests) {
			case 1:
				return assistantCall("parties", "get_business_catalog", `{"query":"альфа"}`), nil
			case 2:
				response := assistantCall("templates", "get_business_catalog", `{"counterparty_id":"alpha"}`)
				balance := assistantCall("totals", "get_monthly_balance", `{"counterparty_id":"alpha","period":"2026-10"}`)
				response.Message.ToolCalls = append(response.Message.ToolCalls, balance.Message.ToolCalls...)
				return response, nil
			case 3:
				return assistantCall("draft", "prepare_document_draft", `{"kind":"invoice","counterparty_id":"alpha","template_id":"rent","period":"2026-10","issue_date":"2026-10-01"}`), nil
			default:
				t.Fatal("unexpected model call")
				return openrouter.Response{}, nil
			}
		},
	}
	history := []openrouter.Message{
		{Role: "system", Content: "fixture-private-system"},
		{Role: "tool", Content: "fixture-private-history"},
		{Role: "user", Content: "Плательщик Альфа"},
	}
	result, err := a.Answer(context.Background(), "owner-a", history, "Подготовь счёт за октябрь 2026 от 1 октября.")
	if err != nil || result.Draft == nil || result.Draft.InvoiceRequest == nil {
		t.Fatalf("draft = %#v, err = %v", result, err)
	}
	draft := result.Draft
	invoice := draft.InvoiceRequest
	template := workspace.Counterparties[0].Templates[0]
	if draft.Kind != "invoice" || draft.CounterpartyName != "Альфа" || draft.TemplateLabel != "Аренда" ||
		invoice.CounterpartyID != "alpha" || invoice.TemplateID != "rent" || invoice.Period != "2026-10" ||
		invoice.IssueDate != "2026-10-01" || invoice.Provider != "local" || invoice.Basis == nil ||
		*invoice.Basis != template.Basis || !reflect.DeepEqual(invoice.Items, template.Items) {
		t.Fatalf("unexpected draft %#v and invoice %#v", draft, invoice)
	}
	invoice.Items[0].Description = "changed draft"
	if workspace.Counterparties[0].Templates[0].Items[0].Description == "changed draft" {
		t.Fatal("draft shares template items with workspace")
	}
	if !strings.Contains(result.Reply, "123,45 ₽") || strings.Contains(result.Reply, "fixture-private-") {
		t.Fatalf("reply = %q", result.Reply)
	}
	wantReads := [][3]string{{"owner-a", "", ""}, {"owner-a", "", ""}, {"owner-a", "2026-10", "alpha"}, {"owner-a", "", ""}}
	if !reflect.DeepEqual(reads, wantReads) {
		t.Fatalf("workspace scope = %v, want %v", reads, wantReads)
	}
	for _, request := range requests {
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "fixture-private-") || strings.Contains(string(raw), "owner-a") {
			t.Fatal("private data entered a model request")
		}
		if request.Model != "openai/gpt-6.1-sol" || len(request.Tools) != 3 || request.ToolChoice != "auto" {
			t.Fatalf("model config = %q, tools = %d, tool choice = %v", request.Model, len(request.Tools), request.ToolChoice)
		}
	}
	last := requests[2].Messages
	if m := last[len(last)-2]; m.Role != "tool" || m.ToolCallID != "templates" || m.Content != `{"choices":[{"id":"rent","name":"Аренда"}]}` {
		t.Fatalf("template projection = %#v", m)
	}
	if m := last[len(last)-1]; m.Role != "tool" || m.ToolCallID != "totals" || !strings.Contains(m.Content.(string), `"paidMinor":1000`) {
		t.Fatalf("monthly balance projection = %#v", m)
	}
}

func TestAssistantDraftValidation(t *testing.T) {
	for _, test := range []struct {
		name, function, arguments, problem, kind, replyFragment string
	}{
		{"mutation unavailable", "create_invoice", `{}`, "Инструмент недоступен", "", ""},
		{"owner override unavailable", "prepare_document_draft", `{"kind":"reconciliation","counterparty_id":"alpha","from":"2026-09-01","to":"2026-09-30","owner":"other"}`, "Неверные параметры черновика", "", ""},
		{"party missing", "prepare_document_draft", `{"kind":"invoice","template_id":"rent","period":"2026-10"}`, "Уточните плательщика", "", ""},
		{"month missing", "prepare_document_draft", `{"kind":"invoice","counterparty_id":"alpha","template_id":"rent"}`, "Уточните месяц и год", "", ""},
		{"foreign template", "prepare_document_draft", `{"kind":"invoice","counterparty_id":"alpha","template_id":"other","period":"2026-10"}`, "нет такого шаблона", "", ""},
		{"redeliver current invoice", "prepare_document_draft", `{"kind":"invoice","counterparty_id":"alpha","template_id":"rent","period":"2026-09"}`, "", "invoice", "Подтвердите получение PDF"},
		{"act requires invoice", "prepare_document_draft", `{"kind":"act","counterparty_id":"alpha","template_id":"rent","period":"2026-10"}`, "нужен действующий счёт", "", ""},
		{"act from invoice", "prepare_document_draft", `{"kind":"act","counterparty_id":"alpha","template_id":"rent","period":"2026-09"}`, "", "act", "неподписанный PDF"},
		{"contract records intent", "prepare_document_draft", `{"kind":"contract","counterparty_id":"alpha","template_id":"rent","period":"2026-09"}`, "", "contract", "Генерация файла договора пока недоступна"},
		{"reconciliation date invalid", "prepare_document_draft", `{"kind":"reconciliation","counterparty_id":"alpha","from":"2026-02-29","to":"2026-09-30"}`, "начальную и конечную даты", "", ""},
		{"reconciliation reversed", "prepare_document_draft", `{"kind":"reconciliation","counterparty_id":"alpha","from":"2026-10-01","to":"2026-09-30"}`, "начальную и конечную даты", "", ""},
		{"reconciliation valid", "prepare_document_draft", `{"kind":"reconciliation","counterparty_id":"alpha","from":"2026-09-01","to":"2026-09-30"}`, "", "reconciliation", "с 2026-09-01 по 2026-09-30"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			a := Assistant{
				workspace: func(_ context.Context, owner, _, _ string) (business.Workspace, error) {
					if owner != "owner-a" {
						t.Fatalf("workspace called for %q", owner)
					}
					return assistantFixture(), nil
				},
				complete: func(_ context.Context, request openrouter.Request) (openrouter.Response, error) {
					calls++
					if calls == 1 {
						return assistantCall("test", test.function, test.arguments), nil
					}
					message := request.Messages[len(request.Messages)-1]
					if calls != 2 || test.problem == "" || message.Role != "tool" || message.ToolCallID != "test" ||
						!strings.Contains(message.Content.(string), test.problem) {
						t.Fatalf("unexpected clarification input %#v", message)
					}
					return openrouter.Response{Message: openrouter.Message{Role: "assistant", Content: "Уточните параметры."}}, nil
				},
			}
			result, err := a.Answer(context.Background(), "owner-a", nil, "Подготовь документ для Альфы.")
			if err != nil {
				t.Fatal(err)
			}
			if test.kind == "" {
				if result.Draft != nil || calls != 2 || result.Reply != "Уточните параметры." {
					t.Fatalf("unexpected result = %#v, model calls = %d", result, calls)
				}
				return
			}
			if result.Draft == nil || result.Draft.Kind != test.kind || calls != 1 ||
				!strings.Contains(result.Reply, test.replyFragment) {
				t.Fatalf("unexpected result = %#v, model calls = %d", result, calls)
			}
			if test.kind == "invoice" {
				locator := result.Draft.InvoiceRequest
				if result.Draft.ExistingInvoiceID != "fixture-private-invoice" || locator == nil || locator.CounterpartyID != "alpha" ||
					locator.TemplateID != "rent" || locator.Period != "2026-09" || locator.IssueDate != "" || len(locator.Items) != 0 {
					t.Fatalf("existing invoice draft = %#v", result.Draft)
				}
			} else if result.Draft.InvoiceRequest != nil {
				t.Fatalf("unexpected invoice request for %s", test.kind)
			}
			if test.kind == "reconciliation" && (result.Draft.From != "2026-09-01" || result.Draft.To != "2026-09-30" || result.Draft.TemplateID != "") {
				t.Fatalf("reconciliation draft = %#v", result.Draft)
			}
		})
	}
}
