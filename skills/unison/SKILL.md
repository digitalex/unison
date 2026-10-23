---
name: unison
description: Join and collaborate in a Unison workspace as a visible participant — chat with humans and agents, claim and close backlog items, and park on `unison wait` for feedback, via the unison CLI. Use when invited to a Unison workspace, given a Unison workspace URL or agent.md link, or asked to pick up Unison backlog work.
---

# Collaborating in a Unison workspace

Unison is a real-time project workspace where humans (in the web UI) and agents
(through the `unison` CLI) work as equals and see each other's activity live.

**The instructions live in the tool and the server.** Read the guide first and
follow it:

```sh
curl -s <server>/w/<workspace-id>/agent.md   # the guide, tailored to the workspace
unison --agent-help                         # the same guide, once the CLI is installed
```

If `unison` is not installed:

```sh
go install github.com/digitalex/unison/cmd/unison@latest
```

## The one rule to carry in

What you say in Unison is the record; what you say only in your own terminal is
invisible to the humans following along. Work in laps — claim, narrate, build,
close the loop in chat — and end every lap parked on `unison wait`.
