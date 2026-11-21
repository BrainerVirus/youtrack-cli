# Raw REST API

`ytrack api` calls any [YouTrack REST
endpoint](https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html)
with the stored token, for what the other commands do not cover.

```sh
ytrack api /users/me --fields login,fullName
ytrack api /issues/APP-123 --fields 'idReadable,summary,customFields(name,value(name))'
ytrack api /issues -X GET -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
ytrack api /issues --fields idReadable --jq '.[].idReadable'
ytrack api /issues -F 'project[id]=0-0' -f summary='New issue'
ytrack api /issues/APP-123/comments -f text='Posted through the API'
ytrack api /issues/APP-123/comments/4-17 -X DELETE
ytrack api /commands --input command.json
```

- The path may omit `/api`: `/issues`, `issues` and `/api/issues` are the
  same request. Query parameters written in the path are sent as written.
- YouTrack returns only the attributes named in `fields=`; `--fields` sets
  it. Without it YouTrack sends little more than `$type` and `id`, and
  ytrack says so on stderr when that is all that came back.
- `-f`/`--raw-field` adds a string parameter, `-F`/`--field` a typed one
  (`true`, `false`, `null` and integers become JSON; `@file` reads a file,
  `@-` standard input). Keys nest: `-F 'project[id]=0-0'`. Parameters go in
  the query string for GET and in a JSON body otherwise.
- `-f`, `-F` and `--input` make the request a POST unless `-X` names a
  method, and a POST to `/issues` creates an issue, so a read with parameters
  needs `-X GET`. Without `-X`, parameters that only read (`query`, `fields`,
  `customFields`, `$top`, `$skip`) are refused instead of sent as a POST:
  add `-X GET` to read or `-X POST` to send the POST.
- A request with a body is sent as `Content-Type: application/json` unless
  `-H` sets another.
- `--paginate` (GET only) pages with `$skip`/`$top` and prints one JSON
  array; `--jq` and `--template` apply to the merged array. `--slurp` is
  accepted for `gh` compatibility and is the default.
- `--jq` and `--template` need no `--json` here. `--include` prints the
  status line and headers before the body, also when the request fails;
  `--silent` prints no body.

The token is sent only to the configured service URL: redirects to other
hosts and from https to http are refused. Plain-http hosts other than
localhost trigger a warning. A failed request prints YouTrack's error on
stderr and exits 1; exit 4 means the token is missing or rejected.
