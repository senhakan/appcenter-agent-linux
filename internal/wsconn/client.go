package wsconn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Client manages a persistent WebSocket connection to the server with
// automatic reconnection using exponential backoff.
type Client struct {
	wsURL     string
	agentUUID string
	secretKey string
	version   string
	platform  string
	arch      string
	hostname  string
	osVersion string
	ipAddress string
	fullIP    []string

	reconnectMin time.Duration
	reconnectMax time.Duration

	callbacks Callbacks
	logger    *log.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

// Config holds the parameters needed to create a WS client.
type Config struct {
	// ServerURL is the base HTTP URL (e.g. "http://10.6.100.170:8000").
	// The WS URL is derived from this.
	ServerURL string

	// WSURL overrides the derived WS URL if non-empty.
	WSURL string

	AgentUUID string
	SecretKey string
	Version   string
	Platform  string
	Arch      string
	Hostname  string
	OSVersion string
	IPAddress string
	FullIP    []string

	ReconnectMinSec int
	ReconnectMaxSec int

	Callbacks Callbacks
	Logger    *log.Logger
}

func deriveWSURL(serverURL, wsURL string) string {
	if wsURL != "" {
		return wsURL
	}
	base := strings.TrimRight(serverURL, "/")
	if strings.HasPrefix(base, "https://") {
		base = "wss://" + strings.TrimPrefix(base, "https://")
	} else {
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base + "/api/v1/agent/ws"
}

func NewClient(cfg Config) *Client {
	minSec := cfg.ReconnectMinSec
	if minSec <= 0 {
		minSec = 2
	}
	maxSec := cfg.ReconnectMaxSec
	if maxSec <= 0 {
		maxSec = 60
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Client{
		wsURL:        deriveWSURL(cfg.ServerURL, cfg.WSURL),
		agentUUID:    cfg.AgentUUID,
		secretKey:    cfg.SecretKey,
		version:      cfg.Version,
		platform:     cfg.Platform,
		arch:         cfg.Arch,
		hostname:     cfg.Hostname,
		osVersion:    cfg.OSVersion,
		ipAddress:    cfg.IPAddress,
		fullIP:       cfg.FullIP,
		reconnectMin: time.Duration(minSec) * time.Second,
		reconnectMax: time.Duration(maxSec) * time.Second,
		callbacks:    cfg.Callbacks,
		logger:       logger,
	}
}

// Run starts the WS client loop. It blocks until ctx is cancelled.
// It automatically reconnects with exponential backoff on failure.
func (c *Client) Run(ctx context.Context) {
	backoff := c.reconnectMin

	for {
		if ctx.Err() != nil {
			c.logger.Println("ws client stopped")
			return
		}

		err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			c.logger.Println("ws client stopped")
			return
		}

		if c.callbacks.OnDisconnected != nil {
			c.callbacks.OnDisconnected()
		}

		if err != nil {
			c.logger.Printf("ws connection lost: %v (reconnect in %v)", err, backoff)
		} else {
			backoff = c.reconnectMin
		}

		// Jitter: ±25%
		jitter := time.Duration(float64(backoff) * (0.75 + 0.5*rand.Float64()))
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter):
		}

		if err != nil {
			backoff = time.Duration(math.Min(float64(backoff)*2, float64(c.reconnectMax)))
		}
	}
}

