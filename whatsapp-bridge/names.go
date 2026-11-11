package main

import (
	"context"
	"strings"
	"unicode"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Find a display name for a person. Contacts are keyed by phone number, but chats now often use
// a LID (anonymous ID), so a LID is mapped to its phone number before looking it up.
func lookupContactName(client *whatsmeow.Client, jid types.JID) string {
	ctx := context.Background()
	candidates := []types.JID{jid.ToNonAD()}
	if jid.Server == types.HiddenUserServer {
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
			candidates = append([]types.JID{pn}, candidates...)
		}
	}

	for _, candidate := range candidates {
		contact, err := client.Store.Contacts.GetContact(ctx, candidate)
		if err != nil || !contact.Found {
			continue
		}
		// Names saved in your address book are trusted
		for _, name := range []string{contact.FullName, contact.FirstName} {
			if name = cleanName(name); name != "" {
				return name
			}
		}
		// Names the other person chose themselves are marked so they can't pass for a saved contact
		for _, name := range []string{contact.BusinessName, contact.PushName} {
			if name = cleanName(name); name != "" {
				return selfChosenPrefix + name
			}
		}
	}
	return ""
}

// Marks a name the other person chose, as in WhatsApp's own "~Name" display
const selfChosenPrefix = "~"

// Strip control characters and newlines and cap the length, since names end up in the LLM context
func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) && r != '‍' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	if runes := []rune(name); len(runes) > 60 {
		name = string(runes[:60])
	}
	return name
}

// Recompute every direct chat name from the current address book. Stored names are never trusted, so names
// saved by older versions or contacts removed from the address book cannot keep an unmarked name.
func backfillChatNames(client *whatsmeow.Client, messageStore *MessageStore, logger waLog.Logger) {
	rows, err := messageStore.db.Query("SELECT jid, name FROM chats WHERE jid NOT LIKE '%@g.us'")
	if err != nil {
		logger.Warnf("Failed to list chats for name backfill: %v", err)
		return
	}

	updates := map[string]string{}
	for rows.Next() {
		var chatJID, name string
		if err := rows.Scan(&chatJID, &name); err != nil {
			continue
		}
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			continue
		}
		resolved := lookupContactName(client, jid)
		if resolved == "" {
			resolved = jid.User
		}
		if resolved != name {
			updates[chatJID] = resolved
		}
	}
	rows.Close()

	for chatJID, name := range updates {
		if _, err := messageStore.db.Exec("UPDATE chats SET name = ? WHERE jid = ?", name, chatJID); err != nil {
			logger.Warnf("Failed to rename chat %s: %v", chatJID, err)
		}
	}
	if len(updates) > 0 {
		logger.Infof("Resolved names for %d chats", len(updates))
	}
}
