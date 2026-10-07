# `ytrack` — Unofficial YouTrack CLI

**Status:** Accepted starting specification (see Decisions)  
**Document version:** 0.3  
**Last updated:** 2026-10-07  
**Implementation language:** Go  
**CLI framework:** Cobra  

> `ytrack` is an unofficial command-line interface for JetBrains YouTrack. Its UX should feel familiar to engineers who already use `gh` and `glab`, while remaining native to YouTrack's own concepts, query language, command system, and REST API.

## Decisions (2026-10-07)

These settle choices the draft left open. Where the body below still says `yt` or
disagrees, these win.

1. **Build, don't adopt.** Existing YouTrack CLIs are small, unbacked projects.
   This one aims for `gh`/`glab` parity and a JSON contract that workit owns.
   Borrow the agent-frugal output idea from DorskFR/yt.
2. **Go, one static binary per platform.** No runtime install, fast startup, and
   `gh`/`glab` (both Go, MIT) patterns transfer, reused with attribution (§37).
3. **Executable name `ytrack`.** `yt` collides with three existing tools. The
   repository stays `youtrack-cli`. Environment variables use the `YTRACK_` prefix.
4. **v1 architecture.** Keep the layers of §5, but the wire layer is a
   hand-written typed adapter for the resources v1 needs. The OpenAPI generator
   is deferred until one is proven against the polymorphic `$type` schema (open
   questions 3 and 4). Commit `api/openapi/youtrack.json` as a snapshot, and
   contract tests check requests against it.
5. **Agent output.** `--json <field,...>` takes an explicit field list (the `gh`
   rule), so output is frugal by default. Also: `--jq`, `--template`, errors on
   stderr, stable exit codes, and pipe-safe output with no color or prompts when
   not a TTY.
6. **Login.** `ytrack auth login` in v1 is assisted permanent-token login. It
   opens the instance's token page, takes the token through a hidden prompt,
   stdin (`--with-token`) or `YTRACK_TOKEN`, and verifies it with
   `/api/users/me` before saving. Browser login (`--web`: Authorization Code
   with PKCE and a loopback redirect) comes later. It needs an OAuth client
   registered on the instance (YouTrack 2026.2+), and its tokens expire with no
   refresh. YouTrack offers no device-code flow and no dynamic client
   registration (§9.2).
7. **Credentials.** The OS keyring holds one entry per host. Login fails when
   no keyring is available unless `--insecure-storage` is passed (file written
   `0600`). Tokens never appear in argv, and `--debug` redacts `Authorization`.
   `auth status` masks the token; `auth token` prints it on request.
8. **workit integration.** workit calls `ytrack ... --json` when it is
   installed. Its built-in YouTrack code stays one release as a fallback, then
   is removed. A token moves over with `ytrack auth login --with-token`.
9. **Slice order.**
   1. Foundation: auth, config, HTTP, output, `ytrack api`.
   2. `issue list|view|comment`.
   3. `work-item add|list`. After this slice workit can switch to `ytrack`.
   4. `issue create|edit|command`.
   5. goreleaser binaries and a Homebrew tap.

---

## 1. Purpose

The project should provide a high-quality command-line interface for YouTrack with the same general engineering qualities users expect from mature CLIs such as GitHub CLI (`gh`) and GitLab CLI (`glab`):

- secure authentication;
- multiple-host support;
- interactive and non-interactive workflows;
- consistent command structure;
- human-readable output;
- machine-readable JSON output;
- `jq` and template-based filtering/formatting;
- shell completion;
- browser/editor integration;
- predictable scripting behavior;
- strong error messages;
- low startup latency;
- stable internal architecture;
- complete API escape hatch through a raw API command.

The CLI should **not** attempt to turn YouTrack into GitHub or GitLab. It should borrow proven CLI infrastructure and UX conventions while modeling YouTrack correctly.

---

## 2. Important terminology correction

JetBrains currently publishes an **OpenAPI Specification (OAS) 3.0** document for the YouTrack REST API. This is not a "YouTrack REST API v3".

The current public REST API evolves with YouTrack releases. JetBrains publishes a REST API changelog that lists additions and deprecations by YouTrack version.

Official references:

- OpenAPI Specification: <https://www.jetbrains.com/help/youtrack/devportal/youtrack-openapi-specification.html>
- REST API Reference: <https://www.jetbrains.com/help/youtrack/devportal/rest-api-reference.html>
- REST API overview: <https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html>
- REST API URL/endpoints: <https://www.jetbrains.com/help/youtrack/devportal/api-url-and-endpoints.html>

Every YouTrack instance exposes its OAS document at:

```text
<YouTrack Service URL>/api/openapi.json
```

Example:

```text
https://acme.youtrack.cloud/api/openapi.json
```

The OAS document is explicitly intended by JetBrains for generating client libraries with tools such as OpenAPI Generator.

---

## 3. Design goals

### 3.1 Primary goals

1. Provide excellent terminal workflows for everyday YouTrack usage.
2. Provide access to the complete public YouTrack REST API.
3. Work with both YouTrack Cloud and YouTrack Server.
4. Support multiple YouTrack installations.
5. Be secure by default.
6. Be equally usable by humans, shell scripts, CI systems, and automation agents.
7. Minimize maintenance cost as the YouTrack API evolves.
8. Avoid coupling CLI UX directly to raw API endpoint layouts.
9. Keep generated API code disposable and replaceable.
10. Preserve low startup time and low runtime overhead.

### 3.2 Non-goals

The initial project does **not** need to implement concepts whose system of record is outside YouTrack.

Not planned as core features:

- Git repository management;
- cloning repositories;
- branch management;
- Git credential management;
- pull-request creation or merging;
- CI/CD pipeline management;
- SSH key management;
- repository discovery from Git remotes;
- GitHub/GitLab-specific GraphQL abstractions;
- reproducing the entire YouTrack web UI in a terminal.

