package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/business"
)

type businessRoutesService struct {
	BusinessService
	owner, period, party, fileID, token string
	imported                            business.ImportRequest
}

func (s *businessRoutesService) Workspace(_ context.Context, owner, period, party string) (business.Workspace, error) {
	s.owner, s.period, s.party = owner, period, party
	return business.Workspace{Invoices: []business.InvoiceView{}, Payments: []business.PaymentView{}, Counterparties: []business.CounterpartyView{}, Periods: []string{}}, nil
}
func (s *businessRoutesService) File(_ context.Context, owner, id string) (business.File, error) {
	s.owner, s.fileID = owner, id
	return business.File{Document: business.Document{ID: id, Filename: "Счёт № 1.pdf", MIMEType: "application/pdf"}, Data: []byte("%PDF-synthetic")}, nil
}
func (s *businessRoutesService) Connect(_ context.Context, owner, token string) (business.Bank, error) {
	s.owner, s.token = owner, token
	return business.Bank{Connected: true, AccountCount: 2}, nil
}
func (s *businessRoutesService) Import(_ context.Context, owner string, body business.ImportRequest) (business.ImportResult, error) {
	s.owner, s.imported = owner, body
	return business.ImportResult{InvoiceCount: len(body.Invoices)}, nil
}
func TestBusinessAllRoutesRequireReadyAdmin(t *testing.T) {
	paths := []struct{ method, path string }{{"GET", ""}, {"POST", "/invoices"}, {"GET", "/files/private-id"}, {"POST", "/allocations"}, {"DELETE", "/allocations/private-id"}, {"POST", "/bank/connect"}, {"DELETE", "/bank/connect"}, {"POST", "/bank/sync"}, {"POST", "/import"}}
	for _, route := range paths {
		for _, token := range []string{"", "user", "bootstrap-admin"} {
			t.Run(route.method+route.path+token, func(t *testing.T) {
				router := NewRouter(Dependencies{Auth: fakeAuth{}, Business: &businessRoutesService{}})
				request := httptest.NewRequest(route.method, "/api/admin/business"+route.path, strings.NewReader(`{}`))
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := http.StatusForbidden
				if token == "" {
					want = http.StatusUnauthorized
				}
				if response.Code != want {
					t.Fatalf("status %d, want %d", response.Code, want)
				}
			})
		}
	}
}
func TestBusinessRoutesScopeToAuthenticatedOwnerAndKeepFilesPrivate(t *testing.T) {
	service := &businessRoutesService{}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Business: service})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, wireGuardAdminRequest(http.MethodGet, "/api/admin/business?period=2026-09&counterpartyId=payer", ""))
	if response.Code != 200 || service.owner != "admin" || service.period != "2026-09" || service.party != "payer" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("workspace scope = %d %#v", response.Code, service)
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, wireGuardAdminRequest(http.MethodGet, "/api/admin/business/files/private-id", ""))
	if response.Code != 200 || service.owner != "admin" || service.fileID != "private-id" || response.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") || strings.ContainsAny(response.Header().Get("Content-Disposition"), "\r\n") || response.Header().Get("Cache-Control") != "no-store" || response.Body.String() != "%PDF-synthetic" {
		t.Fatalf("download = %d %v", response.Code, response.Header())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, wireGuardAdminRequest(http.MethodPost, "/api/admin/business/bank/connect", `{"token":"synthetic-private-token"}`))
	if response.Code != 200 || service.token != "synthetic-private-token" || strings.Contains(response.Body.String(), "synthetic-private-token") {
		t.Fatalf("connect = %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, wireGuardAdminRequest(http.MethodPost, "/api/admin/business/import", `{"seller":{"name":"Продавец"},"counterparties":[],"invoices":[],"documents":[]}`))
	var result business.ImportResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || service.owner != "admin" || service.imported.Seller.Name != "Продавец" {
		t.Fatalf("import = %d %s %v", response.Code, response.Body.String(), err)
	}
}
func TestBusinessImportRejectsMalformedOrTrailingJSON(t *testing.T) {
	for _, body := range []string{`{`, `{} {}`, `{"documents":[{"dataBase64":123}]}`} {
		response := httptest.NewRecorder()
		NewRouter(Dependencies{Auth: fakeAuth{}, Business: &businessRoutesService{}}).ServeHTTP(response, wireGuardAdminRequest(http.MethodPost, "/api/admin/business/import", body))
		if response.Code != 400 {
			t.Fatalf("malformed import = %d %s", response.Code, response.Body.String())
		}
	}
}

func TestBusinessFileDecodesEncodedColonID(t *testing.T) {
	service := &businessRoutesService{}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Business: service})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, wireGuardAdminRequest(http.MethodGet, "/api/admin/business/files/doc%3Abs%3A012345abcdef", ""))
	if response.Code != http.StatusOK || service.fileID != "doc:bs:012345abcdef" || service.owner != "admin" || response.Body.String() != "%PDF-synthetic" {
		t.Fatalf("encoded private download = %d id %q owner %q body %q", response.Code, service.fileID, service.owner, response.Body.String())
	}
}

func TestBusinessFileKeepsMalformedAndUnsafeIDsRejected(t *testing.T) {
	service, err := business.NewService(nil, strings.Repeat("s", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Business: service})
	for _, id := range []string{"private%2Fsecret", "private%5Csecret", "private%00secret", "private%0Asecret"} {
		t.Run(id, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, wireGuardAdminRequest(http.MethodGet, "/api/admin/business/files/"+id, ""))
			if response.Code != http.StatusNotFound {
				t.Fatalf("unsafe id = %d %s", response.Code, response.Body.String())
			}
		})
	}
	response := httptest.NewRecorder()
	request := wireGuardAdminRequest(http.MethodGet, "/api/admin/business/files/invalid", "")
	request.URL.Path = "/api/admin/business/files/invalid%ZZ"
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed escape = %d", response.Code)
	}
}
