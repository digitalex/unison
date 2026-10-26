package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// The inbox: things a human must act on (a PR to review, a question to
// answer, manual setup), as opposed to status updates, which go to chat.
// Inboxes belong to accounts (users), one per workspace, so every device a
// person joined from sees the same inbox. Any participant may send to a
// member's inbox; agents usually send to the member they act for.

// InboxEntry is one entry in a member's inbox.
type InboxEntry struct {
	ID              int64        `json:"id"`
	WorkspaceID     string       `json:"workspace_id"`
	RecipientUserID int64        `json:"recipient_user_id"`
	RecipientName   string       `json:"recipient_name"`
	SenderID        int64        `json:"sender_id"`
	SenderName      string       `json:"sender_name"`
	SenderEmoji     string       `json:"sender_emoji"`
	Body            string       `json:"body"`
	ItemID          *int64       `json:"item_id,omitempty"`
	Item            *MessageItem `json:"item,omitempty"`
	URL             string       `json:"url,omitempty"`
	CreatedAt       int64        `json:"created_at"`
	DismissedAt     int64        `json:"dismissed_at,omitempty"`
}

// InboxEvent is the data of inbox.* events. Events go to everyone in the
// workspace, so they carry no content: the recipient's clients refetch.
type InboxEvent struct {
	ID              int64 `json:"id"`
	RecipientUserID int64 `json:"recipient_user_id"`
}

// selectInbox selects entries (alias e) with their sender and item.
const selectInbox = `SELECT e.id, e.workspace_id, e.recipient_user_id, u.display_name, e.sender_id, p.name, p.emoji, e.body,
	e.item_id, i.title, i.status, e.url, e.created_at, e.dismissed_at
	FROM inbox e JOIN users u ON u.id = e.recipient_user_id JOIN participants p ON p.id = e.sender_id
	LEFT JOIN items i ON i.id = e.item_id`

