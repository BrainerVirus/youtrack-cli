// Package api implements `ytrack api`, the raw REST escape hatch, modelled on
// `gh api` from github.com/cli/cli (MIT).
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// DefaultPageSize is the $top used by --paginate when the path sets none.
const DefaultPageSize = 100

type options struct {
	path        string
	method      string
	rawFields   []string
	magicFields []string
	headers     []string
	input       string
	fields      string
	paginate    bool
	jq          string
	template    string
	silent      bool
	include     bool
}

// NewCmdAPI returns the api command.
func NewCmdAPI(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Make an authenticated YouTrack REST API request",
		Long: `Make an authenticated request to the YouTrack REST API and print the response.

The path is relative to the REST root and may omit the /api prefix:
"/issues", "issues" and "/api/issues" are the same request. Query parameters
in the path, such as fields=..., are sent as written.

YouTrack returns only the attributes named in the fields query parameter; use
--fields to set it, e.g. --fields 'idReadable,summary,reporter(login)'.

-f/--raw-field adds a string parameter. -F/--field adds a typed parameter:
true, false, null and integers become JSON values, and "@file" reads the value
from a file ("@-" from stdin). Keys nest: -F 'project[id]=0-0'. Parameters go
in the query string for GET and in a JSON body otherwise. Passing parameters
switches the default method from GET to POST.

--paginate follows YouTrack's $skip/$top paging until a short page and prints
all items as one JSON array.`,
		Example: `  $ ytrack api /users/me --fields login,fullName
  $ ytrack api /issues -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
  $ ytrack api /issues --fields idReadable --jq '.[].idReadable'
  $ ytrack api /issues -F 'project[id]=0-0' -f summary='New issue'
  $ ytrack api /commands --input command.json`,
		Args: cmdutil.ExactArgs(1, "expected one argument: the API path, e.g. /issues"),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.path = args[0]
			if !cmd.Flags().Changed("method") && (len(opts.rawFields) > 0 || len(opts.magicFields) > 0 || opts.input != "") {
				opts.method = http.MethodPost
			}
			opts.method = strings.ToUpper(opts.method)
			if err := cmdutil.MutuallyExclusive("cannot use `--jq` and `--template` together", opts.jq != "", opts.template != ""); err != nil {
				return err
			}
			if opts.paginate && opts.method != http.MethodGet {
				return clierr.FlagErrorf("--paginate only works with GET requests")
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.method, "method", "X", http.MethodGet, "HTTP `method` for the request")
	fl.StringArrayVarP(&opts.rawFields, "raw-field", "f", nil, "Add a string parameter in `key=value` format")
	fl.StringArrayVarP(&opts.magicFields, "field", "F", nil, "Add a typed parameter in `key=value` format")
	fl.StringArrayVarP(&opts.headers, "header", "H", nil, "Add a request header in `key:value` format")
	fl.StringVar(&opts.input, "input", "", "The `file` to use as the request body (\"-\" for stdin)")
	fl.StringVar(&opts.fields, "fields", "", "Set YouTrack's fields= query parameter to this `projection`")
	fl.BoolVar(&opts.paginate, "paginate", false, "Fetch all pages with $skip/$top and print one JSON array")
	fl.StringVarP(&opts.jq, "jq", "q", "", "Filter the response with a jq `expression`")
	fl.StringVarP(&opts.template, "template", "t", "", "Format the response with a Go `template`")
	fl.BoolVar(&opts.silent, "silent", false, "Do not print the response body")
	fl.BoolVarP(&opts.include, "include", "i", false, "Print the HTTP status and response headers before the body")
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	path, rawQuery, err := NormalizePath(opts.path, host.URL)
	if err != nil {
		return clierr.FlagErrorWrap(err)
	}
	if opts.fields != "" {
		if hasParam(rawQuery, "fields") {
			return clierr.FlagErrorf("--fields conflicts with fields= in the path")
		}
		rawQuery = appendParam(rawQuery, "fields", opts.fields)
	}

	params, err := parseFields(opts.rawFields, opts.magicFields, ios.In)
	if err != nil {
		return clierr.FlagErrorWrap(err)
	}

	var body io.Reader
	var bodyIsJSON bool
	switch {
	case opts.input != "":
		b, err := readUserFile(opts.input, ios.In)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		if rawQuery, err = addQueryParams(rawQuery, params); err != nil {
			return clierr.FlagErrorWrap(err)
		}
	case opts.method == http.MethodGet || opts.method == http.MethodHead:
		if rawQuery, err = addQueryParams(rawQuery, params); err != nil {
			return clierr.FlagErrorWrap(err)
		}
	case len(params) > 0:
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encoding parameters: %w", err)
		}
		body = bytes.NewReader(b)
		bodyIsJSON = true
	}

	headers := http.Header{}
	for _, h := range opts.headers {
		name, value, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return clierr.FlagErrorf("header %q must be in key:value format", h)
		}
		headers.Add(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	if bodyIsJSON && headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}

	format := output.Format{JQ: opts.jq, Template: opts.template}

	send := func(query string) (*http.Response, error) {
		u := client.URL(path)
		if query != "" {
			u += "?" + query
		}
		req, err := http.NewRequestWithContext(cmd.Context(), opts.method, u, body)
		if err != nil {
			return nil, err
		}
		for name, values := range headers {
			req.Header[name] = values
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if opts.include {
			printHeaders(ios.Out, resp)
		}
		if err := transport.CheckResponse(resp); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		return resp, nil
	}

	if !opts.paginate {
		resp, err := send(rawQuery)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusNoContent || opts.method == http.MethodHead {
			return nil
		}
		if opts.silent {
			_, err = io.Copy(io.Discard, resp.Body)
			return err
		}
		if isJSON(resp) || format != (output.Format{}) {
			return output.WriteJSON(ios, resp.Body, format)
		}
		_, err = io.Copy(ios.Out, resp.Body)
		return err
	}

	items, err := paginate(rawQuery, send)
	if err != nil {
		return err
	}
	if opts.silent {
		return nil
	}
	merged, err := json.Marshal(items)
	if err != nil {
		return err
	}
	merged = append(merged, '\n')
	return output.WriteJSON(ios, bytes.NewReader(merged), format)
}

