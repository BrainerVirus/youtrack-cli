# Attachments

```sh
ytrack issue attach APP-123 dump.sql repro.sh       # attach to the issue: name<TAB>id per file
ytrack issue comment APP-123 --body 'Repro: ![](shot.png)' --attach shot.png --attach trace.html
ytrack issue attachments APP-123                    # files on the issue and its comments
ytrack issue download APP-123 -o out/               # save them all: name<TAB>bytes per file
ytrack issue download APP-123 trace.log 8-123 --comment 4-17 --clobber
```

## Upload

Files go up with `--attach` (comment) or `ytrack issue attach` (issue), never
through `ytrack api` multipart or curl. In a comment, `![](name)` shows an
attached image inline and `[text](name)` links to an attached file; `name` is
the file's base name. A missing file fails before anything is posted.

Once the comment is posted, never run the command again, even after a
warning or exit 1: the comment and the uploads exist, and a rerun posts a
duplicate. A warning that a file is on the issue, not the comment, still
means the comment shows it; exit 1 names the files that are neither on it
nor shown. Check with `ytrack issue attachments APP-123` (the last column is
the comment ID), and add a missing file with `ytrack issue attach APP-123 <file>`.

## Download

Files come down with `ytrack issue download`, never by fetching the signed
`url` with `ytrack api` or curl with a token. It uses the issue's host, like
`issue view`; an unexpected 404 means the wrong host, so check
`ytrack auth status` in the current folder. It never overwrites a file
without `--clobber` and exits 1 naming each file that failed.
