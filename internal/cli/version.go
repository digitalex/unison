package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Version is stamped into release builds (-ldflags -X, see release.yml).
var Version string

// versionWarnInterval is how long normal commands wait before repeating an
// outdated-CLI warning (#337).
const versionWarnInterval = 24 * time.Hour

// buildVersion names this binary: the release it was stamped with, else what
// the Go toolchain recorded (`go install …@latest` knows its module version; a
// build from a clone knows its commit), else "dev".
func buildVersion() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			return "dev-" + s.Value[:12]
		}
	}
	return "dev"
}

func (a *app) version(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: version", errUsage)
	}
	v := buildVersion()
	sv := ""
	if a.serverExplicit {
		sv = a.fetchServerVersion()
	}
	if a.jsonOut {
		out := map[string]string{"version": v}
		if sv != "" {
			out["server_version"] = sv
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return a.printJSON(raw)
	}
	a.printf("unison %s\n", v)
	if sv != "" {
		a.printf("server %s\n", sv)
	}
	return nil
}

func (a *app) fetchServerVersion() string {
	c := newClient(a.server, "")
	c.http = &http.Client{Timeout: 2 * time.Second}
	var resp struct {
		Version string `json:"version"`
	}
	if err := c.do("GET", "/api/version", nil, &resp); err != nil {
		return ""
	}
	if !isStampedVersion(resp.Version) {
		return ""
	}
	return resp.Version
}

func isStampedVersion(v string) bool {
	return v != "" && v != "dev" && !strings.HasPrefix(v, "dev-")
}

type semver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(v string) (semver, bool) {
	if !strings.HasPrefix(v, "v") {
		return semver{}, false
	}
	v = v[1:]
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var pre string
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || maj < 0 || min < 0 || pat < 0 {
		return semver{}, false
	}
	return semver{major: maj, minor: min, patch: pat, pre: pre}, true
}

// isVersionOlder reports whether client is strictly older than server.
// Dev builds without a version stamp return false so they stay quiet.
func isVersionOlder(client, server string) bool {
	if !isStampedVersion(client) || !isStampedVersion(server) {
		return false
	}
	c, ok1 := parseSemver(client)
	s, ok2 := parseSemver(server)
	if !ok1 || !ok2 {
		return false
	}
	if c.major != s.major {
		return c.major < s.major
	}
	if c.minor != s.minor {
		return c.minor < s.minor
	}
	if c.patch != s.patch {
		return c.patch < s.patch
	}
	if c.pre == s.pre {
		return false
	}
	if c.pre == "" {
		return false // release is newer than prerelease
	}
	if s.pre == "" {
		return true
	}
	return comparePre(c.pre, s.pre) < 0
}

func comparePre(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		ai, aErr := strconv.Atoi(ap[i])
		bi, bErr := strconv.Atoi(bp[i])
		switch {
		case aErr == nil && bErr == nil:
			if ai < bi {
				return -1
			}
			return 1
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		case ap[i] < bp[i]:
			return -1
		default:
			return 1
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	if len(ap) > len(bp) {
		return 1
	}
	return 0
}

func versionWarnFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "unison", "version-warn.json")
}

func (a *app) checkServerVersion(serverVer string) {
	if a.versionChecked || a.errOut == nil {
		return
	}
	cliVer := buildVersion()
	if !isVersionOlder(cliVer, serverVer) {
		return
	}
	a.versionChecked = true
	now := time.Now()
	path := versionWarnFile()
	warns := map[string]time.Time{}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &warns)
		}
	}
	if !a.forceVersionWarn {
		if last, ok := warns[a.server]; ok && now.Sub(last) < versionWarnInterval {
			return
		}
	}
	warnOutdated(a.errOut, cliVer, serverVer, a.server)
	if path != "" {
		warns[a.server] = now
		if raw, err := json.Marshal(warns); err == nil {
			_ = os.MkdirAll(filepath.Dir(path), 0o700)
			_ = os.WriteFile(path, raw, 0o600)
		}
	}
}

func warnOutdated(w io.Writer, cliVer, serverVer, serverURL string) {
	fmt.Fprintf(w, "this unison CLI (%s) is older than the server (%s); update with: npm install -g @digitalex99/unison  (or curl -fsSL %s/download/unison -o unison)\n",
		cliVer, serverVer, strings.TrimRight(serverURL, "/"))
}
