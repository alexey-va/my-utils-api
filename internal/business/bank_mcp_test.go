package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const mcpTestToken = "synthetic-private-mcp-token"
const mcpAccountRoot = `{"Account":[{"accountId":"40817810802000000008/044525104","customerCode":"300000092","status":"Enabled","currency":"RUB","accountType":"Business"}]}`

type mcpFixture struct {
	t                                           *testing.T
	sse                                         bool
	fault                                       string
	restStatus                                  int
	initializations, terminations, nativeWrites int
	methods, calls                              []string
	session                                     string
}

func (f *mcpFixture) reply(id int, result any) *http.Response {
	f.t.Helper()
	data, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  any    `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		f.t.Fatal(err)
	}
	response := bankResponse(200, string(data))
	response.Header.Set("Content-Type", "application/json")
	if f.sse {
		response = bankResponse(200, ": heartbeat\r\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\r\n\r\nevent: message\r\ndata: "+string(data)+"\r\n\r\n")
		response.Header.Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	return response
}

func (f *mcpFixture) roundTrip(r *http.Request) (*http.Response, error) {
	f.t.Helper()
	if r.URL.Scheme != "https" || r.Header.Get("Authorization") != "Bearer "+mcpTestToken || strings.Contains(r.URL.String(), mcpTestToken) {
		f.t.Fatal("credential/destination boundary changed")
	}
	if r.URL.Host == "enter.tochka.com" {
		if r.URL.Path == "/uapi/invoice/v1.0/bills" {
			f.nativeWrites++
			return bankResponse(403, `private native invoice rejection`), nil
		}
		if f.restStatus == 0 {
			f.restStatus = 403
		}
		return bankResponse(f.restStatus, `private native read rejection`), nil
	}
	if r.URL.Host != "api.tochka.com" || r.URL.Path != "/mcp" {
		f.t.Fatalf("unexpected credential destination %s", r.URL.Redacted())
	}
	if r.Header.Get("Accept") != "application/json, text/event-stream" {
		f.t.Fatal("MCP must accept JSON and SSE")
	}
	if r.Method == http.MethodDelete {
		if r.Header.Get("Mcp-Session-Id") != f.session {
			f.t.Fatal("wrong terminated session")
		}
		f.terminations++
		return bankResponse(204, ""), nil
	}
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
		f.t.Fatal("wrong MCP request method/type")
	}
	var payload struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int            `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		f.t.Fatal(err)
	}
	if payload.JSONRPC != "2.0" {
		f.t.Fatal("wrong JSON-RPC version")
	}
	f.methods = append(f.methods, payload.Method)
	if payload.Method == "initialize" {
		if payload.ID == nil || *payload.ID != 1 || r.Header.Get("MCP-Protocol-Version") != "" || r.Header.Get("Mcp-Session-Id") != "" {
			f.t.Fatal("a private MCP session was reused")
		}
		var params struct {
			Protocol     string                         `json:"protocolVersion"`
			Capabilities map[string]json.RawMessage     `json:"capabilities"`
			ClientInfo   struct{ Name, Version string } `json:"clientInfo"`
		}
		if err := json.Unmarshal(payload.Params, &params); err != nil || params.Protocol != bankMCPProtocol || len(params.Capabilities) != 0 || params.ClientInfo.Name != "my-utils-business-readonly" {
			f.t.Fatalf("bad initialize: %#v %v", params, err)
		}
		f.initializations++
		f.session = fmt.Sprintf("private-session-%d", f.initializations)
		if f.fault == "redirect" {
			response := bankResponse(302, "private redirect")
			response.Header.Set("Location", "https://untrusted.example/mcp")
			return response, nil
		}
		if f.fault == "rpc error" {
			response := bankResponse(200, `{"jsonrpc":"2.0","id":1,"error":{"message":"`+mcpTestToken+`"}}`)
			response.Header.Set("Content-Type", "application/json")
			return response, nil
		}
		protocol := bankMCPProtocol
		if f.fault == "protocol" {
			protocol = "2099-01-01"
		}
		response := f.reply(*payload.ID, struct {
			Protocol string `json:"protocolVersion"`
		}{protocol})
		response.Header.Set("Mcp-Session-Id", f.session)
		return response, nil
	}
	if r.Header.Get("MCP-Protocol-Version") != bankMCPProtocol || r.Header.Get("Mcp-Session-Id") != f.session {
		f.t.Fatal("negotiated protocol/session headers missing")
	}
	if payload.Method == "notifications/initialized" {
		if payload.ID != nil {
			f.t.Fatal("notification must omit id")
		}
		return bankResponse(202, ""), nil
	}
	if payload.ID == nil {
		f.t.Fatal("request missing id")
	}
	switch payload.Method {
	case "tools/list":
		names := []string{mcpAccountsTool, mcpCreateStatementTool, mcpReadStatementTool, "openapi_mcp.createInvoice"}
		if f.fault == "missing read tool" {
			names = []string{mcpAccountsTool}
		}
		var result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor,omitempty"`
		}
		for _, name := range names {
			result.Tools = append(result.Tools, struct {
				Name string `json:"name"`
			}{name})
		}
		if f.fault == "catalog pagination" {
			result.NextCursor = "more"
		}
		return f.reply(*payload.ID, result), nil
	case "tools/call":
		var params struct {
			Name      string                     `json:"name"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(payload.Params, &params); err != nil {
			f.t.Fatal(err)
		}
		f.calls = append(f.calls, params.Name)
		root := mcpAccountRoot
		switch params.Name {
		case mcpAccountsTool:
			if len(params.Arguments) != 0 {
				f.t.Fatal("account call received unexpected arguments")
			}
		case mcpCreateStatementTool:
			if len(params.Arguments) != 3 || string(params.Arguments["accountId"]) != `"40817810802000000008/044525104"` || string(params.Arguments["startDateTime"]) != `"2026-09-01"` || string(params.Arguments["endDateTime"]) != `"2026-09-30"` {
				f.t.Fatalf("createStatement arguments = %#v", params.Arguments)
			}
			root = `{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement-one","status":"Created","startDateTime":"2026-09-01","endDateTime":"2026-09-30"}}`
		case mcpReadStatementTool:
			if len(params.Arguments) != 2 || string(params.Arguments["accountId"]) != `"40817810802000000008/044525104"` || string(params.Arguments["statementId"]) != `"statement-one"` {
				f.t.Fatalf("readStatement arguments = %#v", params.Arguments)
			}
			root = `{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement-one","status":"Ready","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[{"transactionId":"mcp-credit","creditDebitIndicator":"Credit","status":"Booked","documentProcessDate":"2026-09-02","description":"Оплата по счёту № 17 от 01.09.2026","Amount":{"amount":100,"currency":"RUB"},"DebtorParty":{"inn":"7701234567","name":"Тестовый плательщик"}},{"transactionId":"mcp-debit","creditDebitIndicator":"Debit","Amount":{"amount":999,"currency":"RUB"}}]}]}`
		default:
			f.t.Fatalf("non-allowlisted tool called: %s", params.Name)
		}
		if f.fault == "tool malformed" {
			root = "private malformed " + mcpTestToken
		}
		if f.fault == "partial marker" {
			root = `{"Account":[],"Meta":{"totalPages":2}}`
		}
		if f.fault == "account cap" {
			var accounts struct {
				Accounts []BankAccount `json:"Account"`
			}
			for i := 0; i < 21; i++ {
				accounts.Accounts = append(accounts.Accounts, BankAccount{ID: fmt.Sprintf("%020d/044525104", i+1), CustomerCode: "300000092", Status: "Enabled", Currency: "RUB"})
			}
			data, _ := json.Marshal(accounts)
			root = string(data)
		}
		var result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		result.IsError = f.fault == "tool error"
		result.Content = append(result.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{"text", root})
		if f.fault == "tool error" {
			result.Content[0].Text = mcpTestToken
		}
		return f.reply(*payload.ID, result), nil
	default:
		f.t.Fatalf("unexpected MCP method %s", payload.Method)
	}
	return nil, errors.New("unexpected synthetic request")
}

func TestBusinessMCPReadBridgeSupportsJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%t", sse), func(t *testing.T) {
			fixture := &mcpFixture{t: t, sse: sse}
			client := testBankClient(t, fixture.roundTrip)
			accounts, err := client.Accounts(context.Background(), mcpTestToken)
			if err != nil || len(accounts) != 1 {
				t.Fatalf("MCP accounts = %#v %v", accounts, err)
			}
			payments, err := client.Statements(context.Background(), mcpTestToken, accounts, "2026-09-01", "2026-09-30")
			if err != nil || len(payments) != 1 || payments[0].AmountMinor != 10000 || payments[0].AccountLabel != "Точка · 0008" {
				t.Fatalf("MCP credits = %#v %v", payments, err)
			}
			repeated, err := client.Statements(context.Background(), mcpTestToken, accounts, "2026-09-01", "2026-09-30")
			if err != nil || !reflect.DeepEqual(payments, repeated) {
				t.Fatalf("same provider credit changed identity: %#v %v", repeated, err)
			}
			if fixture.initializations != 3 || fixture.terminations != 3 || fixture.nativeWrites != 0 {
				t.Fatalf("session lifecycle = initialized %d closed %d writes %d", fixture.initializations, fixture.terminations, fixture.nativeWrites)
			}
			for _, name := range fixture.calls {
				if name != mcpAccountsTool && name != mcpCreateStatementTool && name != mcpReadStatementTool {
					t.Fatal("unexpected MCP write")
				}
			}
		})
	}
}

