// Package wsrelay implements a simple WebSocket relay for live log streaming.
// The control plane server uses this to push parsed log events to the web UI
// without requiring a separate message broker.
//
// Design: a single Hub broadcasts to all connected WebSocket clients.
// Log producers call Hub.Publish(msg); connected browsers receive the JSON.
//
// This uses only stdlib net/http + bufio (no external WebSocket lib).
// Implements RFC 6455 framing manually — text frames only, no compression.
package wsrelay

import (
	"bufio"
	"bytes"
	"crypto/sha1" //nolint:gosec // SHA-1 is required by RFC 6455
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Hub manages WebSocket connections and broadcasts messages.
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	buf     [][]byte // ring buffer of last 200 messages for late joiners
	bufMax  int
	nextSub uint64
	subs    map[uint64]chan []byte
}

type client struct {
	conn   net.Conn
	send   chan []byte
	closed bool
}

// NewHub creates a Hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
		bufMax:  200,
		subs:    make(map[uint64]chan []byte),
	}
}

// Publish broadcasts a JSON message to all connected clients.
func (h *Hub) Publish(msg []byte) {
	safe := safeControllerEvent(msg)
	h.mu.Lock()
	// Store in ring buffer
	h.buf = append(h.buf, msg)
	if len(h.buf) > h.bufMax {
		h.buf = h.buf[len(h.buf)-h.bufMax:]
	}
	// Snapshot client list
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	if safe != nil {
		for _, ch := range h.subs {
			select {
			case ch <- safe:
			default:
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- safe:
				default:
				}
			}
		}
	}
	h.mu.Unlock()

	for _, c := range clients {
		select {
		case c.send <- msg:
		default:
			// Slow client — drop message, it will catch up or disconnect
		}
	}
}

// SubscribeSafe exposes only allowlisted metadata events to bounded SSE clients.
func (h *Hub) SubscribeSafe() (uint64, <-chan []byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= 128 {
		return 0, nil, false
	}
	h.nextSub++
	ch := make(chan []byte, 1)
	h.subs[h.nextSub] = ch
	return h.nextSub, ch, true
}

func (h *Hub) UnsubscribeSafe(id uint64) {
	h.mu.Lock()
	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
	h.mu.Unlock()
}

func safeControllerEvent(message []byte) []byte {
	if len(message) == 0 || len(message) > 16<<10 {
		return nil
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return nil
	}
	typeName, _ := value["type"].(string)
	allowed := map[string][]string{
		"ai_provider_changed": {"status", "provider", "model", "updated_at"},
		"ai_proposal_changed": {"id", "status", "reviewed_by", "updated_at"},
		"rollout_progress":    {"rollout_id", "status", "progress", "updated_at"},
		"rollout_changed":     {"rollout_id", "status", "updated_at"},
	}
	fields, ok := allowed[typeName]
	if !ok {
		return nil
	}
	safe := map[string]any{"type": typeName}
	for _, field := range fields {
		item, exists := value[field]
		if !exists {
			continue
		}
		switch typed := item.(type) {
		case string:
			if len(typed) <= 256 {
				safe[field] = typed
			}
		case json.Number:
			if field == "progress" {
				if number, err := typed.Float64(); err == nil && number >= 0 && number <= 100 {
					safe[field] = number
				}
			}
		}
	}
	encoded, err := json.Marshal(safe)
	if err != nil || len(encoded) > 4096 {
		return nil
	}
	return encoded
}

// ServeHTTP handles WebSocket upgrade requests.
// Mount this as an http.Handler on your mux: GET /api/v1/stream
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketUpgrade(r) {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return
	}

	key := r.Header.Get("Sec-Websocket-Key")
	accept := wsAcceptKey(key)

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return
	}

	// Send 101 Switching Protocols
	fmt.Fprintf(bufrw, "HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err := bufrw.Flush(); err != nil {
		conn.Close()
		return
	}

	c := &client{conn: conn, send: make(chan []byte, 256)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	// Send recent buffer to new client
	replay := append([][]byte{}, h.buf...)
	h.mu.Unlock()

	// Send buffered messages to catch up
	for _, msg := range replay {
		if err := writeTextFrame(conn, msg); err != nil {
			break
		}
	}

	go c.writeLoop()
	c.readLoop(h) // blocks until client disconnects

	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	conn.Close()
}

func (c *client) writeLoop() {
	for msg := range c.send {
		if err := writeTextFrame(c.conn, msg); err != nil {
			return
		}
	}
}

func (c *client) readLoop(h *Hub) {
	// We only need to read ping/pong/close frames; data from client is ignored.
	buf := bufio.NewReader(c.conn)
	for {
		c.conn.SetReadDeadline(time.Now().Add(70 * time.Second)) //nolint:errcheck
		header := make([]byte, 2)
		if _, err := io.ReadFull(buf, header); err != nil {
			return
		}
		opcode := header[0] & 0x0f
		masked := (header[1] & 0x80) != 0
		payLen := int(header[1] & 0x7f)
		switch payLen {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(buf, ext); err != nil {
				return
			}
			payLen = int(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(buf, ext); err != nil {
				return
			}
			payLen = int(binary.BigEndian.Uint64(ext))
		}
		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(buf, maskKey[:]); err != nil {
				return
			}
		}
		payload := make([]byte, payLen)
		if _, err := io.ReadFull(buf, payload); err != nil {
			return
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i%4]
			}
		}
		switch opcode {
		case 0x8: // close
			return
		case 0x9: // ping — send pong
			if err := writePong(c.conn, payload); err != nil {
				return
			}
		}
	}
}

func writeTextFrame(conn net.Conn, msg []byte) error {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
	l := len(msg)
	var header []byte
	header = append(header, 0x81) // FIN + text opcode
	if l < 126 {
		header = append(header, byte(l))
	} else if l <= 0xffff {
		header = append(header, 126, byte(l>>8), byte(l))
	} else {
		header = append(header, 127,
			0, 0, 0, 0, byte(l>>24), byte(l>>16), byte(l>>8), byte(l))
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(msg)
	return err
}

func writePong(conn net.Conn, payload []byte) error {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	header := []byte{0x8A, byte(len(payload))}
	conn.Write(header) //nolint:errcheck
	_, err := conn.Write(payload)
	return err
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		r.Header.Get("Sec-Websocket-Key") != ""
}

func wsAcceptKey(key string) string {
	h := sha1.New() //nolint:gosec
	h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
