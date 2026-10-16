package controlplane

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestAuthenticatedMetadataWebSocketUpgrade(t *testing.T) {
	fixture := newAPIFixture(t)
	endpoint, _ := url.Parse(fixture.http.URL)
	connection, err := net.Dial("tcp", endpoint.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	cookies := fixture.client.Jar.Cookies(endpoint)
	pairs := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		pairs = append(pairs, cookie.Name+"="+cookie.Value)
	}
	_, err = fmt.Fprintf(connection, "GET /api/v1/stream HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nCookie: %s\r\n\r\n", endpoint.Host, base64.StdEncoding.EncodeToString(nonce), strings.Join(pairs, "; "))
	if err != nil {
		t.Fatal(err)
	}
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101 Switching Protocols") {
		t.Fatalf("status=%q", status)
	}
}
