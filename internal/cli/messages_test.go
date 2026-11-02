package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestChatAboutItem: `chat send --item` ties a message to a backlog item;
// `chat log` marks it, `backlog show` lists it, and `wait` says what it is
// about.
func TestChatAboutItem(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada")
	e.ok(human, "backlog", "add", "Landing page")

	out := e.ok(agent, "chat", "send", "Hero done,\nstarting the footer", "--item", "1")
	if !strings.Contains(out, `about #1 "Landing page"`) {
		t.Errorf("chat send --item = %q", out)
	}
	msgID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(out)[1]
	// Waiting before reading chat, the human learns what the message is about.
	waitOut := e.ok(human, "wait", "--timeout", "10")
	if !strings.Contains(waitOut, `Ada (about item #1 "Landing page"): Hero done,`) {
		t.Errorf("wait = %q", waitOut)
	}
	e.ok(human, "chat", "send", "nice", "--reply-to", msgID)
	if _, errOut, code := e.run(agent, "chat", "send", "x", "--item", "99"); code == 0 || !strings.Contains(errOut, "no backlog item #99") {
		t.Errorf("unknown item: exit %d %q", code, errOut)
	}

	log := e.ok(human, "chat", "log")
	if !strings.Contains(log, "Ada [item #1]: Hero done,") || strings.Contains(log, "Alex [item #1]") {
		t.Errorf("chat log =\n%s", log)
	}
	show := e.ok(human, "backlog", "show", "1")
	if !strings.Contains(show, "Chat about #1:") || !strings.Contains(show, "Ada: Hero done, …") || strings.Contains(show, "nice") {
		t.Errorf("backlog show =\n%s", show)
	}
	var detail struct {
		Item     store.Item      `json:"item"`
		Messages []store.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(e.ok(human, "backlog", "show", "1", "--json")), &detail); err != nil || detail.Item.MessageCount != 1 || len(detail.Messages) != 1 {
		t.Errorf("backlog show --json = %+v %v", detail, err)
	}
}

