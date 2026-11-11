package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
)

// Exit codes the MCP server uses to explain why an on-demand start failed
const (
	exitNotPaired      = 4
	exitAlreadyRunning = 5
)

// Held for the bridge's whole lifetime so two bridges never connect with the same session
// (WhatsApp would kick one of them, which looks like a StreamReplaced).
const lockFile = "store/bridge.lock"

// Unix time of the last authenticated API request, used for the idle timeout
var lastActivity atomic.Int64

func markActivity() {
	lastActivity.Store(time.Now().Unix())
}

// When started on demand by the MCP server there is no terminal to show a QR code, so refuse to pair
func pairingAllowed() bool {
	return os.Getenv("WHATSAPP_NO_PAIRING") != "1"
}

// Report whether the bridge is connected; the MCP server polls this after starting the bridge
func statusHandler(client *whatsmeow.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"connected": client.IsConnected(),
			"logged_in": client.IsLoggedIn(),
		})
	}
}

// Stop the bridge after WHATSAPP_IDLE_TIMEOUT without API requests, and when the process that started it
// exits (WHATSAPP_EXIT_WITH_PARENT=1), so an on-demand bridge never outlives the Claude session
func watchLifetime(stop chan<- string) error {
	var idle time.Duration
	if v := os.Getenv("WHATSAPP_IDLE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return fmt.Errorf("invalid WHATSAPP_IDLE_TIMEOUT %q (e.g. 15m, 0 to disable)", v)
		}
		idle = d
	}
	withParent := os.Getenv("WHATSAPP_EXIT_WITH_PARENT") == "1"
	parent := os.Getppid()
	markActivity()

	if idle == 0 && !withParent {
		return nil
	}
	go func() {
		for range time.Tick(5 * time.Second) {
			if withParent && os.Getppid() != parent {
				stop <- "The MCP server that started the bridge has exited"
				return
			}
			if idle > 0 && time.Since(time.Unix(lastActivity.Load(), 0)) > idle {
				stop <- fmt.Sprintf("Idle for %s", idle)
				return
			}
		}
	}()
	return nil
}
