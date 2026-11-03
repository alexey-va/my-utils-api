package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/go-chi/chi/v5"
)

type BusinessService interface {
	Workspace(context.Context, string, string, string) (business.Workspace, error)
	CreateInvoice(context.Context, string, business.CreateInvoiceRequest) (business.InvoiceView, error)
	File(context.Context, string, string) (business.File, error)
	Allocate(context.Context, string, business.AllocateRequest) (business.Allocation, error)
	ReverseAllocation(context.Context, string, string) error
	Connect(context.Context, string, string) (business.Bank, error)
	Disconnect(context.Context, string) error
	Sync(context.Context, string, string, string) (business.Bank, error)
	Import(context.Context, string, business.ImportRequest) (business.ImportResult, error)
}

func (a *API) registerBusinessRoutes(router chi.Router) {
	if a.business == nil {
		return
	}
	router.Get("/api/admin/business", a.businessWorkspace)
	router.Get("/api/admin/business/", a.businessWorkspace)
	router.Post("/api/admin/business/invoices", a.createBusinessInvoice)
	router.Get("/api/admin/business/files/{fileID}", a.businessFile)
	router.Post("/api/admin/business/allocations", a.createBusinessAllocation)
	router.Delete("/api/admin/business/allocations/{allocationID}", a.reverseBusinessAllocation)
	router.Post("/api/admin/business/bank/connect", a.connectBusinessBank)
	router.Delete("/api/admin/business/bank/connect", a.disconnectBusinessBank)
	router.Post("/api/admin/business/bank/sync", a.syncBusinessBank)
	router.Post("/api/admin/business/import", a.importBusiness)
}
func businessOwner(request *http.Request) string {
	principal, _ := principalFrom(request.Context())
	return principal.User.ID
}
func (a *API) businessWorkspace(w http.ResponseWriter, r *http.Request) {
	result, err := a.business.Workspace(r.Context(), businessOwner(r), r.URL.Query().Get("period"), r.URL.Query().Get("counterpartyId"))
	w.Header().Set("Cache-Control", "no-store")
	writeDomainResult(w, result, err)
}
func (a *API) createBusinessInvoice(w http.ResponseWriter, r *http.Request) {
	var body business.CreateInvoiceRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := a.business.CreateInvoice(r.Context(), businessOwner(r), body)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (a *API) businessFile(w http.ResponseWriter, r *http.Request) {
	id, err := url.PathUnescape(chi.URLParam(r, "fileID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор документа")
		return
	}
	file, err := a.business.File(r.Context(), businessOwner(r), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
}
func (a *API) createBusinessAllocation(w http.ResponseWriter, r *http.Request) {
	var body business.AllocateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := a.business.Allocate(r.Context(), businessOwner(r), body)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (a *API) reverseBusinessAllocation(w http.ResponseWriter, r *http.Request) {
	if err := a.business.ReverseAllocation(r.Context(), businessOwner(r), chi.URLParam(r, "allocationID")); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) connectBusinessBank(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := a.business.Connect(r.Context(), businessOwner(r), body.Token)
	body.Token = ""
	w.Header().Set("Cache-Control", "no-store")
	writeDomainResult(w, result, err)
}
func (a *API) disconnectBusinessBank(w http.ResponseWriter, r *http.Request) {
	if err := a.business.Disconnect(r.Context(), businessOwner(r)); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) syncBusinessBank(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := a.business.Sync(r.Context(), businessOwner(r), body.From, body.To)
	writeDomainResult(w, result, err)
}
func (a *API) importBusiness(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, business.MaxImportBytes)
	var body business.ImportRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(w, http.StatusRequestEntityTooLarge, "Пакет импорта слишком велик")
		} else {
			writeError(w, http.StatusBadRequest, "Неверный пакет импорта")
		}
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(w, http.StatusRequestEntityTooLarge, "Пакет импорта слишком велик")
		} else {
			writeError(w, http.StatusBadRequest, "Неверный пакет импорта")
		}
		return
	}
	result, err := a.business.Import(r.Context(), businessOwner(r), body)
	writeDomainResult(w, result, err)
}