YouTrack may expose information about VCS changes or pull requests associated with issues, but the external VCS platform remains the system of record for those resources.

---

## 4. Product model

The CLI should have three conceptual surfaces:

```text
                         yt
                          │
          ┌───────────────┼────────────────┐
          │               │                │
   Human workflows      yt api       CLI infrastructure
          │               │                │
     issue list        raw REST            auth
     issue view        requests             config
     issue create                           output
     issue command                          paging
     project ...                            editor
     agile ...                              browser
     article ...                            aliases
     work-item ...                          completion
                                           keyring
                          │
                          ▼
                   YouTrack REST API
                          │
                  /api/openapi.json
```

### Core rule

**Do not create a first-class CLI command for every REST endpoint.**

Instead:

- `yt api` provides effectively complete REST API access;
- first-class commands provide curated, ergonomic workflows;
- the OpenAPI document drives the transport/client layer, not the human UX hierarchy.

This prevents the CLI from becoming a mechanical mirror of hundreds of server endpoints.

---

# 5. Maintainability architecture

This is a foundational design principle.

The CLI must not directly bind its command implementations to generated OpenAPI endpoint types. The OpenAPI-generated layer should be considered **replaceable implementation detail**.

A recommended dependency direction is:

```text
CLI commands
    │
    ▼
Application / use-case services
    │
    ▼
Stable internal YouTrack interfaces
    │
    ▼
Compatibility / translation adapters
    │
    ▼
Generated OpenAPI client
    │
    ▼
HTTP transport
    │
    ▼
YouTrack
```

No layer above the adapter should need to know whether an endpoint path, generated DTO name, or OAS schema changed.

## 5.1 Layer 1 — HTTP transport

Responsibilities:

- base URL normalization;
- authentication headers;
- `Accept` / `Content-Type` handling;
- user agent;
- timeouts;
- TLS behavior;
- retries where safe;
- request IDs / diagnostics;
- HTTP error normalization;
- debug logging;
- connection reuse;
- rate-limit handling if YouTrack exposes relevant signals;
- upload/download streaming.

Example internal interface:

```go
type Transport interface {
    Do(ctx context.Context, req *http.Request) (*http.Response, error)
}
```

The rest of the application should never manually construct authentication headers.

## 5.2 Layer 2 — generated OpenAPI client

Generated from:

```text
/api/openapi.json
```

Suggested location:

```text
internal/youtrack/gen/
```

Rules:

- generated files are never edited manually;
- generated files can be deleted and regenerated at any time;
- generated package names must not leak through stable public/internal interfaces;
- generated DTOs should not be used directly by Cobra commands;
- generated code should be pinned to a committed OAS snapshot for reproducible builds.

The exact generator should remain replaceable. Candidate approaches include OpenAPI Generator or a Go-focused OAS generator, as long as the generated client is performant and does not force runtime reflection-heavy behavior.

## 5.3 Layer 3 — compatibility / translation layer

This is the proposed **anti-corruption layer** between YouTrack's evolving REST surface and the CLI's stable model.

Suggested location:

```text
internal/youtrack/adapter/
```

Responsibilities:

- map generated request/response DTOs into stable internal domain types;
- hide endpoint renames and reorganizations;
- hide schema property renames;
- adapt optional/new fields;
- translate pagination mechanisms;
- normalize custom-field representations;
- select endpoint implementations based on server capabilities;
- provide compatibility fallbacks when supporting older YouTrack releases;
- map raw API errors to stable CLI errors.

Example:

```go
type IssueGateway interface {
    Get(ctx context.Context, id string, opts GetIssueOptions) (Issue, error)
    List(ctx context.Context, opts ListIssueOptions) (IssuePage, error)
    Create(ctx context.Context, input CreateIssueInput) (Issue, error)
    Update(ctx context.Context, id string, input UpdateIssueInput) (Issue, error)
    Delete(ctx context.Context, id string) error
    ApplyCommand(ctx context.Context, input ApplyCommandInput) error
}
```

The command layer depends on `IssueGateway`, not on `gen.ClientWithResponses` or equivalent generated types.

If JetBrains changes:

```text
/api/foo
```

to:

```text
/api/bar
```

ideally only the generated client and adapter need modification.

The user-facing command remains:

```bash
yt issue ...
```

## 5.4 Layer 4 — stable domain model

Suggested location:

```text
internal/domain/
```

Examples:

```go
type Issue struct {
    ID          string
    ReadableID  string
    Summary     string
    Description string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Reporter    *UserRef
    Assignee    *UserRef
    Fields      []CustomField
}
```

These types should be designed for CLI use, not generated from the server schema.

They should remain stable unless the CLI itself needs a semantic change.

## 5.5 Layer 5 — application services

Suggested location:

```text
internal/app/
```

Responsibilities:

- orchestrate multi-request workflows;
- apply business rules;
- resolve issue IDs;
- compile ergonomic flags into YouTrack query syntax;
- perform bulk operations;
- handle confirmation policy;
- coordinate output-neutral data operations.

Example:

```go
type IssueService struct {
    Issues IssueGateway
    Users  UserGateway
}
```

The service layer must not depend on Cobra.

## 5.6 Layer 6 — CLI commands

Suggested location:

```text
internal/cmd/
```

Responsibilities:

- parse flags/arguments;
- prompt when interactive;
- validate CLI syntax;
- invoke application services;
- choose renderer;
- set exit codes.

Commands should contain minimal API-specific logic.

---

# 6. API evolution strategy

## 6.1 Do not regenerate at runtime

The binary should **not** download the OpenAPI document and generate Go code when users invoke a command.

Reasons:

- expensive;
- unpredictable;
- requires compiler/generator tooling;
- harms startup performance;
- creates unreviewed runtime behavior;
- complicates reproducibility.

Code generation belongs in development/CI/release workflows.

