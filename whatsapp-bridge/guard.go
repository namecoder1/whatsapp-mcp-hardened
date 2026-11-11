package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Written when WhatsApp signals a ban or restriction. While it exists the bridge refuses to start,
// so it never reconnects or shows a new QR code on a restricted account.
const restrictionMarkerFile = "store/ACCOUNT_RESTRICTED"

// Exit code used when the bridge stops because of a restriction
const exitRestricted = 3

// Return the marker contents if a previous run recorded a restriction
func readRestrictionMarker() (string, bool) {
	data, err := os.ReadFile(restrictionMarkerFile)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func writeRestrictionMarker(reason string) {
	content := fmt.Sprintf("%s\n%s\n\nUnlink this device from the phone and wait before using the bridge again.\nDelete this file to allow the bridge to start.\n",
		time.Now().Format(time.RFC3339), reason)
	if err := os.WriteFile(restrictionMarkerFile, []byte(content), 0600); err != nil {
		fmt.Printf("Failed to write %s: %v\n", restrictionMarkerFile, err)
	}
}

// Classify account-health events. Returns a non-empty reason if the bridge must stop.
func restrictionReason(evt interface{}) (reason string, restricted bool) {
	switch v := evt.(type) {
	case *events.TemporaryBan:
		return v.String(), true
	case *events.NotifyAccountReachoutTimelock:
		if v.IsActive {
			return fmt.Sprintf("WhatsApp restricted messaging (%s) until %s", v.EnforcementType, v.TimeEnforcementEnds.Time.Format(time.RFC3339)), true
		}
	case *events.LoggedOut:
		switch v.Reason {
		case events.ConnectFailureMainDeviceGone, events.ConnectFailureUnknownLogout:
			return fmt.Sprintf("Account locked or banned (%v)", v.Reason), true
		}
		// A normal unlink: stop, but a new QR code is fine next time
		return fmt.Sprintf("Device unlinked (%v)", v.Reason), false
	case *events.ClientOutdated:
		return "WhatsApp rejected this client version: update whatsmeow", false
	case *events.StreamReplaced:
		return "Another bridge connected with the same session", false
	}
	return "", false
}

// Rate limits and recipient rules for /api/send
type SendGuard struct {
	mu          sync.Mutex
	db          *sql.DB
	perHour     int
	minInterval time.Duration
	allowGroups bool
}

func NewSendGuard(db *sql.DB) (*SendGuard, error) {
	g := &SendGuard{db: db, perHour: 10, minInterval: 30 * time.Second}

	if v := os.Getenv("WHATSAPP_SEND_PER_HOUR"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid WHATSAPP_SEND_PER_HOUR %q", v)
		}
		g.perHour = n
	}
	if v := os.Getenv("WHATSAPP_SEND_MIN_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("invalid WHATSAPP_SEND_MIN_INTERVAL %q (e.g. 30s, 2m)", v)
		}
		g.minInterval = d
	}
	g.allowGroups = os.Getenv("WHATSAPP_ALLOW_GROUPS") == "1"

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS api_sends (ts INTEGER NOT NULL, recipient TEXT NOT NULL)`)
	return g, err
}

// Run send while holding the guard, after checking the recipient and rate limits; successful sends are recorded
func (g *SendGuard) Do(client *whatsmeow.Client, recipient types.JID, send func() (bool, string)) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.checkRecipient(client, recipient); err != nil {
		return false, err.Error()
	}
	if err := g.checkRate(time.Now()); err != nil {
		return false, err.Error()
	}

	success, message := send()
	if success {
		if _, err := g.db.Exec("INSERT INTO api_sends (ts, recipient) VALUES (?, ?)", time.Now().Unix(), recipient.String()); err != nil {
			fmt.Printf("Failed to record send: %v\n", err)
		}
	}
	return success, message
}

// Only allow chats the other person has already written in, plus your own "message yourself" chat
func (g *SendGuard) checkRecipient(client *whatsmeow.Client, recipient types.JID) error {
	switch recipient.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
	case types.GroupServer:
		if !g.allowGroups {
			return fmt.Errorf("sending to groups is disabled (set WHATSAPP_ALLOW_GROUPS=1 on the bridge)")
		}
	default:
		return fmt.Errorf("sending to %s recipients is not allowed", recipient.Server)
	}

	if own := client.Store.ID; own != nil && recipient.User == own.User {
		return nil
	}
	if own := client.Store.LID; !own.IsEmpty() && recipient.User == own.User {
		return nil
	}

	// The same person can appear as a phone-number JID or a LID, so check both
	ctx := context.Background()
	candidates := []string{recipient.ToNonAD().String()}
	if recipient.Server == types.DefaultUserServer {
		if lid, err := client.Store.LIDs.GetLIDForPN(ctx, recipient); err == nil && !lid.IsEmpty() {
			candidates = append(candidates, lid.String())
		}
	} else if recipient.Server == types.HiddenUserServer {
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, recipient); err == nil && !pn.IsEmpty() {
			candidates = append(candidates, pn.String())
		}
	}

	for _, jid := range candidates {
		var found int
		err := g.db.QueryRow("SELECT 1 FROM messages WHERE chat_jid = ? AND is_from_me = 0 LIMIT 1", jid).Scan(&found)
		if err == nil {
			return nil
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("failed to check chat history: %v", err)
		}
	}
	return fmt.Errorf("refusing to send to %s: they have never written to you in a synced chat", recipient)
}

func (g *SendGuard) checkRate(now time.Time) error {
	var count int
	if err := g.db.QueryRow("SELECT COUNT(*) FROM api_sends WHERE ts > ?", now.Add(-time.Hour).Unix()).Scan(&count); err != nil {
		return fmt.Errorf("failed to check send rate: %v", err)
	}
	if count >= g.perHour {
		return fmt.Errorf("send limit reached: %d messages in the last hour (WHATSAPP_SEND_PER_HOUR=%d)", count, g.perHour)
	}

	// Send times are stored as Unix seconds
	var last sql.NullInt64
	if err := g.db.QueryRow("SELECT MAX(ts) FROM api_sends").Scan(&last); err != nil {
		return fmt.Errorf("failed to check send rate: %v", err)
	}
	if last.Valid {
		if wait := g.minInterval - now.Sub(time.Unix(last.Int64, 0)); wait > 0 {
			return fmt.Errorf("sending too fast: wait %s before the next message", wait.Round(time.Second))
		}
	}
	return nil
}
