package httpapi

import (
	"net/http"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (a *API) registerWireGuardAdminRoutes(router chi.Router) {
	if a.wireGuard == nil {
		return
	}
	router.Route("/api/admin/wireguard/relays", func(routes chi.Router) {
		routes.Get("/", a.listWireGuardRelays)
		routes.Post("/", a.createWireGuardRelay)
		routes.Post("/{relayId}/rotate-token", validWireGuardUUIDs(a.rotateWireGuardToken, "relayId"))
		routes.Delete("/{relayId}", validWireGuardUUIDs(a.deleteWireGuardRelay, "relayId"))
		routes.Put("/{relayId}/exit-preference", validWireGuardUUIDs(a.updateWireGuardExitPreference, "relayId"))
		routes.Get("/{relayId}/snapshot", validWireGuardUUIDs(a.wireGuardSnapshot, "relayId"))
		routes.Get("/{relayId}/peers", validWireGuardUUIDs(a.listWireGuardPeers, "relayId"))
		routes.Post("/{relayId}/peers", validWireGuardUUIDs(a.createWireGuardPeer, "relayId"))
		routes.Put("/{relayId}/peers/order", validWireGuardUUIDs(a.reorderWireGuardPeers, "relayId"))
		routes.Get("/{relayId}/categories", validWireGuardUUIDs(a.listWireGuardPeerCategories, "relayId"))
		routes.Post("/{relayId}/categories", validWireGuardUUIDs(a.createWireGuardPeerCategory, "relayId"))
		routes.Put("/{relayId}/categories/order", validWireGuardUUIDs(a.reorderWireGuardPeerCategories, "relayId"))
		routes.Patch("/{relayId}/categories/{categoryId}", validWireGuardUUIDs(a.updateWireGuardPeerCategory, "relayId", "categoryId"))
		routes.Delete("/{relayId}/categories/{categoryId}", validWireGuardUUIDs(a.deleteWireGuardPeerCategory, "relayId", "categoryId"))
		routes.Get("/{relayId}/peers/{peerId}/credentials", validWireGuardUUIDs(a.wireGuardCredentials, "relayId", "peerId"))
		routes.Get("/{relayId}/peers/{peerId}/metrics", validWireGuardUUIDs(a.wireGuardMetrics, "relayId", "peerId"))
		routes.Patch("/{relayId}/peers/{peerId}", validWireGuardUUIDs(a.updateWireGuardPeer, "relayId", "peerId"))
		routes.Delete("/{relayId}/peers/{peerId}", validWireGuardUUIDs(a.deleteWireGuardPeer, "relayId", "peerId"))
	})
}

func validWireGuardUUIDs(next http.HandlerFunc, params ...string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		for _, param := range params {
			value := chi.URLParam(request, param)
			parsed, err := uuid.Parse(value)
			if err != nil || !strings.EqualFold(value, parsed.String()) {
				writeError(response, http.StatusBadRequest, "Invalid WireGuard identifier")
				return
			}
		}
		next(response, request)
	}
}
func (a *API) updateWireGuardExitPreference(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b wireguard.UpdateExitPreferenceRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.UpdateExitPreference(r.Context(), chi.URLParam(r, "relayId"), b)
	writeDomainResult(w, v, e)
}

func (a *API) registerWireGuardAgentRoutes(router chi.Router) {
	if a.wireGuard == nil {
		return
	}
	router.Route("/api/internal/wireguard/relays/{relayId}", func(routes chi.Router) {
		routes.Use(a.requireWireGuardAgent)
		routes.Get("/desired", a.wireGuardDesired)
		routes.Post("/heartbeat", a.wireGuardHeartbeat)
	})
}

func (a *API) requireWireGuardAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := strings.TrimSpace(request.Header.Get("X-WireGuard-Agent-Token"))
		if !a.wireGuard.AgentTokenMatches(request.Context(), chi.URLParam(request, "relayId"), token) {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func noStore(response http.ResponseWriter) { response.Header().Set("Cache-Control", "no-store") }
func (a *API) listWireGuardRelays(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.ListRelays(r.Context())
	writeDomainResult(w, v, e)
}
func (a *API) createWireGuardRelay(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b wireguard.CreateRelayRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.CreateRelay(r.Context(), b)
	if e != nil {
		writeDomainError(w, e)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) rotateWireGuardToken(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.RotateToken(r.Context(), chi.URLParam(r, "relayId"))
	writeDomainResult(w, v, e)
}
func (a *API) deleteWireGuardRelay(w http.ResponseWriter, r *http.Request) {
	if e := a.wireGuard.DeleteRelay(r.Context(), chi.URLParam(r, "relayId")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) wireGuardSnapshot(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.Snapshot(r.Context(), chi.URLParam(r, "relayId"), r.URL.Query().Get("range"))
	writeDomainResult(w, v, e)
}
func (a *API) listWireGuardPeers(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.ListPeers(r.Context(), chi.URLParam(r, "relayId"), r.URL.Query().Get("range"))
	writeDomainResult(w, v, e)
}
func (a *API) createWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b wireguard.CreatePeerRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.CreatePeer(r.Context(), chi.URLParam(r, "relayId"), b)
	if e != nil {
		writeDomainError(w, e)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) wireGuardCredentials(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.Credentials(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "peerId"))
	writeDomainResult(w, v, e)
}
func (a *API) wireGuardMetrics(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.Metrics(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "peerId"), r.URL.Query().Get("range"))
	writeDomainResult(w, v, e)
}
func (a *API) updateWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b wireguard.UpdatePeerRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.UpdatePeer(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "peerId"), b)
	writeDomainResult(w, v, e)
}
func (a *API) reorderWireGuardPeers(w http.ResponseWriter, r *http.Request) {
	var b wireguard.UpdatePeerOrderRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	if e := a.wireGuard.ReorderPeers(r.Context(), chi.URLParam(r, "relayId"), b); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) listWireGuardPeerCategories(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.ListPeerCategories(r.Context(), chi.URLParam(r, "relayId"))
	writeDomainResult(w, v, e)
}
func (a *API) createWireGuardPeerCategory(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b wireguard.CreatePeerCategoryRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.CreatePeerCategory(r.Context(), chi.URLParam(r, "relayId"), b)
	if e != nil {
		writeDomainError(w, e)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) updateWireGuardPeerCategory(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b wireguard.UpdatePeerCategoryRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.UpdatePeerCategory(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "categoryId"), b)
	writeDomainResult(w, v, e)
}
func (a *API) reorderWireGuardPeerCategories(w http.ResponseWriter, r *http.Request) {
	var b wireguard.UpdatePeerCategoryOrderRequest
	if !decodeJSON(w, r, &b) {
		return
	}
	if e := a.wireGuard.ReorderPeerCategories(r.Context(), chi.URLParam(r, "relayId"), b); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) deleteWireGuardPeerCategory(w http.ResponseWriter, r *http.Request) {
	if e := a.wireGuard.DeletePeerCategory(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "categoryId")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) deleteWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	if e := a.wireGuard.DeletePeer(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "peerId")); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) wireGuardDesired(w http.ResponseWriter, r *http.Request) {
	v, e := a.wireGuard.Desired(r.Context(), chi.URLParam(r, "relayId"))
	writeDomainResult(w, v, e)
}
func (a *API) wireGuardHeartbeat(w http.ResponseWriter, r *http.Request) {
	var b wireguard.Heartbeat
	if !decodeJSON(w, r, &b) {
		return
	}
	if e := a.wireGuard.Heartbeat(r.Context(), chi.URLParam(r, "relayId"), b); e != nil {
		writeDomainError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