## 6.2 Commit an OAS snapshot

Suggested repository layout:

```text
api/
├── openapi/
│   ├── youtrack.json
│   └── README.md
└── patches/
```

The repository should contain the exact OAS snapshot used for generation.

## 6.3 Automated schema update workflow

Provide a developer command such as:

```bash
make api-sync
```

or:

```bash
go generate ./...
```

Conceptual workflow:

```text
Download latest official OAS
          │
          ▼
Normalize / validate schema
          │
          ▼
Compare with committed schema
          │
          ├─ non-breaking additions
          │      └─ regenerate client
          │
          └─ breaking changes
                 └─ fail CI / require adapter review
          │
          ▼
Generate internal/youtrack/gen
          │
          ▼
Compile
          │
          ▼
Run adapter contract tests
          │
          ▼
Run CLI integration tests
```

The generated diff itself should not be the only review signal. CI should report semantic OAS changes such as:

- path added;
- path removed;
- method added/removed;
- required parameter changed;
- request property changed;
- response type changed;
- enum changed;
- property deprecated;
- authentication behavior changed.

## 6.4 Capability detection over scattered version checks

Avoid code such as:

```go
if version >= "2026.1" {
    ...
}
```

spread throughout the project.

Prefer:

```go
type Capabilities struct {
    OpenAPI              bool
    OAuthPKCE            bool
    UsersAPI             bool
    GroupsAPI            bool
    RolesAPI             bool
    OrganizationsAPI     bool
    ProjectTeamsAPI      bool
}
```

The compatibility layer chooses the implementation.

This matters because JetBrains moved many user/group/role/project-team/organization operations from Hub-oriented APIs into the YouTrack REST API starting with YouTrack 2026.1.

Official references:

- <https://www.jetbrains.com/help/youtrack/devportal/api-users-yt-vs-hub.html>
- <https://www.jetbrains.com/help/youtrack/devportal/hub-rest-api-deprecated-endpoints-2026-1.html>

That real migration is a good example of the kind of change the adapter layer should absorb.

## 6.5 Runtime capability discovery

A connected YouTrack instance exposes its own:

```text
/api/openapi.json
```

The CLI may use this at runtime for **capability discovery**, but should do so lazily and cache the result.

Potential approach:

1. At `yt auth login`, fetch the instance's OAS document once.
2. Compute a schema fingerprint/hash.
3. Extract only the capabilities relevant to the CLI.
4. Store non-secret capability metadata per host.
5. Refresh periodically or when an API call indicates a mismatch.

Example config metadata:

```yaml
hosts:
  acme.youtrack.cloud:
    url: https://acme.youtrack.cloud
    user: cristhofer
    auth: oauth
    capabilities:
      schema_hash: sha256:...
      checked_at: 2026-09-26T12:00:00Z
      oauth_pkce: true
      users_api: true
      groups_api: true
```

Do **not** download the entire schema on every CLI invocation.

## 6.6 Compatibility policy

The project should eventually declare an explicit compatibility policy, for example:

```text
Current YouTrack release: fully supported
Previous N major/minor releases: best-effort supported
Older releases: yt api may still work, but ergonomic wrappers are not guaranteed
```

The exact window should be decided after testing real Server deployments.

Prefer capabilities over version numbers wherever feasible.

---

# 7. Performance principles

Maintainability must not result in a slow CLI.

## 7.1 Startup

Avoid on every invocation:

- downloading OpenAPI;
- loading huge schemas;
- reflection-based endpoint discovery;
- unnecessary update checks;
- unnecessary authentication probes;
- network calls before argument validation.

Commands such as:

```bash
yt version
yt completion zsh
yt help
```

should require no network access.

## 7.2 HTTP

Use one configured `http.Client` / `http.Transport` per invocation and allow connection reuse for multi-request workflows.

Use streaming for:

- attachment uploads;
- attachment downloads;
- large API responses where practical.

## 7.3 Data requests

YouTrack requires callers to explicitly request response fields for many API operations.

The CLI should exploit this for performance by requesting only what each operation needs.

For example, `yt issue list` should not request full descriptions, all comments, attachments, activities, and custom-field metadata unless needed.

Official reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-fields-syntax.html>

## 7.4 Generated client

Generated types and request methods are preferred for hot API paths because they provide compile-time validation and avoid a generic reflection-heavy runtime dispatch layer.

The raw `yt api` command can remain generic because it is explicitly the low-level escape hatch.

---

# 8. Host model

Unlike `gh`, there should be no assumption that every user points at one global SaaS host.

A YouTrack service URL may look like:

```text
https://acme.youtrack.cloud
https://youtrack.acme.internal
https://tools.acme.com/youtrack
https://example.myjetbrains.com/youtrack
```

YouTrack REST API base URL:

```text
<service-url>/api
```

The user should provide the **service URL**, not `/api`.

Example:

```bash
yt auth login --host https://acme.youtrack.cloud
```

Normalization:

```text
input:
https://acme.youtrack.cloud/

canonical service URL:
https://acme.youtrack.cloud

REST API URL:
https://acme.youtrack.cloud/api

OAS URL:
https://acme.youtrack.cloud/api/openapi.json
```

Official reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-url-and-endpoints.html>

---

# 9. Authentication

Support at least two main authentication modes.

## 9.1 Permanent tokens

Interactive:

```bash
yt auth login
```

```text
? YouTrack URL: https://acme.youtrack.cloud
? Authentication method:
  > Log in with browser
    Paste permanent token
```

Explicit:

```bash
yt auth login \
  --host https://acme.youtrack.cloud \
  --with-token
```

The token should be read from stdin or a secure prompt, not passed as a normal positional argument where it may appear in shell history or process listings.

CI/environment variables:

```bash
export YT_HOST=https://acme.youtrack.cloud
export YT_TOKEN='perm:...'
```