func scanInboxEntry(row interface{ Scan(...any) error }) (*InboxEntry, error) {
	var e InboxEntry
	var item, dismissed sql.NullInt64
	var title, status, link sql.NullString
	if err := row.Scan(&e.ID, &e.WorkspaceID, &e.RecipientUserID, &e.RecipientName, &e.SenderID, &e.SenderName, &e.SenderEmoji, &e.Body,
		&item, &title, &status, &link, &e.CreatedAt, &dismissed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.Valid {
		e.ItemID = &item.Int64
		e.Item = &MessageItem{ID: item.Int64, Title: title.String, Status: status.String}
	}
	e.URL, e.DismissedAt = link.String, dismissed.Int64
	return &e, nil
}

// InboxMessage is what a sender puts in an inbox.
type InboxMessage struct {
	To     string // a participant's name, or a member's display name; "" for an agent's sponsor
	Body   string
	ItemID *int64
	URL    string
}

// SendInbox puts an entry in a member's inbox. The recipient is To: a
// participant (resolved to its account; agents and unlinked participants
// have no inbox) or a member by display name. Without To, an agent sends
// to the member it acts for.
func (s *Store) SendInbox(ctx context.Context, sender *Participant, msg InboxMessage) (*InboxEntry, []Event, error) {
	body := strings.TrimSpace(msg.Body)
	if body == "" {
		return nil, nil, fmt.Errorf("%w: an inbox entry needs some text", ErrInvalid)
	}
	link := strings.TrimSpace(msg.URL)
	if link != "" {
		if u, err := url.Parse(link); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, nil, fmt.Errorf("%w: the URL must be an http(s) link", ErrInvalid)
		}
	}
	var e *InboxEntry
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		to, err := inboxRecipient(t, sender, strings.TrimPrefix(strings.TrimSpace(msg.To), "@"))
		if err != nil {
			return err
		}
		if msg.ItemID != nil {
			if _, err := getItem(t, sender.WorkspaceID, *msg.ItemID); errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: there is no backlog item #%d in this workspace", ErrInvalid, *msg.ItemID)
			} else if err != nil {
				return err
			}
		}
		id, err := t.insert(`INSERT INTO inbox (workspace_id, recipient_user_id, sender_id, body, item_id, url, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "id",
			sender.WorkspaceID, to, sender.ID, body, msg.ItemID, nullString(link), log.now)
		if err != nil {
			return err
		}
		if e, err = scanInboxEntry(t.QueryRow(selectInbox+` WHERE e.id = ?`, id)); err != nil {
			return err
		}
		return log.add(sender.WorkspaceID, "inbox.created", sender.ID, InboxEvent{ID: id, RecipientUserID: to})
	})
	if err != nil {
		return nil, nil, err
	}
	return e, evs, nil
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// inboxRecipient resolves whom an entry is for (see SendInbox) to a
// current member's user ID.
func inboxRecipient(t *tx, sender *Participant, to string) (int64, error) {
	var user int64
	switch {
	case to == "" && sender.Sponsor != nil:
		user = sender.Sponsor.UserID
	case to == "":
		return 0, fmt.Errorf("%w: say whose inbox (to: a participant's name)", ErrInvalid)
	default:
		var linked, sponsor sql.NullInt64
		err := t.QueryRow(`SELECT user_id, sponsor_user_id FROM participants WHERE workspace_id = ? AND lower(name) = lower(?)`, sender.WorkspaceID, to).Scan(&linked, &sponsor)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Not a participant: a member by display name.
			err = t.QueryRow(`SELECT u.id FROM users u JOIN memberships m ON m.user_id = u.id
				WHERE m.workspace_id = ? AND lower(u.display_name) = lower(?) ORDER BY u.id LIMIT 1`, sender.WorkspaceID, to).Scan(&user)
			if errors.Is(err, sql.ErrNoRows) {
				return 0, fmt.Errorf("%w: nobody called %q here", ErrInvalid, to)
			} else if err != nil {
				return 0, err
			}
		case err != nil:
			return 0, err
		case sponsor.Valid:
			return 0, fmt.Errorf("%w: %s is an agent; inboxes are for the humans (members)", ErrInvalid, to)
		case !linked.Valid:
			return 0, fmt.Errorf("%w: %s isn't linked to an account, so has no inbox", ErrInvalid, to)
		default:
			user = linked.Int64
		}
	}
	member, err := isMember(t, sender.WorkspaceID, user)
	if err != nil {
		return 0, err
	}
	if !member {
		return 0, fmt.Errorf("%w: that person is no longer a member of this workspace", ErrInvalid)
	}
	return user, nil
}

// ListInbox returns a member's inbox in a workspace, newest first: open
// entries, or all of them with includeDismissed.
func (s *Store) ListInbox(ctx context.Context, wsID string, userID int64, includeDismissed bool) ([]InboxEntry, error) {
	q := selectInbox + ` WHERE e.workspace_id = ? AND e.recipient_user_id = ?`
	if !includeDismissed {
		q += ` AND e.dismissed_at IS NULL`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY e.id DESC`, wsID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InboxEntry{}
	for rows.Next() {
		e, err := scanInboxEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// DismissInbox dismisses (or, with dismiss false, restores) an entry of the
// member's own inbox; anyone else's entries are ErrNotFound.
func (s *Store) DismissInbox(ctx context.Context, wsID string, userID, id int64, dismiss bool) (*InboxEntry, []Event, error) {
	var e *InboxEntry
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if e, err = scanInboxEntry(t.QueryRow(selectInbox+` WHERE e.id = ? AND e.workspace_id = ? AND e.recipient_user_id = ?`, id, wsID, userID)); err != nil {
			return fmt.Errorf("inbox entry %d: %w", id, err)
		}
		if (e.DismissedAt != 0) == dismiss {
			return nil
		}
		at, typ := any(log.now), "inbox.dismissed"
		if !dismiss {
			at, typ = nil, "inbox.restored"
		}
		if _, err := t.Exec(`UPDATE inbox SET dismissed_at = ? WHERE id = ?`, at, id); err != nil {
			return err
		}
		if e, err = scanInboxEntry(t.QueryRow(selectInbox+` WHERE e.id = ?`, id)); err != nil {
			return err
		}
		return log.add(wsID, typ, 0, InboxEvent{ID: id, RecipientUserID: userID})
	})
	if err != nil {
		return nil, nil, err
	}
	return e, evs, nil
}

// unlinkInbox keeps inbox entries about a deleted item, no longer tied to it.
func unlinkInbox(t *tx, itemID int64) error {
	_, err := t.Exec(`UPDATE inbox SET item_id = NULL WHERE item_id = ?`, itemID)
	return err
}
