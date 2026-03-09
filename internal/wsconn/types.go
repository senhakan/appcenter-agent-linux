package wsconn

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Message is the standard WS envelope matching the server's make_message format.
type Message struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	TS      string         `json:"ts"`
	Payload map[string]any `json:"payload"`
	Ack     bool           `json:"ack"`
}

func newMessage(msgType string, payload map[string]any) Message {
	return Message{
		ID:      msgID(),
		Type:    msgType,
		TS:      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Payload: payload,
		Ack:     false,
	}
}

func msgID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("msg_%s", hex.EncodeToString(b))
}

// Callbacks that the WS client invokes on lifecycle events.
type Callbacks struct {
	// OnConnected is called after auth+hello succeeds. The caller should
	// suppress HTTP heartbeat while WS is active.
	OnConnected func()

	// OnDisconnected is called when the WS connection drops (before reconnect).
	// The caller should resume HTTP heartbeat.
	OnDisconnected func()

	// OnSignal is called when the server pushes server.signal. Typically
	// triggers an immediate heartbeat.
	OnSignal func()

	// OnServerHello is called with the server.hello payload (config, pending commands, etc.)
	OnServerHello func(payload map[string]any)

	// OnServerCommand is called when server pushes command dispatch events.
	OnServerCommand func(payload map[string]any)

	// OnRSRequest is called when server pushes a remote support session request.
	OnRSRequest func(payload map[string]any)

	// OnRSEnd is called when server pushes a remote support session end signal.
	OnRSEnd func(payload map[string]any)

	// OnConfigPatch is called when server pushes configuration changes.
	OnConfigPatch func(payload map[string]any)

	// OnInventorySyncRequired is called when server requests a full inventory sync.
	OnInventorySyncRequired func(payload map[string]any)

	// OnBroadcastRestart is called when server requests a controlled restart.
	OnBroadcastRestart func(payload map[string]any)

	// OnBroadcastSelfUpdate is called when server asks agent to trigger self-update flow.
	OnBroadcastSelfUpdate func(payload map[string]any)
}
