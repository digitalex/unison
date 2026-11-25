import assert from "node:assert/strict";
import test from "node:test";
import {
  DEFAULT_NOTIFY_EVENTS,
  NOTIFY_EVENTS,
  NOTIFY_EVENTS_KEY,
  eventCategory,
  notification,
  readEventPrefs,
  shouldSuppressForFocus,
  writeEventPrefs,
} from "./notify.js";

function memoryStorage(initial = {}) {
  const map = new Map(Object.entries(initial));
  return {
    getItem: (k) => (map.has(k) ? map.get(k) : null),
    setItem: (k, v) => map.set(k, String(v)),
  };
}

test("default per-event notification preferences match issue #193", () => {
  assert.equal(NOTIFY_EVENTS.length, 6);
  assert.deepEqual(DEFAULT_NOTIFY_EVENTS, {
    mentions: true,
    prs: true,
    joins: true,
    backlog_added: false,
    backlog_closed: false,
    todo_assigned: false,
  });
});

test("readEventPrefs and writeEventPrefs persist and merge over defaults", () => {
  const store = memoryStorage();
  assert.deepEqual(readEventPrefs(store), DEFAULT_NOTIFY_EVENTS);

  const saved = writeEventPrefs({ backlog_added: true, mentions: false, ignored: "nope" }, store);
  assert.equal(saved.backlog_added, true);
  assert.equal(saved.mentions, false);
  assert.equal(saved.prs, true);
  assert.equal(saved.backlog_closed, false);
  assert.deepEqual(readEventPrefs(store), saved);

  // Malformed JSON falls back safely to defaults.
  const corrupt = memoryStorage({ [NOTIFY_EVENTS_KEY]: "{bad json" });
  assert.deepEqual(readEventPrefs(corrupt), DEFAULT_NOTIFY_EVENTS);
});

test("notification respects per-event toggles, ownership, and self-authored suppression", () => {
  const me = { id: 1, name: "Alex", user: { display_name: "Alex" } };
  const participants = [
    me,
    { id: 2, name: "Ada", emoji: "🦊" },
    { id: 3, name: "Bob", emoji: "🧑" },
  ];
  const allOn = {
    mentions: true,
    prs: true,
    joins: true,
    backlog_added: true,
    backlog_closed: true,
    todo_assigned: true,
  };

  // 1. @mentions & action requests in chat
  const mentionEv = { type: "message.created", actor_id: 2, data: { id: 42, body: "Hey @Alex check this" } };
  assert.deepEqual(notification(mentionEv, me, participants), {
    title: "🦊 Ada mentioned you",
    body: "Hey @Alex check this",
    tag: "msg-42",
    message: 42,
  });
  assert.equal(notification({ ...mentionEv, actor_id: 1 }, me, participants), null, "self-authored message suppressed");
  assert.equal(notification(mentionEv, me, participants, { prefs: { ... DEFAULT_NOTIFY_EVENTS, mentions: false } }), null);

  // 2. PR & review events on your items (pr.linked + GitHub PR/review webhooks)
  const myItemWithPR = {
    id: 100,
    number: 7,
    title: "Ship OAuth",
    created_by: 3,
    assignee_id: 1,
    prs: [{ id: 1, url: "https://github.com/acme/app/pull/55" }],
  };
  const otherItemWithPR = { ...myItemWithPR, id: 101, number: 8, created_by: 3, assignee_id: 2 };

  const prEv = { type: "pr.linked", actor_id: 2, data: myItemWithPR };
  assert.deepEqual(notification(prEv, me, participants), {
    title: "🦊 Ada linked a PR to #7",
    body: "Ship OAuth — https://github.com/acme/app/pull/55",
    tag: "pr-100-1",
    item: 7,
  });
  assert.equal(notification({ type: "pr.linked", actor_id: 2, data: otherItemWithPR }, me, participants), null, "PR on unrelated item ignored");
  assert.equal(notification(prEv, me, participants, { prefs: { ...DEFAULT_NOTIFY_EVENTS, prs: false } }), null);

  const reviewHook = {
    type: "webhook.received",
    actor_id: 0,
    data: {
      delivery_id: 9,
      source: "github",
      event: "pull_request_review",
      summary: 'github: pull_request_review submitted #55 "Ship OAuth" by bob',
    },
  };
  assert.deepEqual(notification(reviewHook, me, participants, { items: [myItemWithPR] }), {
    title: "PR #55 update on #7",
    body: 'pull_request_review submitted #55 "Ship OAuth" by bob',
    tag: "wh-9",
    item: 7,
  });
  const selfReviewHook = {
    ...reviewHook,
    data: { ...reviewHook.data, summary: 'github: pull_request_review submitted #55 "Ship OAuth" by alex' },
  };
  assert.equal(notification(selfReviewHook, me, participants, { items: [myItemWithPR] }), null, "self webhook sender suppressed");

  // 3. People & agents joining
  const joinEv = {
    type: "participant.joined",
    actor_id: 2,
    data: { id: 2, name: "Ada", emoji: "🦊", sponsor: { display_name: "Bob" } },
  };
  assert.deepEqual(notification(joinEv, me, participants), {
    title: "🦊 Ada joined",
    body: "Ada (agent of Bob) joined the workspace.",
    tag: "joined-2",
  });

  // 4. Backlog item added (off by default, on when toggled)
  const addEv = { type: "backlog.added", actor_id: 2, data: { id: 200, number: 12, title: "Fix mobile nav" } };
  assert.equal(notification(addEv, me, participants), null, "backlog.added off by default");
  assert.deepEqual(notification(addEv, me, participants, { prefs: allOn }), {
    title: "🦊 Ada added #12",
    body: "Fix mobile nav",
    tag: "backlog-added-200",
    item: 12,
  });
  assert.equal(notification({ ...addEv, actor_id: 1 }, me, participants, { prefs: allOn }), null);

  // 5. Backlog item closed (off by default, on when toggled)
  const closeEv = {
    type: "backlog.closed",
    actor_id: 2,
    data: { id: 200, number: 12, title: "Fix mobile nav", close_note: "Merged PR #60" },
  };
  assert.equal(notification(closeEv, me, participants), null, "backlog.closed off by default");
  assert.deepEqual(notification(closeEv, me, participants, { prefs: allOn }), {
    title: "🦊 Ada closed #12",
    body: "Fix mobile nav — Merged PR #60",
    tag: "backlog-closed-200",
    item: 12,
  });

  // 6. TODO assigned to you (off by default, on when toggled; supports linked item or /todos fallback)
  const todoWithItem = {
    type: "todo.created",
    actor_id: 2,
    data: { id: 5, title: "Review PR #60", assignee_id: 1, item_id: 200, item_number: 12 },
  };
  assert.equal(notification(todoWithItem, me, participants), null, "todo.created off by default");
  assert.deepEqual(notification(todoWithItem, me, participants, { prefs: allOn }), {
    title: "🦊 Ada assigned you a TODO",
    body: "Review PR #60",
    tag: "todo-5",
    item: 12,
  });
  const todoStandalone = {
    type: "todo.added",
    actor_id: 2,
    data: { id: 6, title: "Rotate staging secret", assignee_id: 1 },
  };
  assert.deepEqual(notification(todoStandalone, me, participants, { prefs: allOn }), {
    title: "🦊 Ada assigned you a TODO",
    body: "Rotate staging secret",
    tag: "todo-6",
    screen: "todos",
  });
  assert.equal(
    notification({ ...todoStandalone, data: { ...todoStandalone.data, assignee_id: 3 } }, me, participants, { prefs: allOn }),
    null,
    "TODO for someone else ignored",
  );
  assert.equal(eventCategory({ type: "todo.created" }), "todo_assigned");
});

