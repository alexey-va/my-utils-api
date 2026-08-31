package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/alexey-va/my-utils-api/internal/workout"
)

const (
	testRelayID    = "00000000-0000-0000-0000-000000000001"
	testPeerID     = "00000000-0000-0000-0000-000000000101"
	testSecondPeer = "00000000-0000-0000-0000-000000000102"
	testCategoryID = "00000000-0000-0000-0000-000000000201"
	testSecondCat  = "00000000-0000-0000-0000-000000000202"
)

// wireGuardRoutesService records the values crossing the HTTP boundary. The
// embedded interface keeps this test double small while the methods under
// contract are explicit below.
type wireGuardRoutesService struct {
	WireGuardService

	relayID, listRelayID, categoryID, peerID string
	categoryName                             string
	categoryOrder                            wireguard.UpdatePeerCategoryOrderRequest
	peerUpdate                               wireguard.UpdatePeerRequest
	peerOrder                                wireguard.UpdatePeerOrderRequest
	categoryErr, peerErr                     error
	category                                 wireguard.PeerCategory
	peer                                     wireguard.Peer
}

func (s *wireGuardRoutesService) ListPeerCategories(_ context.Context, relayID string) ([]wireguard.PeerCategory, error) {
	s.listRelayID = relayID
	return []wireguard.PeerCategory{s.category}, s.categoryErr
}

func (s *wireGuardRoutesService) CreatePeerCategory(_ context.Context, relayID string, body wireguard.CreatePeerCategoryRequest) (wireguard.PeerCategory, error) {
	s.relayID, s.categoryName = relayID, body.Name
	return s.category, s.categoryErr
}

func (s *wireGuardRoutesService) UpdatePeerCategory(_ context.Context, relayID, categoryID string, body wireguard.UpdatePeerCategoryRequest) (wireguard.PeerCategory, error) {
	s.relayID, s.categoryID, s.categoryName = relayID, categoryID, body.Name
	return s.category, s.categoryErr
}

func (s *wireGuardRoutesService) ReorderPeerCategories(_ context.Context, relayID string, body wireguard.UpdatePeerCategoryOrderRequest) error {
	s.relayID, s.categoryOrder = relayID, body
	return s.categoryErr
}

func (s *wireGuardRoutesService) DeletePeerCategory(_ context.Context, relayID, categoryID string) error {
	s.relayID, s.categoryID = relayID, categoryID
	return s.categoryErr
}

func (s *wireGuardRoutesService) UpdatePeer(_ context.Context, relayID, peerID string, body wireguard.UpdatePeerRequest) (wireguard.Peer, error) {
	s.relayID, s.peerID, s.peerUpdate = relayID, peerID, body
	return s.peer, s.peerErr
}

func (s *wireGuardRoutesService) ReorderPeers(_ context.Context, relayID string, body wireguard.UpdatePeerOrderRequest) error {
	s.relayID, s.peerOrder = relayID, body
	return s.peerErr
}

func (s *wireGuardRoutesService) DeletePeer(_ context.Context, relayID, peerID string) error {
	s.relayID, s.peerID = relayID, peerID
	return s.peerErr
}

func wireGuardAdminRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer ready-admin")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func TestWireGuardCategoryRoutesContract(t *testing.T) {
	t.Parallel()
	service := &wireGuardRoutesService{category: wireguard.PeerCategory{ID: testCategoryID, Name: "Personal", SortOrder: 3}}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})

	tests := []struct {
		name, method, path, body string
		wantStatus               int
		check                    func(*testing.T)
	}{
		{
			name: "list", method: http.MethodGet, path: "/api/admin/wireguard/relays/" + testRelayID + "/categories", wantStatus: http.StatusOK,
			check: func(t *testing.T) {
				if service.listRelayID != testRelayID {
					t.Fatalf("list relay id = %q", service.listRelayID)
				}
			},
		},
		{
			name: "create", method: http.MethodPost, path: "/api/admin/wireguard/relays/" + testRelayID + "/categories", body: `{"name":"Personal"}`, wantStatus: http.StatusCreated,
			check: func(t *testing.T) {
				if service.relayID != testRelayID || service.categoryName != "Personal" {
					t.Fatalf("create args = %q, %q", service.relayID, service.categoryName)
				}
			},
		},
		{
			name: "update", method: http.MethodPatch, path: "/api/admin/wireguard/relays/" + testRelayID + "/categories/" + testCategoryID, body: `{"name":"Work"}`, wantStatus: http.StatusOK,
			check: func(t *testing.T) {
				if service.relayID != testRelayID || service.categoryID != testCategoryID || service.categoryName != "Work" {
					t.Fatalf("update args = %q, %q, %q", service.relayID, service.categoryID, service.categoryName)
				}
			},
		},
		{
			name: "order", method: http.MethodPut, path: "/api/admin/wireguard/relays/" + testRelayID + "/categories/order", body: `{"items":[{"categoryId":"` + testSecondCat + `"},{"categoryId":"` + testCategoryID + `"}]}`, wantStatus: http.StatusNoContent,
			check: func(t *testing.T) {
				want := wireguard.UpdatePeerCategoryOrderRequest{Items: []wireguard.PeerCategoryOrderItem{{CategoryID: testSecondCat}, {CategoryID: testCategoryID}}}
				if service.relayID != testRelayID || !reflect.DeepEqual(service.categoryOrder, want) {
					t.Fatalf("order args = %q, %#v", service.relayID, service.categoryOrder)
				}
			},
		},
		{
			name: "delete", method: http.MethodDelete, path: "/api/admin/wireguard/relays/" + testRelayID + "/categories/" + testCategoryID, wantStatus: http.StatusNoContent,
			check: func(t *testing.T) {
				if service.relayID != testRelayID || service.categoryID != testCategoryID {
					t.Fatalf("delete args = %q, %q", service.relayID, service.categoryID)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Reset captures because all cases share one service instance.
			service.relayID, service.listRelayID, service.categoryID, service.categoryName = "", "", "", ""
			response := httptest.NewRecorder()
			router.ServeHTTP(response, wireGuardAdminRequest(test.method, test.path, test.body))
			if response.Code != test.wantStatus {
				t.Fatalf("%s %s = %d, want %d, body=%s", test.method, test.path, response.Code, test.wantStatus, response.Body.String())
			}
			test.check(t)
		})
	}
}

