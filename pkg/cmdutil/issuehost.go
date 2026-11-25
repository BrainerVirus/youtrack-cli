package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// readableID matches a readable issue ID and captures its project.
var readableID = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)-[0-9]+$`)

// IssueProject returns the project short name of a readable issue ID
// (APP-123 gives APP), or "" for a database ID such as 2-1234.
func IssueProject(id string) string {
	if m := readableID.FindStringSubmatch(id); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// WithIssueHost runs run, which talks to the host APIClient picks, for a
// command that reads issue id. When the folder rule or the default host
// picked the host (no --host, issue URL, $YTRACK_HOST or $YTRACK_TOKEN), more
// than one host is logged in, and the picked host does not have id's project
// while exactly one other logged-in host does, run talks to that host
// instead and one stderr line says so. The answer comes from the project
// cache when it knows; otherwise run first goes to the picked host and only a
// 404 triggers the project lookup (one paged read per host, cached for
// config.ProjectCacheTTL) and a single rerun. When the picked host lacks the
// project and two or more other hosts have it, the 404 ends with
// "; try --host <a> or --host <b>". Hosts whose `auth paths` fence them to
// other folders take no part: they are never probed, switched to or named.
// No other error ever suggests a host.
func (f *Factory) WithIssueHost(ctx context.Context, id string, run func() error) error {
	return f.withIssueHost(ctx, id, false, run)
}

// WithIssueHostForWrite is WithIssueHost for a command that changes issue id:
// it settles the host before run, looking the project up when the cache does
// not know, so no write request ever goes to a host that does not own the key.
func (f *Factory) WithIssueHostForWrite(ctx context.Context, id string, run func() error) error {
	return f.withIssueHost(ctx, id, true, run)
}

func (f *Factory) withIssueHost(ctx context.Context, id string, write bool, run func() error) error {
	f.keyHost, f.keyProject = "", ""
	project := IssueProject(id)
	if project == "" || os.Getenv("YTRACK_TOKEN") != "" {
		return run()
	}
	cfg, err := f.Config()
	if err != nil {
		return run()
	}
	host, res, err := f.HostResolution(cfg)
	if err != nil || !slices.Contains([]string{HostFromPath, HostFromMainCheckout, HostFromDefault}, res.Source) {
		return run()
	}
	// A host fenced to other folders by `auth paths` is never probed,
	// switched to or named.
	others := f.suggestable(cfg, otherHosts(cfg, host.Key))
	if len(others) == 0 {
		return run()
	}
	probe := projectProbe{f: f, cfg: cfg, project: project, cache: f.projectCache()}
	if owner, ok := probe.owner(ctx, host, others, write, write); ok {
		f.useKeyHost(owner, project, host, res)
		return run()
	}
	err = run()
	if !isNotFound(err) {
		return err
	}
	owners, ok := probe.owners(ctx, host, others, true)
	if !ok || len(owners) == 0 {
		return err
	}
	if len(owners) == 1 && !write {
		f.useKeyHost(owners[0], project, host, res)
		return run()
	}
	return withTryHosts(err, owners)
}

// suggestable keeps the keys WithIssueHost may probe, switch to or name in
// this folder. A host with path globs (`auth paths`) is meant for those
// folders only, so it is kept only when the folder rule picks it for the
// working directory (through a worktree's main checkout too, see folderRule).
func (f *Factory) suggestable(cfg *config.Config, keys []string) []string {
	m, _, matched, err := f.folderRule(cfg)
	var out []string
	for _, k := range keys {
		if len(cfg.HostPaths(k)) > 0 && (err != nil || !matched || m.Host != k) {
			continue
		}
		out = append(out, k)
	}
	return out
}

// withTryHosts appends "; try --host <a> or --host <b>" to err's message,
// keeping err underneath; err is unchanged when hosts is empty.
func withTryHosts(err error, hosts []string) error {
	if len(hosts) == 0 {
		return err
	}
	tries := make([]string, len(hosts))
	for i, h := range hosts {
		tries[i] = "--host " + h
	}
	return &tryHostsError{err: err, suffix: "; try " + strings.Join(tries, " or ")}
}

type tryHostsError struct {
	err    error
	suffix string
}

func (e *tryHostsError) Error() string { return e.err.Error() + e.suffix }
func (e *tryHostsError) Unwrap() error { return e.err }

func (f *Factory) useKeyHost(owner, project string, picked hosts.Host, res Resolution) {
	f.keyHost, f.keyProject = owner, project
	why := "the default host is " + picked.Key
	if res.Source == HostFromPath || res.Source == HostFromMainCheckout {
		why = "the folder rule picked " + picked.Key
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "using %s for %s-* (%s)\n", owner, project, why)
}

func (f *Factory) projectCache() config.ProjectCache {
	dir, err := config.CacheDir()
	if err != nil {
		dir = ""
	}
	return config.ProjectCache{Dir: dir, Now: f.Now}
}

func isNotFound(err error) bool {
	apiErr, ok := errors.AsType[*transport.APIError](err)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

// projectProbe answers which logged-in host has a project.
type projectProbe struct {
	f       *Factory
	cfg     *config.Config
	project string
	cache   config.ProjectCache
}

// owner returns the one host in others that has the project, when picked
// does not. With lookup false it uses only cached answers; with lookup set it
// asks each host the cache does not know about, with its stored token. With
// write set, a cached "picked lacks the project" is checked again on picked
// before a write is sent elsewhere: the project may have been created since.
func (p projectProbe) owner(ctx context.Context, picked hosts.Host, others []string, lookup, write bool) (string, bool) {
	owners, cached := p.ownersOf(ctx, picked, others, lookup)
	if len(owners) != 1 {
		return "", false
	}
	if write && cached {
		if has, known, _ := p.has(ctx, picked.Key, true, true); has || !known {
			return "", false
		}
	}
	return owners[0], true
}

// owners returns the hosts in others that have the project when picked
// does not, and whether that is known for every host (see owner for lookup).
func (p projectProbe) owners(ctx context.Context, picked hosts.Host, others []string, lookup bool) ([]string, bool) {
	owners, _ := p.ownersOf(ctx, picked, others, lookup)
	return owners, owners != nil
}

// ownersOf is owners, plus whether picked's answer came from the cache; it
// returns nil owners when picked has the project or an answer is unknown,
// and an empty non-nil slice when no other host has it.
func (p projectProbe) ownersOf(ctx context.Context, picked hosts.Host, others []string, lookup bool) (owners []string, cached bool) {
	has, known, cached := p.has(ctx, picked.Key, lookup, false)
	if has || !known {
		return nil, false
	}
	owners = []string{}
	for _, key := range others {
		has, known, _ := p.has(ctx, key, lookup, false)
		if !known {
			return nil, false
		}
		if has {
			owners = append(owners, key)
		}
	}
	return owners, cached
}

// has says whether host key has the project, whether that is known and
// whether the answer came from the cache. The cache is keyed by the host's
// stored service URL, so a host key logged in again against another URL
// does not reuse old answers. With refresh set it skips the cache.
func (p projectProbe) has(ctx context.Context, key string, lookup, refresh bool) (has, known, cached bool) {
	e := p.cfg.Host(key)
	if e == nil {
		return false, false, false
	}
	h, err := hosts.Parse(e.URL)
	if err != nil {
		return false, false, false
	}
	if p.cache.Dir != "" && !refresh {
		if has, known := p.cache.Lookup(h.URL, p.project); known {
			return has, true, true
		}
	}
	if !lookup {
		return false, false, false
	}
	token, _, err := p.f.CredentialStore(p.cfg).Get(key)
	if err != nil || token == "" {
		return false, false, false
	}
	has, err = adapter.HasProject(ctx, p.f.NewClient(h, token), p.project)
	if err != nil {
		return false, false, false
	}
	if p.cache.Dir != "" {
		p.cache.Store(h.URL, p.project, has)
	}
	return has, true, false
}