test("shouldSuppressForFocus suppresses only when visible, focused, and on the matching view/section", () => {
  const sections = [{ id: 10, slug: "decisions", name: "Decisions" }];
  const chatEv = { type: "message.created", data: { id: 1 } };
  const mainBacklogEv = { type: "backlog.added", data: { id: 5, number: 5, section_id: null } };
  const sectionItemEv = { type: "backlog.closed", data: { id: 6, number: 6, section_id: 10 } };
  const todoEv = { type: "todo.created", data: { id: 3, assignee_id: 1 } };
  const joinEv = { type: "participant.joined", data: { id: 2 } };

  // Background / unfocused tab never suppresses.
  assert.equal(shouldSuppressForFocus(chatEv, { view: "chat" }, false), false);
  assert.equal(shouldSuppressForFocus(mainBacklogEv, { view: "backlog" }, false), false);
  assert.equal(shouldSuppressForFocus(joinEv, { view: "backlog" }, false), false);

  // Focused tab suppresses only on the relevant view / section.
  assert.equal(shouldSuppressForFocus(chatEv, { view: "chat" }, true), true);
  assert.equal(shouldSuppressForFocus(chatEv, { view: "backlog" }, true), false);

  assert.equal(shouldSuppressForFocus(mainBacklogEv, { view: "backlog" }, true), true);
  assert.equal(shouldSuppressForFocus(mainBacklogEv, { view: "chat" }, true), false);
  assert.equal(shouldSuppressForFocus(mainBacklogEv, { view: "section", sectionSlug: "decisions", sections }, true), false);

  assert.equal(shouldSuppressForFocus(sectionItemEv, { view: "backlog", sections }, true), false);
  assert.equal(shouldSuppressForFocus(sectionItemEv, { view: "section", sectionSlug: "decisions", sections }, true), true);
  assert.equal(shouldSuppressForFocus(sectionItemEv, { view: "section", sectionSlug: "triage", sections }, true), false);

  assert.equal(shouldSuppressForFocus(todoEv, { view: "todos" }, true), true);
  assert.equal(shouldSuppressForFocus(todoEv, { view: "backlog" }, true), false);

  assert.equal(shouldSuppressForFocus(joinEv, { view: "chat" }, true), true);
});
