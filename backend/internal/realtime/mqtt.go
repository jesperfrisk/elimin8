// Package realtime owns the embedded MQTT broker. Browsers use the same-origin
// /api/mqtt WebSocket endpoint; there is no public TCP MQTT listener.
package realtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type Outbox interface {
	Pending(context.Context) ([]models.Game, error)
	Published(context.Context, string, int64) error
}

type Broker struct {
	server  *mqtt.Server
	service *services.GameService
	wake    chan struct{}
	slots   chan struct{}
}

func New(service *services.GameService) (*Broker, error) {
	caps := mqtt.NewDefaultServerCapabilities()
	caps.MaximumPacketSize = 8192
	caps.MaximumClients = 2048
	caps.MaximumSessionExpiryInterval = 0
	caps.MaximumClientWritesPending = 64
	server := mqtt.New(&mqtt.Options{InlineClient: true, Capabilities: caps, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	b := &Broker{server: server, service: service, wake: make(chan struct{}, 1), slots: make(chan struct{}, 2048)}
	if err := server.AddHook(&authHook{service: service}, nil); err != nil {
		return nil, err
	}
	if err := server.Serve(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Broker) Close() error {
	for _, cl := range b.server.Clients.GetAll() {
		if !cl.Net.Inline {
			cl.Stop(packets.ErrServerShuttingDown)
		}
	}
	return b.server.Close()
}
func (b *Broker) Notify() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
	default:
		http.Error(w, "Too many connections", 503)
		return
	}
	// Gorilla's default origin check requires the request Origin to match Host.
	u := websocket.Upgrader{Subprotocols: []string{"mqtt"}, HandshakeTimeout: 5 * time.Second}
	c, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	c.SetReadLimit(8192)
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	_ = b.server.EstablishConnection("websocket", &socketConn{Conn: c.UnderlyingConn(), socket: c})
}

type socketConn struct {
	net.Conn
	socket *websocket.Conn
	reader io.Reader
}

func (c *socketConn) Read(p []byte) (int, error) {
	for {
		if c.reader == nil {
			kind, reader, err := c.socket.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				return 0, io.ErrUnexpectedEOF
			}
			c.reader = reader
		}
		n, err := c.reader.Read(p)
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}
func (c *socketConn) Write(p []byte) (int, error) {
	if err := c.socket.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

type authHook struct {
	mqtt.HookBase
	service *services.GameService
}

func (h *authHook) ID() string { return "game-access" }
func (h *authHook) Provides(b byte) bool {
	return b == mqtt.OnConnectAuthenticate || b == mqtt.OnACLCheck || b == mqtt.OnPacketRead
}

func (h *authHook) OnPacketRead(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	// Recheck access on keepalive too, so idle revoked/expired connections close.
	if pk.FixedHeader.Type == packets.Pingreq {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, _, err := h.service.Authorize(ctx, string(cl.Properties.Username)); err != nil {
			return pk, packets.ErrNotAuthorized
		}
	}
	return pk, nil
}
func (h *authHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	if pk.Connect.WillFlag || !pk.Connect.Clean {
		return false
	}
	token := string(pk.Connect.Username)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, _, err := h.service.Authorize(ctx, token)
	return err == nil && strings.HasPrefix(cl.ID, a.Hash[:24]+"-")
}
func (h *authHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if write {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, _, err := h.service.Authorize(ctx, string(cl.Properties.Username))
	if err != nil {
		cl.Stop(packets.ErrNotAuthorized)
		return false
	}
	role := "organizer"
	if a.PlayerID != "" {
		role = "player"
	}
	return topic == services.Topic(a.GameID, role)
}

// Only the backend retries notification delivery. Frontends never poll.
// Notifications may coalesce and repeat; MongoDB remains authoritative.
func (b *Broker) RunOutbox(ctx context.Context, repo Outbox) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	b.Notify()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.wake:
		case <-ticker.C:
		}
		for {
			batchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			games, err := repo.Pending(batchCtx)
			if err != nil {
				cancel()
				if ctx.Err() == nil {
					slog.Warn("MQTT outbox read failed; will retry")
				}
				break
			}
			failed := false
			for _, g := range games {
				payload, _ := json.Marshal(map[string]any{"gameId": g.ID, "type": "game.updated", "revision": g.Revision})
				for _, role := range []string{"organizer", "player"} {
					if err = b.server.Publish(services.Topic(g.ID, role), payload, false, 1); err != nil {
						failed = true
						break
					}
				}
				if !failed {
					if err = repo.Published(batchCtx, g.ID, g.Revision); err != nil {
						failed = true
					}
				}
				if failed {
					slog.Warn("MQTT outbox delivery failed; will retry")
					break
				}
			}
			cancel()
			if failed || len(games) < 100 {
				break
			}
		}
	}
}