func TestBusinessMCPFallbackOnlyHandlesNative401Or403(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fixture := &mcpFixture{t: t, restStatus: status}
			client := testBankClient(t, fixture.roundTrip)
			_, err := client.Accounts(context.Background(), mcpTestToken)
			wantFallback := status == 401 || status == 403
			if (fixture.initializations > 0) != wantFallback || (err == nil) != wantFallback {
				t.Fatalf("native %d: init %d error %v", status, fixture.initializations, err)
			}
		})
	}
}

func TestBusinessMCPRejectsErrorsAndIncompleteDataWithoutSecrets(t *testing.T) {
	for _, fault := range []string{"redirect", "rpc error", "protocol", "missing read tool", "catalog pagination", "tool error", "tool malformed", "partial marker", "account cap"} {
		t.Run(fault, func(t *testing.T) {
			fixture := &mcpFixture{t: t, fault: fault}
			client := testBankClient(t, fixture.roundTrip)
			_, err := client.Accounts(context.Background(), mcpTestToken)
			if err == nil || strings.Contains(err.Error(), mcpTestToken) || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}

func TestBusinessMCPNeverCallsInvoiceOrUnknownTools(t *testing.T) {
	fixture := &mcpFixture{t: t}
	client := testBankClient(t, fixture.roundTrip)
	session, err := client.readonlyMCP(context.Background(), mcpTestToken)
	if err != nil {
		t.Fatal(err)
	}
	session.tools["openapi_mcp.createInvoice"] = true
	var result json.RawMessage
	if err := session.call("openapi_mcp.createInvoice", struct{}{}, &result); err == nil {
		t.Fatal("write tool accepted")
	}
	if err := session.read(http.MethodPost, "/invoice/v1.0/bills", struct{}{}, &result); err == nil {
		t.Fatal("invoice endpoint was routed to MCP")
	}
	if len(fixture.calls) != 0 {
		t.Fatal("a disallowed call reached the bank")
	}
	session.close()
	_, err = client.CreateInvoice(context.Background(), mcpTestToken, fixtureSeller(), fixtureParty(), fixtureInvoice("invoice", "2026-09", true))
	if !rejectedCreate(err) || fixture.nativeWrites != 1 {
		t.Fatalf("native invoice rejection = %v writes %d", err, fixture.nativeWrites)
	}
}

func TestBusinessMCPSSEMatchesResponseIDAndHandlesMultilineData(t *testing.T) {
	stream := ": heartbeat\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"value\":\"other\"}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":3,\ndata: \"result\":{\"value\":\"Счёт\"}}\n\n"
	var result struct {
		Value string `json:"value"`
	}
	if err := readMCPEvent(strings.NewReader(stream), 3, &result); err != nil || result.Value != "Счёт" {
		t.Fatalf("SSE = %#v %v", result, err)
	}
	if err := readMCPEvent(strings.NewReader("data: malformed\n\n"), 3, &result); err == nil {
		t.Fatal("malformed SSE accepted")
	}
	if err := readMCPEvent(strings.NewReader(`data: {"jsonrpc":"2.0","id":4,"result":{}}`), 3, &result); err == nil {
		t.Fatal("wrong response ID accepted")
	}
}

func TestBusinessMCPConnectedSyncRemainsIdempotent(t *testing.T) {
	fixture := &mcpFixture{t: t, sse: true}
	client := testBankClient(t, fixture.roundTrip)
	service, owner := testService(t, client)
	ctx := context.Background()
	request := importFixture()
	request.Payments = nil
	if _, err := service.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Connect(ctx, owner, mcpTestToken); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
			t.Fatal(err)
		}
	}
	view, err := service.Workspace(ctx, owner, "all", "")
	if err != nil || len(view.Payments) != 1 || len(view.Payments[0].Allocations) != 1 {
		t.Fatalf("MCP sync = %#v %v", view.Payments, err)
	}
	if err := service.ReverseAllocation(ctx, owner, view.Payments[0].Allocations[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
		t.Fatal(err)
	}
	view, err = service.Workspace(ctx, owner, "all", "")
	if err != nil || len(view.Payments) != 1 || len(view.Payments[0].Allocations) != 0 || view.Summary.PaidMinor != 0 {
		t.Fatal("MCP replay duplicated credit or restored a reversed allocation")
	}
	if fixture.initializations != fixture.terminations {
		t.Fatal("MCP session lifecycle leaked")
	}
	failed := &mcpFixture{t: t, fault: "tool error"}
	badService, badOwner := testService(t, testBankClient(t, failed.roundTrip))
	if _, err := badService.Connect(ctx, badOwner, mcpTestToken); err == nil {
		t.Fatal("connection persisted before successful MCP account validation")
	}
	if _, err := badService.loadToken(ctx, badOwner); err == nil {
		t.Fatal("failed MCP connection retained a token")
	}
}