// paginate requests pages with $skip/$top until one comes back short and
// returns all items. $skip and $top already in the path set the start and
// page size.
func paginate(rawQuery string, send func(string) (*http.Response, error)) ([]json.RawMessage, error) {
	skip, top := 0, DefaultPageSize
	rest := make([]string, 0)
	for part := range strings.SplitSeq(rawQuery, "&") {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		key, _ = url.QueryUnescape(key)
		switch key {
		case "$skip":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return nil, clierr.FlagErrorf("invalid $skip %q", value)
			}
			skip = n
		case "$top":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return nil, clierr.FlagErrorf("invalid $top %q", value)
			}
			top = n
		default:
			rest = append(rest, part)
		}
	}
	base := strings.Join(rest, "&")

	var all []json.RawMessage
	for {
		q := fmt.Sprintf("$skip=%d&$top=%d", skip, top)
		if base != "" {
			q = base + "&" + q
		}
		resp, err := send(q)
		if err != nil {
			return nil, err
		}
		var page []json.RawMessage
		err = json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err != nil {
			return nil, errors.New("--paginate needs an endpoint that returns a JSON array")
		}
		all = append(all, page...)
		if len(page) < top {
			return all, nil
		}
		skip += len(page)
	}
}

var jsonContentType = regexp.MustCompile(`[/+]json(;|$)`)

func isJSON(resp *http.Response) bool {
	return jsonContentType.MatchString(resp.Header.Get("Content-Type"))
}

// NormalizePath splits a user-supplied API path into a path starting with /api
// and the raw query. "/issues", "issues" and "/api/issues" all become
// "/api/issues". An absolute URL is accepted only on the configured host.
func NormalizePath(p, serviceURL string) (path, rawQuery string, err error) {
	if strings.Contains(p, "://") {
		prefix := strings.TrimRight(serviceURL, "/")
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok || (rest != "" && rest[0] != '/' && rest[0] != '?') {
			return "", "", fmt.Errorf("URL %q is not on the configured host %s; pass a path such as /issues", p, prefix)
		}
		p = rest
	}
	path, rawQuery, _ = strings.Cut(p, "?")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path != "/api" && !strings.HasPrefix(path, "/api/") {
		path = "/api" + strings.TrimSuffix(path, "/")
		if path == "/api/" {
			path = "/api"
		}
	}
	return path, rawQuery, nil
}

func hasParam(rawQuery, name string) bool {
	for part := range strings.SplitSeq(rawQuery, "&") {
		key, _, _ := strings.Cut(part, "=")
		if key == name {
			return true
		}
	}
	return false
}

// escapeValue query-escapes v but keeps the characters YouTrack's fields
// syntax uses, so URLs stay readable in --debug output.
func escapeValue(v string) string {
	return strings.NewReplacer("%28", "(", "%29", ")", "%2C", ",", "%24", "$").Replace(url.QueryEscape(v))
}

func appendParam(rawQuery, key, value string) string {
	kv := escapeValue(key) + "=" + escapeValue(value)
	if rawQuery == "" {
		return kv
	}
	return rawQuery + "&" + kv
}

// addQueryParams appends -f/-F parameters to the query. Arrays repeat the key,
// which is how YouTrack reads multi-valued parameters.
func addQueryParams(rawQuery string, params map[string]any) (string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		values, ok := params[k].([]any)
		if !ok {
			values = []any{params[k]}
		}
		for _, v := range values {
			s, err := scalarString(k, v)
			if err != nil {
				return "", err
			}
			rawQuery = appendParam(rawQuery, k, s)
		}
	}
	return rawQuery, nil
}

func scalarString(key string, v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("parameter %q: nested values are only allowed in a request body (use -X POST)", key)
	}
}

func printHeaders(w io.Writer, resp *http.Response) {
	fmt.Fprintf(w, "%s %s\n", resp.Proto, resp.Status)
	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "%s: %s\n", name, strings.Join(resp.Header[name], ", "))
	}
	fmt.Fprintln(w)
}