func (c *Client) connectAndServe(ctx context.Context) error {
	c.logger.Printf("ws connecting to %s", c.wsURL)

	conn, _, err := websocket.Dial(ctx, c.wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{},
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "bye")
	}()

	// Increase read limit for large server.hello payloads.
	conn.SetReadLimit(1 << 20) // 1 MB

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// 1) Send agent.auth
	authMsg := newMessage("agent.auth", map[string]any{
		"uuid":   c.agentUUID,
		"secret": c.secretKey,
	})
	if err := c.writeJSON(ctx, conn, authMsg); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// 2) Read server.auth.ok
	authResp, err := c.readMessage(ctx, conn)
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	if authResp.Type == "server.auth.result" {
		if ok, _ := authResp.Payload["ok"].(bool); !ok {
			errMsg, _ := authResp.Payload["error"].(string)
			return fmt.Errorf("auth rejected: %s", errMsg)
		}
	} else if authResp.Type != "server.auth.ok" {
		return fmt.Errorf("unexpected auth response type: %s", authResp.Type)
	}

	c.logger.Printf("ws authenticated")

	// 3) Send agent.hello
	helloMsg := newMessage("agent.hello", map[string]any{
		"hostname":   c.hostname,
		"os_version": c.osVersion,
		"version":    c.version,
		"platform":   c.platform,
		"arch":       c.arch,
		"ip_address": c.ipAddress,
		"full_ip":    c.fullIP,
	})
	if err := c.writeJSON(ctx, conn, helloMsg); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// 4) Read server.hello
	helloResp, err := c.readMessage(ctx, conn)
	if err != nil {
		return fmt.Errorf("read hello response: %w", err)
	}
	if helloResp.Type == "server.hello" {
		c.logger.Printf("ws server.hello received")
		if c.callbacks.OnServerHello != nil {
			c.callbacks.OnServerHello(helloResp.Payload)
		}
	}

	// Connected successfully — reset backoff will happen in Run()
	if c.callbacks.OnConnected != nil {
		c.callbacks.OnConnected()
	}

	c.logger.Printf("ws connected, entering message loop")

	// 5) Message loop
	err = c.messageLoop(ctx, conn)
	if err != nil {
		// A dropped connection after a successful handshake is expected and should
		// not keep increasing reconnect backoff.
		if ctx.Err() != nil {
			return err
		}
		c.logger.Printf("ws message loop ended: %v", err)
		return nil
	}
	return nil
}

func (c *Client) messageLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		msg, err := c.readMessage(ctx, conn)
		if err != nil {
			return err
		}

		switch msg.Type {
		case "server.ping":
			pong := newMessage("agent.pong", map[string]any{
				"ref": msg.ID,
			})
			if writeErr := c.writeJSON(ctx, conn, pong); writeErr != nil {
				return fmt.Errorf("send pong: %w", writeErr)
			}

		case "server.signal":
			c.logger.Printf("ws received server.signal")
			if c.callbacks.OnSignal != nil {
				c.callbacks.OnSignal()
			}

		case "server.hello":
			// Possible re-hello after reconnect
			if c.callbacks.OnServerHello != nil {
				c.callbacks.OnServerHello(msg.Payload)
			}

		case "server.command.dispatch":
			if c.callbacks.OnServerCommand != nil {
				c.callbacks.OnServerCommand(msg.Payload)
			}

		case "server.rs.request":
			if c.callbacks.OnRSRequest != nil {
				c.callbacks.OnRSRequest(msg.Payload)
			}

		case "server.rs.end":
			if c.callbacks.OnRSEnd != nil {
				c.callbacks.OnRSEnd(msg.Payload)
			}

		case "server.config.patch":
			if c.callbacks.OnConfigPatch != nil {
				c.callbacks.OnConfigPatch(msg.Payload)
			}

		case "server.inventory.sync_required":
			if c.callbacks.OnInventorySyncRequired != nil {
				c.callbacks.OnInventorySyncRequired(msg.Payload)
			}

		case "server.broadcast.restart":
			if c.callbacks.OnBroadcastRestart != nil {
				c.callbacks.OnBroadcastRestart(msg.Payload)
			}

		case "server.broadcast.self_update":
			if c.callbacks.OnBroadcastSelfUpdate != nil {
				c.callbacks.OnBroadcastSelfUpdate(msg.Payload)
			}

		default:
			c.logger.Printf("ws unhandled message type: %s", msg.Type)
		}
	}
}

func (c *Client) writeJSON(ctx context.Context, conn *websocket.Conn, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) readMessage(ctx context.Context, conn *websocket.Conn) (Message, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("unmarshal: %w", err)
	}
	return msg, nil
}

// SendMessage sends a message to the server if connected. Returns false if not connected.
func (c *Client) SendMessage(ctx context.Context, msg Message) bool {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return false
	}

	if err := c.writeJSON(ctx, conn, msg); err != nil {
		c.logger.Printf("ws send failed: %v", err)
		return false
	}
	return true
}

// SendEvent sends a typed event payload using the standard envelope.
func (c *Client) SendEvent(ctx context.Context, msgType string, payload map[string]any) bool {
	return c.SendMessage(ctx, newMessage(msgType, payload))
}

// IsConnected returns true if a WS connection is currently active.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}