After receiving credentials, verify them with a lightweight authenticated request such as current-user retrieval.

YouTrack recommends permanent tokens for REST integrations.

Reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-log-in-to-youtrack.html>

## 9.2 Browser login / OAuth

YouTrack 2026.2 supports OAuth 2.0 Authorization Code with PKCE for public clients.

Proposed UX:

```bash
yt auth login --web
```

Conceptual flow:

```text
yt
 │
 ├─ generate state
 ├─ generate PKCE verifier
 ├─ derive challenge
 ├─ start temporary loopback HTTP listener
 │
 ├─ open browser
 │
 ▼
YouTrack authorization page
 │
 ▼
http://127.0.0.1:<port>/oauth/callback?code=...
 │
 ▼
yt exchanges code + verifier
 │
 ▼
credential stored securely
```

The important difference from `gh` is that GitHub controls both GitHub.com and the official GitHub CLI OAuth application. An unofficial YouTrack CLI may need an OAuth client to be registered on each target installation.

Therefore browser login should be designed to support:

1. manually configured OAuth client IDs;
2. public clients + PKCE;
3. automatic client registration mechanisms where supported/configured by the YouTrack administrator.

OAuth should not block the first usable release if permanent-token login is already complete.

References:

- <https://www.jetbrains.com/help/youtrack/devportal/OAuth-authorization-in-youtrack.html>
- <https://www.jetbrains.com/help/youtrack/devportal/Authorization-Code.html>
- <https://www.jetbrains.com/help/youtrack/cloud/oauth-clients.html>

---

# 10. Credential storage

Use native credential storage:

```text
macOS    → Keychain
Windows  → Windows Credential Manager
Linux    → Secret Service / compatible keyring
```

Sensitive tokens should not normally be stored in the plain config file.

If secure storage is unavailable, default behavior should be explicit failure:

```text
Error: no supported secure credential store is available.

Use YT_TOKEN for ephemeral authentication,
or explicitly opt into insecure local credential storage.
```

Possible explicit override:

```bash
yt auth login --insecure-storage
```

Do not silently downgrade from secure storage to plaintext.

---

# 11. Multi-instance support

The CLI should support multiple YouTrack hosts from day one.

```bash
yt auth status
```

Example:

```text
acme.youtrack.cloud
  ✓ Logged in as cristhofer
  ✓ Active host

bugs.example.net
  ✓ Logged in as cnavarro
```

Possible command:

```bash
yt auth switch --host bugs.example.net
```

Environment override:

```bash
YT_HOST=https://bugs.example.net yt issue list
```

Config stores metadata, not secrets:

```yaml
hosts:
  acme.youtrack.cloud:
    url: https://acme.youtrack.cloud
    user: cristhofer
    auth: oauth

  bugs.example.net:
    url: https://bugs.example.net
    user: cnavarro
    auth: token

defaults:
  host: acme.youtrack.cloud
```

---

# 12. Raw API command

`yt api` is one of the most important commands in the project.

It should provide low-level authenticated access to any REST endpoint without waiting for a specialized command to be implemented.

Examples:

```bash
yt api /issues
```

```bash
yt api /issues/APP-123 \
  --fields 'id,idReadable,summary'
```

```bash
yt api /commands \
  --method POST \
  --input command.json
```

Target options inspired by `gh api`:

```text
-X, --method
-H, --header
-f, --raw-field
-F, --field
    --fields
    --input
    --paginate
    --slurp
    --include
    --silent
    --host
    --jq
    --template
```

`--fields` is a YouTrack-specific convenience that maps naturally to YouTrack's `fields=` query parameter.

The raw API command should also work with custom extension endpoints installed by YouTrack apps.

YouTrack custom endpoints live under `/api/.../extensionEndpoints/...`.

Reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-url-and-endpoints.html>

---

# 13. First-class resource commands

YouTrack is issue-centric, but its REST API covers considerably more than issues.

Useful resource families include:

- issues;
- comments;
- attachments;
- issue links;
- activities;
- work items/time tracking;
- commands;
- tags;
- saved queries;
- projects;
- custom fields;
- bundles;
- agile boards;
- sprints;
- knowledge-base articles;
- users;
- groups;
- organizations;
- roles and permissions;
- global settings;
- backup-related resources;
- other administrative resources.

Reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-resources.html>

Initial human-facing hierarchy:

```text
yt
│
├── auth
│   ├── login
│   ├── logout
│   ├── status
│   └── switch
│
├── issue
│   ├── list
│   ├── view
│   ├── create
│   ├── edit
│   ├── delete
│   ├── comment
│   ├── attach
│   ├── link
│   ├── unlink
│   ├── command
│   ├── history
│   └── open
│
├── project
│   ├── list
│   ├── view
│   ├── create
│   └── edit
│
├── agile
│   ├── list
│   ├── view
│   └── sprint
│
├── article
│   ├── list
│   ├── view
│   ├── create
│   └── edit
│
├── work-item
│   ├── list
│   ├── add
│   ├── edit
│   └── delete
│
├── tag
│   ├── list
│   ├── create
│   └── delete
│
├── user
├── group
├── role
├── organization
│
├── api
├── config
├── alias
├── completion
└── version
```

Administrative APIs can remain accessible through `yt api` until real demand justifies ergonomic wrappers.

---

# 14. Issue workflows

`issue` should receive the largest amount of product/UX attention.

## 14.1 List

```bash
yt issue list
```

Example output:

```text
ID        STATE        PRIORITY   ASSIGNEE     SUMMARY
APP-248   Open         Critical   cristhofer   Login fails with SSO
APP-241   In Progress  Major      maria        Migrate API endpoint
APP-233   Open         Normal     —            Improve dashboard
```

## 14.2 Native YouTrack query language

YouTrack already provides an expressive query language. Preserve it directly.

```bash
yt issue list --query 'project: APP for: me #Unresolved'
```

