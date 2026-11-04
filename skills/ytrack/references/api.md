# YouTrack REST API through `ytrack api`

Use `ytrack api` when no `ytrack` command covers the job. It sends the stored
token to the active host (or `--host`), so you never handle a token.

## Requests

```sh
ytrack api /users/me --fields login,fullName
ytrack api /issues/APP-123 --fields 'idReadable,summary,customFields(name,value(name))'
ytrack api /issues -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
ytrack api /admin/projects --fields shortName,name --paginate
ytrack api /issues/APP-123/comments -f text='Posted through the API'
ytrack api /issues/APP-123/comments/4-17 -X DELETE
ytrack api /commands --input command.json
```

- The path may omit `/api`: `/issues`, `issues` and `/api/issues` are the same.
- `--fields` sets YouTrack's `fields=` projection. Without it YouTrack returns
  little more than `id` and `$type`; name every attribute you need, nesting
  with parentheses: `reporter(login,fullName)`.
- `-f key=value` adds a string parameter, `-F key=value` a typed one (`true`,
  `false`, `null` and integers become JSON; `@file` reads a file). Keys nest:
  `-F 'project[id]=0-0'`.
- GET sends parameters in the query string. Any parameter or `--input` makes
  the request a POST with a JSON body unless `-X` names a method;
  `Content-Type: application/json` is added for you.
- `--paginate` follows `$skip`/`$top` and prints one JSON array (GET only).
- `--jq '<expr>'` filters the response; it does not need `--json` here.
- `--include` prints the status line and headers before the body.

## Errors

A failed request prints YouTrack's error on stderr and exits 1; exit 4 means
the token is missing or rejected (ask the user to run `ytrack auth login`).
`--debug` logs each request on stderr with the token redacted.

Reference: https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html
