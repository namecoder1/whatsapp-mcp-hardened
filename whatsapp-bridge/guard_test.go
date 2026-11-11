package main

import (
	"database/sql"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

func newTestGuard(t *testing.T) *SendGuard {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	t.Setenv("WHATSAPP_SEND_PER_HOUR", "3")
	t.Setenv("WHATSAPP_SEND_MIN_INTERVAL", "30s")
	g, err := NewSendGuard(db)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func recordSend(t *testing.T, g *SendGuard, at time.Time) {
	t.Helper()
	if _, err := g.db.Exec("INSERT INTO api_sends (ts, recipient) VALUES (?, ?)", at.Unix(), "x@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRate(t *testing.T) {
	g := newTestGuard(t)
	now := time.Now()

	if err := g.checkRate(now); err != nil {
		t.Fatalf("first send rejected: %v", err)
	}

	recordSend(t, g, now.Add(-10*time.Second))
	if err := g.checkRate(now); err == nil {
		t.Error("send 10s after the previous one accepted, want min interval error")
	}

	recordSend(t, g, now.Add(-20*time.Minute))
	recordSend(t, g, now.Add(-40*time.Minute))
	if err := g.checkRate(now.Add(time.Minute)); err == nil {
		t.Error("4th send within an hour accepted, want hourly limit error")
	}

	// Sends older than an hour no longer count
	if err := g.checkRate(now.Add(50 * time.Minute)); err != nil {
		t.Errorf("send after window expired rejected: %v", err)
	}
}

func TestInvalidGuardSettings(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	for _, env := range [][2]string{{"WHATSAPP_SEND_PER_HOUR", "0"}, {"WHATSAPP_SEND_PER_HOUR", "abc"}, {"WHATSAPP_SEND_MIN_INTERVAL", "30"}} {
		t.Run(env[0]+"="+env[1], func(t *testing.T) {
			t.Setenv(env[0], env[1])
			if _, err := NewSendGuard(db); err == nil {
				t.Error("invalid setting accepted")
			}
		})
	}
}

func TestRestrictionReason(t *testing.T) {
	cases := []struct {
		name       string
		evt        interface{}
		stop       bool
		restricted bool
	}{
		{"temp ban", &events.TemporaryBan{Code: events.TempBanSentToTooManyPeople}, true, true},
		{"reachout timelock", &events.NotifyAccountReachoutTimelock{EnforcementType: "BULK_MESSAGING", IsActive: true}, true, true},
		{"inactive timelock", &events.NotifyAccountReachoutTimelock{IsActive: false}, false, false},
		{"banned", &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureUnknownLogout}, true, true},
		{"locked", &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureMainDeviceGone}, true, true},
		{"normal unlink", &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureLoggedOut}, true, false},
		{"outdated", &events.ClientOutdated{}, true, false},
		{"message", &events.Message{}, false, false},
	}
	for _, c := range cases {
		reason, restricted := restrictionReason(c.evt)
		if (reason != "") != c.stop || restricted != c.restricted {
			t.Errorf("%s: reason=%q restricted=%v, want stop=%v restricted=%v", c.name, reason, restricted, c.stop, c.restricted)
		}
	}
}