Short form:

```bash
yt issue list -q 'project: APP for: me #Unresolved'
```

Convenience flags may compile to YouTrack query syntax:

```bash
yt issue list --project APP --assignee me
```

but raw query support must remain first-class.

Reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-query-syntax.html>

## 14.3 View

```bash
yt issue view APP-123
```

Browser:

```bash
yt issue view APP-123 --web
```

## 14.4 Create

Interactive:

```bash
yt issue create
```

```text
? Project: APP
? Summary: OAuth callback fails
? Description: <Open editor>
```

Non-interactive:

```bash
yt issue create \
  --project APP \
  --summary 'OAuth callback fails' \
  --description-file description.md
```

## 14.5 Edit

```bash
yt issue edit APP-123 --summary 'Updated summary'
```

## 14.6 Comments

```bash
yt issue comment APP-123 --body 'Reproduced on 2026.2'
```

## 14.7 Attachments

```bash
yt issue attach APP-123 screenshot.png log.txt
```

Uploads should stream files instead of reading entire files into memory.

---

# 15. YouTrack commands as a first-class feature

YouTrack's command system is unique and powerful enough to deserve a dedicated CLI operation.

Examples:

```bash
yt issue command APP-123 'State Fixed'
```

```bash
yt issue command APP-123 APP-124 \
  'Assignee me Priority Critical'
```

Bulk query-driven operation:

```bash
yt issue command \
  --query 'project: APP Priority: Critical #Unresolved' \
  'Assignee me'
```

Interactive safety:

```text
The following 7 issues will be modified:

APP-113
APP-121
APP-128
...

Apply `Assignee me`? [y/N]
```

Automation:

```bash
yt issue command \
  --query 'project: APP Priority: Critical #Unresolved' \
  'Assignee me' \
  --yes
```

YouTrack exposes `/api/commands` for applying commands to issues.

Reference:

- <https://www.jetbrains.com/help/youtrack/devportal/resource-api-commands.html>

---

# 16. Output model

Match the expectations established by mature engineering CLIs.

## 16.1 Human output

```bash
yt issue list
```

prints an aligned table when stdout is a terminal.

## 16.2 Pipe-safe behavior

```bash
yt issue list | grep OAuth
```

should not emit ANSI color/control noise unless explicitly requested.

## 16.3 JSON

```bash
yt issue list --json idReadable,summary,created
```

## 16.4 `jq`

```bash
yt issue list \
  --json idReadable,summary \
  --jq '.[] | select(.idReadable == "APP-123")'
```

## 16.5 Templates

```bash
yt issue list \
  --json idReadable,summary \
  --template '{{range .}}{{.idReadable}} {{.summary}}{{"\n"}}{{end}}'
```

The `--json`, `--jq`, and Go-template conventions are intentionally familiar to `gh` users.

GitHub CLI formatting reference:

- <https://cli.github.com/manual/gh_help_formatting>

---

# 17. YouTrack `fields` handling

YouTrack requires clients to explicitly request fields for many API responses.

This is a major YouTrack-specific concern.

Human-facing commands should provide carefully chosen defaults:

```bash
yt issue view APP-123
```

Low-level override:

```bash
yt issue view APP-123 \
  --fields 'idReadable,summary,reporter(login),customFields(name,value(name))'
```

Raw API:

```bash
yt api /issues/APP-123 \
  --fields 'idReadable,summary'
```

Internally, default field projections should live centrally so they can evolve without scattering strings through commands.

Suggested package:

```text
internal/youtrack/fields/
```

---

# 18. Pagination

YouTrack collection endpoints commonly use `$skip` and `$top`. Some activity APIs use cursor-style pagination.

The transport/adapter layer should normalize pagination so commands do not reimplement it.

Examples:

```bash
yt issue list --limit 30
```

```bash
yt issue list --limit 200
```

Possible convention:

```bash
yt issue list --limit 0
```

means fetch all pages.

Raw API:

```bash
yt api /issues --paginate
```

Reference:

- <https://www.jetbrains.com/help/youtrack/devportal/api-concept-pagination.html>

---

# 19. Browser integration

Examples:

```bash
yt issue view APP-123 --web
```

```bash
yt issue open APP-123
```

Potentially:

```bash
yt project view APP --web
yt agile view BOARD --web
yt article view KB-A-12 --web
```

Browser invocation should be abstracted behind an injectable interface for testability.

---

# 20. Editor integration

Interactive creation/edit flows should support an external editor.

Suggested precedence:

```text
YT_EDITOR
GIT_EDITOR
VISUAL
EDITOR
```

Example:

```bash
yt issue create
```

may prompt:

```text
? Description: <Open editor>
```

Non-interactive users can provide files/flags instead.

---

# 21. Aliases

Potential commands:

```bash
yt alias set mine 'issue list --query "for: me #Unresolved"'
```

Then:

```bash
yt mine
```

Initial scope should favor command aliases. Shell aliases that execute arbitrary shell code may be deferred because they increase security and portability complexity.

---

# 22. Shell completion

Cobra can support:

```bash
yt completion bash
yt completion zsh
yt completion fish
yt completion powershell
```

Later, dynamic completion can use YouTrack APIs:

```bash
yt issue view APP-<TAB>
yt issue create --project <TAB>
yt issue command APP-1 'Assignee <TAB>'
```

YouTrack exposes command/search suggestion resources, which may eventually enable unusually strong context-sensitive completion.

---

# 23. Proposed Go repository layout

