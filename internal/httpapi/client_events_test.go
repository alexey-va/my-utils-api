package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPAddressDropsEphemeralPortAndPrefersProxyHeader(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("POST", "/api/client-events", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	if got := clientIPAddress(request); got != "127.0.0.1" {
		t.Fatalf("clientIPAddress(remote) = %q", got)
	}

	request.Header.Set("X-Real-IP", "203.0.113.7")
	if got := clientIPAddress(request); got != "203.0.113.7" {
		t.Fatalf("clientIPAddress(proxy) = %q", got)
	}
}
