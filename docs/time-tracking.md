# Time tracking (work items)

```sh
ytrack work-item list APP-123 --author me --since 2026-10-01
ytrack work-item list APP-123 --json id,date,duration --jq 'map(.duration.minutes) | add'
ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
ytrack work-item add APP-123 --duration 1.5h --date 2026-10-06 --type Development
ytrack work-item add APP-123 --duration 45m --type 117-0         # a type's id
ytrack work-item add TEAM-1 --duration 30m --meeting             # text "Meetings"
ytrack work-item edit APP-123 115-3 --duration 2h --text ''      # '' clears the text
ytrack work-item delete APP-123 115-3 --yes
```

The project needs time tracking turned on in its settings.

- `--duration` takes `1h30m`, `1h 30m`, `90m`, `1.5h`, `2 hours 15 minutes`
  or a bare number of minutes. Days and weeks are refused, because their
  length depends on the instance's work schedule.
- `--date auto` (the default for `add`) is today in your work timezone:
  `timezone: Europe/Madrid` in `config.yml` when set, otherwise the system
  timezone (`TZ`). YouTrack stores the day only, at midnight UTC.
- `--type` takes a work item type from the project's time tracking settings,
  by name (case is ignored) or by id. Names follow the instance's language
  and ids do not, so a script that must run on instances in several languages
  should use the id. An unknown value lists every valid name with its id.
- `--meeting` logs meeting time: the text defaults to `Meetings`; a `--text`
  or `--type` you give still applies.
- `list` filters with `--author` (a login or `me`), `--since` and `--until`
  (`YYYY-MM-DD`, inclusive).
- `edit` changes only what you pass. `edit` and `delete` refuse unless the
  work item belongs to the issue you named; take its ID from
  `ytrack work-item list APP-123 --json id`. `delete` asks first on a
  terminal and needs `--yes` otherwise.

`--json` fields: `id`, `date`, `duration` (`{minutes, presentation}`),
`author`, `type`, `text`, `issue` and `url`.
