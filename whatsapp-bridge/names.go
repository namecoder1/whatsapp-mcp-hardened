package main

import (
	"context"
	"strings"

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
		// Prefer the name saved in your address book, then the name they set themselves
		for _, name := range []string{contact.FullName, contact.FirstName, contact.BusinessName, contact.PushName} {
			if name = strings.TrimSpace(name); name != "" {
				return name
			}
		}
	}
	return ""
}

// Rename direct chats that only have their bare ID as a name, now that more contacts or LID mappings may be known
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
		if err != nil || (name != "" && name != jid.User) {
			continue
		}
		if contactName := lookupContactName(client, jid); contactName != "" {
			updates[chatJID] = contactName
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