```text
yt/
├── cmd/
│   └── yt/
│       └── main.go
│
├── api/
│   ├── openapi/
│   │   ├── youtrack.json
│   │   └── README.md
│   └── patches/
│
├── internal/
│   ├── cmd/
│   │   ├── root/
│   │   ├── auth/
│   │   │   ├── login/
│   │   │   ├── logout/
│   │   │   └── status/
│   │   ├── issue/
│   │   │   ├── list/
│   │   │   ├── view/
│   │   │   ├── create/
│   │   │   ├── edit/
│   │   │   └── command/
│   │   ├── project/
│   │   ├── agile/
│   │   ├── article/
│   │   ├── workitem/
│   │   ├── api/
│   │   ├── config/
│   │   ├── alias/
│   │   └── completion/
│   │
│   ├── app/
│   │   ├── issues.go
│   │   ├── projects.go
│   │   └── commands.go
│   │
│   ├── domain/
│   │   ├── issue.go
│   │   ├── project.go
│   │   ├── user.go
│   │   └── errors.go
│   │
│   ├── youtrack/
│   │   ├── gen/           # generated; never hand-edit
│   │   ├── adapter/       # stable translation/compatibility layer
│   │   ├── transport/
│   │   ├── fields/
│   │   ├── pagination/
│   │   └── capabilities/
│   │
│   ├── auth/
│   │   ├── token.go
│   │   ├── oauth.go
│   │   ├── pkce.go
│   │   ├── browser.go
│   │   └── keyring.go
│   │
│   ├── config/
│   │   ├── config.go
│   │   └── hosts.go
│   │
│   ├── iostreams/
│   ├── tableprinter/
│   ├── formatter/
│   ├── browser/
│   └── editor/
│
├── scripts/
│   └── sync-openapi.sh
│
├── go.mod
├── go.sum
├── LICENSE
├── README.md
└── SPEC.md
```

---

# 24. Dependency injection / command construction

Borrow the successful `gh` pattern of injecting IO/config/API/browser/editor dependencies instead of allowing every command to access globals.

Concept:

```go
type Factory struct {
    IO          *iostreams.IOStreams
    Config      func() (*config.Config, error)
    Issues      func() (IssueGateway, error)
    Projects    func() (ProjectGateway, error)
    RawAPI      func() (*RawClient, error)
    Browser     browser.Browser
    Editor      editor.Editor
}
```

Benefits:

- deterministic unit tests;
- no real network calls in command tests;
- easier mocking;
- easier host switching;
- clear ownership of side effects;
- reusable services outside Cobra.

The exact names do not need to copy `gh`, but the principle is valuable.

GitHub CLI repository:

- <https://github.com/cli/cli>

GitLab CLI repository:

- <https://gitlab.com/gitlab-org/cli>

---

# 25. OpenAPI should not design the CLI

The OAS document is an excellent transport contract, but a poor product-design source if followed mechanically.

For example, a deeply nested REST resource should not automatically produce a command like:

```text
yt admin custom-field-settings bundles version values create
```

A human-friendly abstraction might instead be:

```text
yt field version add-value ...
```

or the operation may remain available only through:

```bash
yt api ...
```

Rule:

> OpenAPI drives generated transport/client code. Product workflows drive CLI command names.

---

# 26. Custom fields

YouTrack custom fields are likely to be one of the most complex API areas because values are polymorphic and installation-specific.

The compatibility layer should normalize custom fields into a stable internal representation while retaining raw values when necessary.

Possible model:

```go
type CustomField struct {
    Name      string
    Type      string
    Value     any
    Raw       json.RawMessage
}
```

Ergonomic command syntax can be layered on later, for example:

```bash
yt issue edit APP-123 \
  --field 'Priority=Critical' \
  --field 'Assignee=me'
```

Internally, project field metadata may be required to serialize the correct REST representation.

This should be centralized in a custom-field codec package rather than implemented separately by each command.

---

# 27. Error model

Normalize server/transport failures into stable CLI errors.

Suggested categories:

```text
AuthenticationError
AuthorizationError
NotFoundError
ValidationError
ConflictError
UnsupportedCapabilityError
NetworkError
ServerError
APIContractError
```

Output should distinguish between:

```text
HTTP/API diagnostics      → stderr
requested command data    → stdout
```

This is essential for scripting.

Possible exit code policy:

```text
0   success
1   general command/API failure
2   usage/argument error
3   authentication failure
4   authorization failure
5   requested resource not found
```

Exact codes should be finalized before v1 and then treated as a compatibility contract.

---

# 28. Debugging / diagnostics

Provide a global debug mode:

```bash
yt --verbose issue list
```

or:

```bash
YT_DEBUG=1 yt issue list
```

Debug output may include:

- resolved host;
- request method/path;
- response status;
- duration;
- pagination decisions;
- capability decisions;
- request ID headers;
- retries.

Never print:

- bearer tokens;
- OAuth authorization codes;
- PKCE verifier;
- secret headers;
- sensitive request bodies without explicit unsafe debugging controls.

---

# 29. Testing strategy

## 29.1 Unit tests

Test:

- query compilation;
- field projection building;
- URL normalization;
- pagination;
- output formatting;
- error conversion;
- credential-selection rules;
- capability selection;
- custom-field codecs.

## 29.2 Command tests

Inject fake services and fake IO.

Assert:

- parsed arguments;
- stdout;
- stderr;
- exit behavior;
- interactive prompts;
- confirmation safeguards.

## 29.3 Adapter contract tests

This is critical for maintenance.

For each stable gateway operation, test that the adapter correctly maps generated API objects into internal domain objects.

When the OAS document changes and generated code is regenerated, these tests identify exactly which semantic contracts changed.

## 29.4 Integration tests

Run against disposable/test YouTrack instances where practical.

Cover:

- permanent-token auth;
- OAuth flow components;
- issue CRUD;
- commands;
- pagination;
- attachments;
- project listing;
- custom fields;
- older supported server releases if available.

## 29.5 Golden output tests

Use golden files sparingly for stable human-readable rendering.

Machine-readable JSON behavior should be asserted structurally.

---

# 30. Security principles

