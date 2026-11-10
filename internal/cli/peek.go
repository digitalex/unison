package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
)

// peek answers, without waiting, whether `unison wait` would wake right now
// (GET …/events/peek, from wait's saved cursor): "true" and exit code 0, or
// "false" and exit code 2. It changes nothing: wait's cursor, the read
// marker and the caller's presence all stay as they were.
func (a *app) peek(args []string) error {
	fs := flag.NewFlagSet("peek", flag.ContinueOnError)
	mentions := fs.Bool("mentions", false, "count only what `wait --mentions` wakes for")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: peek [--mentions]", errUsage)
	}
	if err := a.needToken(); err != nil {
		return err
	}
	path, err := a.wsPath("/events/peek")
	if err != nil {
		return err
	}
	q := url.Values{}
	if a.cfg.Workspace == a.ws && a.cfg.Cursor != 0 {
		q.Set("after", strconv.FormatInt(a.cfg.Cursor, 10))
	}
	if *mentions {
		q.Set("mentions", "1")
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var raw json.RawMessage
	if err := a.client().do("GET", path, nil, &raw); err != nil {
		return err
	}
	var resp struct {
		Pending bool `json:"pending"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	if a.jsonOut {
		if err := a.printJSON(raw); err != nil {
			return err
		}
	} else {
		a.printf("%t\n", resp.Pending)
	}
	if !resp.Pending {
		return &exitError{code: 2}
	}
	return nil
}
