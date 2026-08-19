package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (a *API) registerAgentAdminRoutes(r chi.Router) {
	if a.agentMemory == nil {
		return
	}
	r.Get("/api/admin/agent-memory/chats", a.agentChats)
	r.Get("/api/admin/agent-memory/chats/{chatId}", a.agentChat)
	r.Get("/api/admin/agent-memory/chats/{chatId}/messages", a.agentMessages)
	r.Post("/api/admin/agent-memory/chats/{chatId}/messages", a.appendAgentMessage)
	r.Post("/api/admin/agent-memory/chats/{chatId}/chat", a.agentTurn)
	r.Post("/api/admin/agent-memory/chats/{chatId}/facts", a.createAgentFact)
	r.Put("/api/admin/agent-memory/facts/{id}", a.updateAgentFact)
	r.Delete("/api/admin/agent-memory/facts/{id}", a.deleteAgentFact)
	r.Delete("/api/admin/agent-memory/summaries/{id}", a.deleteAgentSummary)
	r.Patch("/api/admin/agent-memory/messages/{id}", a.excludeAgentMessage)
	r.Delete("/api/admin/agent-memory/messages/{id}", a.deleteAgentMessage)
	r.Post("/api/admin/agent-memory/chats/{chatId}/compact", a.compactAgentChat)
	r.Delete("/api/admin/agent-memory/chats/{chatId}/dialog", a.clearAgentDialog)
	r.Post("/api/admin/agent-test-chats", a.createTestChat)
	r.Get("/api/admin/agent-test-chats", a.listTestChats)
	r.Get("/api/admin/agent-test-chats/{id}", a.getTestChat)
	r.Patch("/api/admin/agent-test-chats/{id}", a.renameTestChat)
	r.Delete("/api/admin/agent-test-chats/{id}", a.deleteTestChat)
	r.Get("/api/admin/agent-test-chats/{id}/messages", a.testChatMessages)
	r.Post("/api/admin/agent-test-chats/{id}/messages", a.testChatTurn)
	r.Delete("/api/admin/agent-test-chats/{id}/messages", a.clearTestChat)
}
func chatID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "chatId"), 10, 64)
}
func messageID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
func queryPage(r *http.Request) (*int64, int, error) {
	var before *int64
	if raw := r.URL.Query().Get("beforeId"); raw != "" {
		v, e := strconv.ParseInt(raw, 10, 64)
		if e != nil {
			return nil, 0, e
		}
		before = &v
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil {
			return nil, 0, e
		}
		limit = v
	}
	return before, limit, nil
}
func (a *API) agentChats(w http.ResponseWriter, r *http.Request) {
	v, e := a.agentMemory.ListChats(r.Context())
	writeDomainResult(w, v, e)
}
func (a *API) agentChat(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	v, e := a.agentMemory.Detail(r.Context(), id)
	writeDomainResult(w, v, e)
}
func (a *API) agentMessages(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	before, limit, e := queryPage(r)
	if e != nil {
		writeError(w, 400, "Invalid pagination")
		return
	}
	v, e := a.agentMemory.Messages(r.Context(), id, before, limit)
	writeDomainResult(w, v, e)
}

type agentPayload struct {
	Content string   `json:"content"`
	Images  []string `json:"images"`
}

