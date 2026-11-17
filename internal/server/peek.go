package server

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalex/unison/internal/concern"
	"github.com/digitalex/unison/internal/store"
)

// A peek (#294) answers "would `unison wait` wake me right now?" for callers
// that can't hold a connection open (agents woken by a timer or cron). It is
// a look from someone who isn't here, so it changes nothing anyone can see:
// not the caller's presence (last_seen_at), read marker or open actions. It
// still keeps an agent logged in (see seen and markActive).

// peekCap is the most a peek counts: past it, "a lot" is all that matters.
const peekCap = 100

// peekPage is how many messages or events a peek reads at a time.
const peekPage = 500

func isPeek(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events/peek")
}

// seen records that p made request r: p's presence and, for agents, a
// credential pushed store.AgentCredentialTTL ahead. A peek only extends the
// credential, so a sleeping agent doesn't look online in the roster.
func (s *Server) seen(r *http.Request, p *store.Participant) {
	if !isPeek(r) {
		s.touch(r.Context(), p)
		return
	}
	if err := s.store.ExtendCredential(r.Context(), p); err != nil {
		log.Printf("extend participant %d's credential: %v", p.ID, err)
	}
}

// peek counts what concerns the caller, exactly as `unison wait` would wake
// for it (concern.Filter): their unread chat messages, plus the other events
// after the `after` cursor (default: the newest event, so only unread chat
// counts). `mentions=1` narrows it as `wait --mentions` does.
func (s *Server) peek(w http.ResponseWriter, r *http.Request) {
	ctx, wsID, q, me := r.Context(), r.PathValue("ws"), r.URL.Query(), actor(r)
	latest, err := s.store.LatestSeq(ctx, wsID)
	if err != nil {
		storeError(w, err)
		return
	}
	after := latest
	if v := q.Get("after"); v != "" {
		if after, err = strconv.ParseInt(v, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "after must be an event seq")
			return
		}
	}
	mentionsOnly, _ := strconv.ParseBool(q.Get("mentions"))
	readUpTo, err := s.store.ReadUpTo(ctx, me)
	if err != nil {
		storeError(w, err)
		return
	}
	authors := map[int64]int64{}
	f := &concern.Filter{Me: me, MentionsOnly: mentionsOnly, ReadUpTo: readUpTo, AuthorOf: func(id int64) int64 {
		author, ok := authors[id]
		if !ok {
			if m, err := s.store.GetMessage(ctx, wsID, id); err == nil {
				author = m.AuthorID
			}
			authors[id] = author
		}
		return author
	}}
	count := 0
	for from := readUpTo; count < peekCap; {
		msgs, err := s.store.MessagesAfter(ctx, wsID, from, peekPage)
		if err != nil {
			storeError(w, err)
			return
		}
		for _, m := range msgs {
			authors[m.ID] = m.AuthorID
			if f.MessageConcerns(m) {
				count++
			}
		}
		if len(msgs) < peekPage {
			break
		}
		from = msgs[len(msgs)-1].ID
	}
	for from := after; count < peekCap; {
		evs, err := s.store.EventsAfter(ctx, wsID, from, peekPage)
		if err != nil {
			storeError(w, err)
			return
		}
		for _, e := range evs {
			// Unread messages are counted above, whichever side of the
			// cursor they are on.
			if e.Type != "message.created" && f.Concerns(e) {
				count++
			}
		}
		if len(evs) < peekPage {
			break
		}
		from = evs[len(evs)-1].Seq
	}
	count = min(count, peekCap)
	writeJSON(w, http.StatusOK, map[string]any{"pending": count > 0, "count": count, "latest_seq": latest})
}