func TestWaitUnreadMessages(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--account")

	// Pre-join chat history (even mentioning @Ada) is marked read on join and does not wake Ada.
	e.ok(human, "chat", "send", "@Ada pre-join history")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada")
	if _, _, code := e.run(agent, "wait", "--mentions", "--timeout", "1"); code != 2 {
		t.Fatalf("wait after join with pre-join history: exit = %d, want 2", code)
	}

	// Non-mention chatter does not wake `wait --mentions`, even when unread,
	// yet advances the event cursor; bare `wait` still wakes immediately because it is unread.
	e.ok(human, "chat", "send", "general chatter")
	if _, _, code := e.run(agent, "wait", "--mentions", "--timeout", "1"); code != 2 {
		t.Fatalf("wait --mentions on chatter: exit = %d, want 2", code)
	}
	if out := e.ok(agent, "wait", "--timeout", "10"); !strings.Contains(out, "general chatter") {
		t.Fatalf("bare wait on unread chatter = %q", out)
	}
	// Once printed by `wait`, it is marked read and does not wake again.
	if _, _, code := e.run(agent, "wait", "--timeout", "1"); code != 2 {
		t.Fatalf("second wait after printing unread chatter: exit = %d, want 2", code)
	}

	// Stateless `--token` parking (no .unison.json cursor) still wakes immediately on unread @mention.
	e.ok(human, "chat", "send", "@Ada check the build")
	statelessDir := t.TempDir()
	out := e.ok(statelessDir, "--workspace", ws.ID, "--token", tokenOf(t, agent), "wait", "--mentions", "--timeout", "10")
	if !strings.Contains(out, "@Ada check the build") {
		t.Fatalf("stateless wait --mentions = %q", out)
	}
	if _, _, code := e.run(statelessDir, "--workspace", ws.ID, "--token", tokenOf(t, agent), "wait", "--mentions", "--timeout", "1"); code != 2 {
		t.Fatalf("second stateless wait --mentions: exit = %d, want 2", code)
	}

	// Unread reply in the agent's own thread (without @mention) wakes `wait --mentions` immediately.
	sent := e.ok(agent, "chat", "send", "Starting #153")
	rootID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(sent)[1]
	e.ok(human, "chat", "send", "Sounds good, keep me posted", "--reply-to", rootID)
	out = e.ok(statelessDir, "--workspace", ws.ID, "--token", tokenOf(t, agent), "wait", "--mentions", "--timeout", "10")
	if !strings.Contains(out, "Sounds good, keep me posted") {
		t.Fatalf("stateless wait --mentions on thread reply = %q", out)
	}

	// Reading via `chat log` advances read_up_to, so `wait` does not re-wake for already-read messages.
	e.ok(human, "chat", "send", "@Ada read via chat log")
	e.ok(agent, "chat", "log", "--limit", "5")
	if _, _, code := e.run(agent, "wait", "--mentions", "--timeout", "1"); code != 2 {
		t.Fatalf("wait after chat log: exit = %d, want 2", code)
	}

	// Replying with `--reply-to` marks the target message read, while keeping any newer unread message unread!
	firstOut := e.ok(human, "chat", "send", "@Ada question one")
	firstID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(firstOut)[1]
	e.ok(human, "chat", "send", "@Ada wait, follow-up two!")
	e.ok(agent, "chat", "send", "Answering one", "--reply-to", firstID)
	out = e.ok(agent, "wait", "--mentions", "--timeout", "10")
	if strings.Contains(out, "question one") || !strings.Contains(out, "follow-up two!") {
		t.Fatalf("wait after --reply-to = %q, want only follow-up two", out)
	}
	if _, _, code := e.run(agent, "wait", "--mentions", "--timeout", "1"); code != 2 {
		t.Fatalf("wait after draining follow-up two: exit = %d, want 2", code)
	}
}

func TestChatShowCLI(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada", "--emoji", "🦊")
	e.ok(human, "backlog", "add", "Ship chat show")

	sent := e.ok(agent, "chat", "send", "@Alex PR ready", "--item", "1", "--action")
	rootID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(sent)[1]
	e.ok(human, "chat", "react", rootID, "+1")

	replySent := e.ok(human, "chat", "send", "LGTM!", "--reply-to", rootID)
	replyID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(replySent)[1]

	shownRoot := e.ok(agent, "chat", "show", rootID)
	for _, want := range []string{"[#" + rootID + " ", "🦊 Ada [item #1] [needs @Alex ✓]: @Alex PR ready", "· 👍 1"} {
		if !strings.Contains(shownRoot, want) {
			t.Errorf("chat show %s = %q, missing %q", rootID, shownRoot, want)
		}
	}
	shownReply := e.ok(agent, "chat", "show", "#"+replyID)
	if !strings.Contains(shownReply, "(reply in thread #"+rootID+")") || !strings.Contains(shownReply, "LGTM!") {
		t.Errorf("chat show #%s = %q", replyID, shownReply)
	}

	var m store.Message
	if err := json.Unmarshal([]byte(e.ok(agent, "chat", "show", rootID, "--json")), &m); err != nil || m.Body != "@Alex PR ready" || len(m.Reactions) != 1 {
		t.Errorf("chat show --json = %+v, err %v", m, err)
	}

	if _, errOut, code := e.run(agent, "chat", "show", "9999"); code == 0 || !strings.Contains(errOut, "not found") {
		t.Errorf("chat show missing id: exit %d, stderr %q", code, errOut)
	}
	for _, bad := range [][]string{{"chat", "show"}, {"chat", "show", "1", "2"}, {"chat", "log", "1"}, {"chat", "log", "--limit", "0"}} {
		if _, _, code := e.run(agent, bad...); code == 0 {
			t.Errorf("unison %v succeeded, want usage error", bad)
		}
	}
}