1. Never store credentials in repository/config by default.
2. Prefer OS keyrings.
3. Never include tokens in command-line arguments when avoidable.
4. Redact authorization headers from logs.
5. Use OAuth state validation.
6. Use PKCE for public-client browser login.
7. Bind OAuth callback listener to loopback only.
8. Prefer random available callback ports if supported by configured redirect policy.
9. Require explicit confirmation for destructive/bulk operations in interactive terminals.
10. Provide `--yes` only for intentional automation.
11. Never silently downgrade secure credential storage to plaintext.

---

# 31. Proposed v1 command surface

A reasonable first major target:

```text
yt auth
    login
    logout
    status
    switch

yt issue
    list
    view
    create
    edit
    delete
    comment
    command
    link
    attach
    history
    open

yt project
    list
    view

yt agile
    list
    view

yt article
    list
    view

yt work-item
    list
    add
    edit
    delete

yt tag
    list

yt api

yt config
    get
    set
    list

yt alias
    list
    set
    delete

yt completion

yt version
```

Global flags:

```text
--host
--help
--version
--verbose
--no-color
```

Common read-command flags:

```text
--json
--jq
--template
--web
--fields
```

Common list flags:

```text
--limit
--query
```

---

# 32. Suggested implementation phases

## Phase 0 — foundation

- repository/bootstrap;
- license;
- Cobra root command;
- IO abstraction;
- config abstraction;
- host normalization;
- HTTP transport;
- error model;
- OpenAPI snapshot/generation pipeline;
- adapter interfaces;
- testing harness.

Deliverable:

```bash
yt version
yt help
```

## Phase 1 — auth + raw API

- permanent-token login;
- OS keyring;
- environment credentials;
- multiple hosts;
- `auth status`;
- `auth logout`;
- `yt api`;
- `--fields`;
- raw pagination;
- JSON/JQ/template infrastructure.

This phase already creates a generally useful CLI because every REST endpoint is reachable through `yt api`.

## Phase 2 — issues

- list;
- view;
- create;
- edit;
- comments;
- command;
- attachments;
- issue links;
- history;
- `--web`;
- interactive editor.

## Phase 3 — YouTrack-native power features

- query compiler/sugar flags;
- bulk issue commands;
- command suggestions;
- dynamic completion;
- custom-field metadata/codec;
- saved-query integration.

## Phase 4 — broader resources

- projects;
- agile boards/sprints;
- work items;
- articles;
- tags;
- users/groups/roles/organizations as appropriate.

## Phase 5 — browser OAuth

- PKCE;
- loopback callback server;
- OAuth client configuration;
- automatic client registration where reliably supported;
- credential refresh behavior where applicable.

OAuth can move earlier if it proves straightforward, but token auth should not be delayed by OAuth complexity.

## Phase 6 — compatibility automation

- runtime capability cache;
- schema fingerprinting;
- semantic OAS diff CI;
- compatibility fixtures for multiple YouTrack releases;
- automated dependency/API update PRs.

---

# 33. Update workflow when JetBrains changes the REST API

The desired maintenance experience should be close to:

```bash
make api-sync
```

Then one of three outcomes:

### A. Additive API change

```text
New endpoint/field
    ↓
generated client changes
    ↓
existing adapters compile unchanged
    ↓
no CLI changes required
```

### B. Compatible schema change needed by a feature

```text
OAS changed
    ↓
generated client changes
    ↓
one adapter mapping changes
    ↓
commands/services remain unchanged
```

### C. Major semantic/API redesign

```text
endpoint semantics changed
    ↓
generated layer changes
    ↓
compatibility adapter changes
    ↓
possibly service/domain changes
    ↓
CLI command changes only if user-facing semantics truly changed
```

That is the intended payoff of the translation layer.

A major upstream change may still require work, but it should not force a full command-tree rewrite.

---

# 34. Why the adapter layer is preferable to direct generated-client usage

Without the adapter:

```text
Cobra command
   ↓
Generated OpenAPI API type
   ↓
YouTrack
```

An upstream rename can touch dozens of commands.

With the adapter:

```text
Cobra command
   ↓
Stable IssueService
   ↓
Stable IssueGateway
   ↓
YouTrackAdapter2026
   ↓
Generated client
```

The change radius is much smaller.

This also makes it possible to support multiple YouTrack server generations simultaneously:

```text
IssueGateway
   ├── ModernYouTrackAdapter
   └── LegacyYouTrackAdapter
```

without duplicating the CLI UX.

---

# 35. What should and should not be generated

## Generate

- endpoint request builders;
- wire DTOs;
- enum constants where reliable;
- query/path parameter encoding;
- response decoding;
- basic OpenAPI validation helpers if useful.

## Do not generate

- Cobra commands;
- command names;
- user-facing flags;
- tables;
- prompts;
- domain models;
- error messages;
- bulk workflow behavior;
- auth UX;
- project configuration conventions;
- compatibility policy.

Those are product decisions and should remain hand-designed.

---

# 36. CLI UX patterns to borrow from `gh` / `glab`

Useful patterns:

```text
auth login/status/logout
host profiles
Options/Factory dependency injection
Cobra hierarchy
IO abstractions
TTY-aware formatting
--json / --jq / --template
raw `api` command
config conventions
environment overrides
native keyring integration
browser launching
editor integration
aliases
completion
central pagination
high-quality command help
stable scripting behavior
```

Do not copy Git-specific assumptions:

```text
repo discovery
branches
PR creation/merge
GitHub GraphQL architecture
Git credential integration
Git remote inference
GitHub-specific Device Flow assumptions
repository-scoped default context
```

---

# 37. Licensing and attribution

GitHub CLI and GitLab CLI are open-source projects under permissive licensing, but code reuse must comply with their respective license notices and attribution requirements.

If the project copies substantial source code, preserve applicable notices.

Prefer architectural/UX inspiration over importing large GitHub/GitLab-specific subsystems unless the copied code is genuinely generic.

