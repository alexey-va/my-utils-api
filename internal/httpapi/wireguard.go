package httpapi

import (
	"net/http"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/go-chi/chi/v5"
)

func (a *API) registerWireGuardAdminRoutes(router chi.Router) {
	if a.wireGuard == nil {
		return
	}
	router.Route("/api/admin/wireguard/relays", func(routes chi.Router) {
		routes.Get("/", a.listWireGuardRelays)
		routes.Post("/", a.createWireGuardRelay)
		routes.Post("/{relayId}/rotate-token", a.rotateWireGuardToken)
		routes.Delete("/{relayId}", a.deleteWireGuardRelay)
		routes.Get("/{relayId}/peers", a.listWireGuardPeers)
		routes.Post("/{relayId}/peers", a.createWireGuardPeer)
		routes.Get("/{relayId}/peers/{peerId}/credentials", a.wireGuardCredentials)
		routes.Get("/{relayId}/peers/{peerId}/metrics", a.wireGuardMetrics)
		routes.Patch("/{relayId}/peers/{peerId}", a.updateWireGuardPeer)
		routes.Delete("/{relayId}/peers/{peerId}", a.deleteWireGuardPeer)
	})
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
func (a *API) listWireGuardPeers(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	v, e := a.wireGuard.ListPeers(r.Context(), chi.URLParam(r, "relayId"), r.URL.Query().Get("range"))
	writeDomainResult(w, v, e)
}
func (a *API) createWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var b struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.CreatePeer(r.Context(), chi.URLParam(r, "relayId"), b.Name)
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
	var b struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	v, e := a.wireGuard.UpdatePeer(r.Context(), chi.URLParam(r, "relayId"), chi.URLParam(r, "peerId"), b.Enabled)
	writeDomainResult(w, v, e)
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