func TestWireGuardPeerRoutesContract(t *testing.T) {
	t.Parallel()
	name := "Laptop"
	category := "Personal"
	enabled := false
	service := &wireGuardRoutesService{peer: wireguard.Peer{ID: testPeerID, Name: name, Category: category, Enabled: enabled}}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})

	request := wireGuardAdminRequest(http.MethodPatch, "/api/admin/wireguard/relays/"+testRelayID+"/peers/"+testPeerID, `{"name":"Laptop","category":"Personal","enabled":false}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.relayID != testRelayID || service.peerID != testPeerID || service.peerUpdate.Name == nil || *service.peerUpdate.Name != name || service.peerUpdate.Category == nil || *service.peerUpdate.Category != category || service.peerUpdate.Enabled == nil || *service.peerUpdate.Enabled != enabled {
		t.Fatalf("peer update = status %d, args %#v/%q/%q, body=%s", response.Code, service.peerUpdate, service.relayID, service.peerID, response.Body.String())
	}

	service.relayID, service.peerID = "", ""
	request = wireGuardAdminRequest(http.MethodPut, "/api/admin/wireguard/relays/"+testRelayID+"/peers/order", `{"items":[{"peerId":"`+testSecondPeer+`","category":"Work"},{"peerId":"`+testPeerID+`","category":"Personal"}]}`)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	wantOrder := wireguard.UpdatePeerOrderRequest{Items: []wireguard.PeerOrderItem{{PeerID: testSecondPeer, Category: "Work"}, {PeerID: testPeerID, Category: "Personal"}}}
	if response.Code != http.StatusNoContent || service.relayID != testRelayID || !reflect.DeepEqual(service.peerOrder, wantOrder) {
		t.Fatalf("peer order = status %d, args %q/%#v", response.Code, service.relayID, service.peerOrder)
	}

	request = wireGuardAdminRequest(http.MethodDelete, "/api/admin/wireguard/relays/"+testRelayID+"/peers/"+testPeerID, "")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.relayID != testRelayID || service.peerID != testPeerID {
		t.Fatalf("peer delete = status %d, args %q/%q", response.Code, service.relayID, service.peerID)
	}
}

func TestWireGuardRoutesMapServiceErrorsToHTTPStatuses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, method, path string
		err                error
		want               int
	}{
		{"category conflict", http.MethodPatch, "/api/admin/wireguard/relays/" + testRelayID + "/categories/" + testCategoryID, &workout.Error{Status: http.StatusConflict, Message: "duplicate"}, http.StatusConflict},
		{"peer not found", http.MethodDelete, "/api/admin/wireguard/relays/" + testRelayID + "/peers/" + testPeerID, &workout.Error{Status: http.StatusNotFound, Message: "missing"}, http.StatusNotFound},
		{"generic failure", http.MethodPut, "/api/admin/wireguard/relays/" + testRelayID + "/peers/order", errors.New("database down"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &wireGuardRoutesService{categoryErr: test.err, peerErr: test.err, category: wireguard.PeerCategory{ID: testCategoryID}}
			router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})
			body := ""
			if test.method == http.MethodPatch {
				body = `{"name":"Work"}`
			} else if test.method == http.MethodPut {
				body = `{"items":[]}`
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, wireGuardAdminRequest(test.method, test.path, body))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestWireGuardRoutesRejectMalformedUUIDsBeforeCallingTheService(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, method, path string
	}{
		{"relay", http.MethodDelete, "/api/admin/wireguard/relays/not-a-uuid"},
		{"noncanonical relay", http.MethodDelete, "/api/admin/wireguard/relays/urn:uuid:" + testRelayID},
		{"peer", http.MethodDelete, "/api/admin/wireguard/relays/" + testRelayID + "/peers/not-a-uuid"},
		{"category", http.MethodDelete, "/api/admin/wireguard/relays/" + testRelayID + "/categories/not-a-uuid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &wireGuardRoutesService{}
			router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, wireGuardAdminRequest(test.method, test.path, ""))
			if response.Code != http.StatusBadRequest || service.relayID != "" || service.peerID != "" || service.categoryID != "" {
				t.Fatalf("malformed UUID = status %d, service args %q/%q/%q, body=%s", response.Code, service.relayID, service.peerID, service.categoryID, response.Body.String())
			}
		})
	}
}