The CLI should clearly identify itself as unofficial, for example:

```text
An unofficial command-line interface for JetBrains YouTrack.
```

Avoid branding that implies JetBrains endorsement or official status.

Repositories:

- GitHub CLI: <https://github.com/cli/cli>
- GitLab CLI: <https://gitlab.com/gitlab-org/cli>

---

# 38. Open questions

These should be resolved as implementation begins.

1. What is the minimum supported YouTrack Server version?
2. Should the executable be named `yt`, given possible naming collisions in package managers?
3. Which OpenAPI generator produces the cleanest and fastest Go client for YouTrack's schema?
4. How should polymorphic custom fields be represented internally?
5. Should `--limit 0` mean unlimited, or should `--limit all` be used instead?
6. Should OAuth be part of v1 or v1.x?
7. What automatic OAuth client-registration behavior is reliable across Cloud and Server?
8. Should config use YAML, TOML, or a `gh`-compatible conceptual layout?
9. Should `yt api` accept both `/issues` and `/api/issues`, normalizing either form?
10. Should first-class admin commands be part of v1 or remain `yt api` only?
11. What compatibility window should be promised for self-hosted YouTrack Server?
12. Which schema-diff tool should gate OpenAPI updates in CI?
13. Should runtime OAS capability metadata be refreshed by TTL, schema fingerprint, explicit `yt auth refresh`, or error-triggered retry?
14. Should an extension/plugin system similar to `gh extension` ever be supported?
15. Should a local disk cache be used for project/field metadata to accelerate completion and repeated commands?

---

# 39. Initial architectural decisions to treat as strong defaults

Unless implementation evidence suggests otherwise:

1. **Go + Cobra** for the executable.
2. **OpenAPI-generated wire client**, committed/reproducible.
3. **Generated code is disposable.**
4. **Stable adapter/gateway layer between generated code and services.**
5. **Stable CLI-specific domain types.**
6. **Commands never depend directly on generated DTOs.**
7. **`yt api` guarantees API escape-hatch coverage.**
8. **First-class commands are curated workflows, not endpoint mirrors.**
9. **Permanent token auth first; OAuth PKCE added cleanly behind the same credential abstraction.**
10. **OS keyring by default; no silent plaintext fallback.**
11. **Multiple YouTrack hosts supported from the beginning.**
12. **Native YouTrack query syntax remains first-class.**
13. **YouTrack commands remain first-class.**
14. **Use runtime capability discovery lazily, not on every startup.**
15. **Centralize `fields`, pagination, errors, custom-field codecs, and host normalization.**
16. **Optimize for both interactive UX and stable automation.**

---

# 40. Relevant official documentation

## YouTrack

- REST API overview  
  <https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html>

- REST API reference  
  <https://www.jetbrains.com/help/youtrack/devportal/rest-api-reference.html>

- OpenAPI Specification  
  <https://www.jetbrains.com/help/youtrack/devportal/youtrack-openapi-specification.html>

- REST API URL and endpoints  
  <https://www.jetbrains.com/help/youtrack/devportal/api-url-and-endpoints.html>

- REST API resources  
  <https://www.jetbrains.com/help/youtrack/devportal/api-resources.html>

- Fields syntax  
  <https://www.jetbrains.com/help/youtrack/devportal/api-fields-syntax.html>

- Query syntax  
  <https://www.jetbrains.com/help/youtrack/devportal/api-query-syntax.html>

- Pagination  
  <https://www.jetbrains.com/help/youtrack/devportal/api-concept-pagination.html>

- Authentication  
  <https://www.jetbrains.com/help/youtrack/devportal/api-log-in-to-youtrack.html>

- OAuth authorization  
  <https://www.jetbrains.com/help/youtrack/devportal/OAuth-authorization-in-youtrack.html>

- Authorization Code flow  
  <https://www.jetbrains.com/help/youtrack/devportal/Authorization-Code.html>

- OAuth clients  
  <https://www.jetbrains.com/help/youtrack/cloud/oauth-clients.html>

- API commands resource  
  <https://www.jetbrains.com/help/youtrack/devportal/resource-api-commands.html>

- Users/groups/access management  
  <https://www.jetbrains.com/help/youtrack/devportal/api-users-yt-vs-hub.html>

- Hub endpoint changes in YouTrack 2026.1  
  <https://www.jetbrains.com/help/youtrack/devportal/hub-rest-api-deprecated-endpoints-2026-1.html>

## Reference CLIs

- GitHub CLI repository  
  <https://github.com/cli/cli>

- GitHub CLI manual  
  <https://cli.github.com/manual/>

- GitLab CLI repository  
  <https://gitlab.com/gitlab-org/cli>

---

# 41. Summary

The maintainable architecture is not:

```text
CLI → hundreds of hand-written raw endpoint calls
```

and it is not:

```text
CLI → generated OpenAPI client everywhere
```

It should be:

```text
                         ┌───────────────────┐
                         │  CLI / Cobra UX   │
                         └─────────┬─────────┘
                                   │
                         ┌─────────▼─────────┐
                         │ Application logic │
                         └─────────┬─────────┘
                                   │
                     stable internal interfaces
                                   │
                         ┌─────────▼─────────┐
                         │ Adapter / compat  │
                         └─────────┬─────────┘
                                   │
                         generated wire API
                                   │
                         ┌─────────▼─────────┐
                         │ HTTP + auth layer │
                         └─────────┬─────────┘
                                   │
                              YouTrack
```

This gives the project both sides of the desired tradeoff:

- **ergonomic, carefully designed commands for humans**;
- **low maintenance when JetBrains evolves the API**.

The OpenAPI-generated client absorbs mechanical API changes. The compatibility layer absorbs semantic REST changes. The service and command layers remain stable unless the user-facing behavior actually needs to change.

That should be the core architectural constraint of the project from its first commit.
