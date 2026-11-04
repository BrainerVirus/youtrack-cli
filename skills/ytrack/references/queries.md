# YouTrack search queries

`ytrack issue list -q '<query>'` and `ytrack api /issues -X GET -f query='<query>'`
send the query to YouTrack unchanged. Quote the whole query in single quotes:
it contains spaces, `#` and braces.

## Terms

| Query | Meaning |
| --- | --- |
| `project: APP` | issues in project APP (short name or name) |
| `#Unresolved` / `#Resolved` | not resolved / resolved |
| `for: me` | assigned to you (`Assignee: me` is the field-name form) |
| `Assignee: jdoe` | assigned to login jdoe |
| `Assignee: Unassigned` | nobody assigned |
| `reporter: me` | reported by you |
| `State: Open` | a field value; field names are the project's own |
| `State: {In Progress}` | braces around a value with spaces |
| `State: -Fixed` | any state except Fixed |
| `State: Open, {In Progress}` | either value |
| `Priority: Critical` | a field value |
| `tag: regression` | has the tag (`tag: {needs review}` with spaces) |
| `created: 2026-10-01 .. 2026-10-31` | date range |
| `updated: Today`, `updated: {This week}` | relative dates |
| `summary: login` | word in the summary |
| `login error` | free text anywhere in the issue |
| `issue id: APP-123` | one issue |
| `has: attachments`, `has: -Assignee` | has / lacks a value |
| `sort by: updated desc` | ordering (also `ytrack issue list --sort 'updated desc'`) |

Terms separated by spaces must all match (AND). `or` and parentheses combine
alternatives: `project: APP (State: Open or State: {In Progress})`.

## Examples

```sh
ytrack issue list -q 'project: APP for: me #Unresolved sort by: updated desc'
ytrack issue list -q 'project: APP State: {In Progress} updated: {This week}'
ytrack issue list -q 'project: APP tag: regression created: 2026-10-01 .. 2026-10-31' --all
ytrack issue list -q 'project: APP #Unresolved' --limit 0 --json idReadable,summary,assignee
```

`--project`, `--assignee`, `--state` and `--sort` on `ytrack issue list` add
these terms for you: `--project APP --assignee me --state 'In Progress'`
becomes `project: APP for: me State: {In Progress}`.

When a query returns nothing or fails, check the field and value names on a
real issue (`ytrack issue view APP-123 --json customFields`): on a
non-English instance or a customized project, `State` may have another name.
