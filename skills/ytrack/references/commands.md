# YouTrack commands

`ytrack issue command <id>... '<command>'` applies a command, as the command
dialog in the YouTrack web UI does. The last argument is the command; every
argument before it is an issue, and all of them change in one request.

## Safe workflow

1. Preview: `ytrack issue command APP-123 'State Fixed' --dry-run`. It prints
   how YouTrack parsed each part and exits 1 when a part is not understood.
2. Apply the same command without `--dry-run`. Add `--comment '<text>'` to
   explain the change (everyone who can see the issue sees the comment) and
   `--silent` to skip notifications when the user asks for it.
3. Check the result: `ytrack issue view APP-123 --json state,assignee,customFields`.

`--dry-run` cannot be combined with `--comment` or `--silent`.

## Write field names, not keywords

YouTrack translates command keywords (`for`, `tag`, `add`, `remove`, ...)
into the instance's language, so `for me` fails on a Spanish instance with
"Comando desconocido: for". A field name followed by a value works on every
locale:

| Instead of | Write |
| --- | --- |
| `for me` | `Assignee me` |
| `Fixed` | `State Fixed` |
| `Critical` | `Priority Critical` |

Field names are the project's own; read them from
`ytrack issue view APP-123 --json customFields`. Values with spaces need no
quotes inside the command: `State In Progress`.

## When to use a command instead of `ytrack issue edit`

`ytrack issue edit` writes through the REST API and needs no keywords:
`--field 'Name=Value'`, `--assignee`, `--add-tag`, `--remove-tag`,
`--summary` and `--description`. Use `ytrack issue command` for:

- several issues in one request: `ytrack issue command APP-1 APP-2 APP-3 'State Fixed'`
- adding or removing one value of a multi-value field without replacing the
  others (`add Fix versions 2026.3`, `remove Fix versions 2026.2`; keywords,
  so `--dry-run` first)
- links between issues (`relates to APP-7`, `duplicates APP-9`; keywords)
- anything else the YouTrack command dialog can do

Reference: https://www.jetbrains.com/help/youtrack/cloud/commands.html