func (a *API) appendAgentMessage(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	var b struct {
		Role    string   `json:"role"`
		Content string   `json:"content"`
		Images  []string `json:"images"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.AppendManual(r.Context(), id, b.Role, b.Content, b.Images)
	writeDomainResult(w, v, e)
}
func (a *API) agentTurn(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	var b agentPayload
	if !decodeJSON(w, r, &b) {
		return
	}
	if strings.TrimSpace(b.Content) == "" && len(b.Images) == 0 {
		writeError(w, 400, "Нужен текст или хотя бы одно изображение.")
		return
	}
	v, e := a.agentMemory.Turn(r.Context(), id, b.Content, b.Images, false)
	writeDomainResult(w, v, e)
}

type factPayload struct {
	Content    string   `json:"content"`
	Confidence *float64 `json:"confidence"`
}

func (a *API) createAgentFact(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	var b factPayload
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.CreateFact(r.Context(), id, b.Content, b.Confidence)
	writeDomainResult(w, v, e)
}
func (a *API) updateAgentFact(w http.ResponseWriter, r *http.Request) {
	var b factPayload
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.UpdateFact(r.Context(), chi.URLParam(r, "id"), b.Content, b.Confidence)
	writeDomainResult(w, v, e)
}
func (a *API) deleteAgentFact(w http.ResponseWriter, r *http.Request) {
	if e := a.agentMemory.DeleteFact(r.Context(), chi.URLParam(r, "id")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(200)
}
func (a *API) deleteAgentSummary(w http.ResponseWriter, r *http.Request) {
	if e := a.agentMemory.DeleteSummary(r.Context(), chi.URLParam(r, "id")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(200)
}
func (a *API) excludeAgentMessage(w http.ResponseWriter, r *http.Request) {
	id, e := messageID(r)
	if e != nil {
		writeError(w, 400, "Invalid message id")
		return
	}
	var b struct {
		Excluded bool `json:"excludedFromContext"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.ExcludeMessage(r.Context(), id, b.Excluded)
	writeDomainResult(w, v, e)
}
func (a *API) deleteAgentMessage(w http.ResponseWriter, r *http.Request) {
	id, e := messageID(r)
	if e != nil {
		writeError(w, 400, "Invalid message id")
		return
	}
	if e := a.agentMemory.DeleteMessage(r.Context(), id); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(200)
}
func (a *API) compactAgentChat(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	keep, _ := strconv.Atoi(r.URL.Query().Get("keepRecent"))
	v, e := a.agentMemory.Compact(r.Context(), id, keep)
	writeDomainResult(w, v, e)
}
func (a *API) clearAgentDialog(w http.ResponseWriter, r *http.Request) {
	id, e := chatID(r)
	if e != nil {
		writeError(w, 400, "Invalid chatId")
		return
	}
	if e := a.agentMemory.ClearDialog(r.Context(), id); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(200)
}

type titlePayload struct {
	Title string `json:"title"`
}

func (a *API) createTestChat(w http.ResponseWriter, r *http.Request) {
	var b titlePayload
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.CreateTestChat(r.Context(), b.Title)
	if e != nil {
		writeDomainError(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (a *API) listTestChats(w http.ResponseWriter, r *http.Request) {
	v, e := a.agentMemory.ListTestChats(r.Context())
	writeDomainResult(w, v, e)
}
func (a *API) getTestChat(w http.ResponseWriter, r *http.Request) {
	v, e := a.agentMemory.TestChat(r.Context(), chi.URLParam(r, "id"))
	writeDomainResult(w, v, e)
}
func (a *API) renameTestChat(w http.ResponseWriter, r *http.Request) {
	var b titlePayload
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.RenameTestChat(r.Context(), chi.URLParam(r, "id"), b.Title)
	writeDomainResult(w, v, e)
}
func (a *API) deleteTestChat(w http.ResponseWriter, r *http.Request) {
	if e := a.agentMemory.DeleteTestChat(r.Context(), chi.URLParam(r, "id")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (a *API) testChatMessages(w http.ResponseWriter, r *http.Request) {
	chat, e := a.agentMemory.TestChat(r.Context(), chi.URLParam(r, "id"))
	if e != nil {
		writeDomainError(w, e)
		return
	}
	before, limit, e := queryPage(r)
	if e != nil {
		writeError(w, 400, "Invalid pagination")
		return
	}
	v, e := a.agentMemory.Messages(r.Context(), chat.MemoryChatID, before, limit)
	writeDomainResult(w, v, e)
}
func (a *API) testChatTurn(w http.ResponseWriter, r *http.Request) {
	chat, e := a.agentMemory.TestChat(r.Context(), chi.URLParam(r, "id"))
	if e != nil {
		writeDomainError(w, e)
		return
	}
	var b agentPayload
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.agentMemory.Turn(r.Context(), chat.MemoryChatID, b.Content, b.Images, true)
	writeDomainResult(w, v, e)
}
func (a *API) clearTestChat(w http.ResponseWriter, r *http.Request) {
	if e := a.agentMemory.ClearTestChat(r.Context(), chi.URLParam(r, "id")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(204)
}
