generate-popular-actions
========================

This is a script for generating [`popular_actions.go`](../../popular_actions.go).

It does:

- Fetches metadata of popular actions
  - from https://github.com
  - from JSONL file in local
- Generates the fetched data set of metadata
  - as Go source file
  - as JSONL file

## Usage

Generate Go source:

```sh
go run ./scripts/generate-popular-actions ./popular_actions.go
```

Discover new versions and regenerate all sources:

```sh
mise run generate:update
mise run test
mise run workflows
```

Discovery reads published tags and branches with `git ls-remote`. Each registry entry can opt in with
`version_pattern`, a regular expression whose single capture group contains a numeric version. The
pattern matches the entire ref name. For example, `v([0-9]+)` tracks major aliases, `v([0-9]+)\\.x`
tracks Octokit's aliases, and `release/v([0-9]+)` tracks PyPI's release branches. Full versions can
use `v([0-9]+\\.[0-9]+\\.[0-9]+)` (backslashes are escaped in JSON).

Discovery queries up to eight repositories concurrently and shares each repository's results across
its sub-actions. Registry and generated output ordering remain stable. Failed queries cancel the
remaining work before any registry update is written.

Only versions newer than the highest matching entry are added. Existing versions and metadata
options are preserved; deliberately omitted older versions are not reintroduced. Prereleases are
excluded by these patterns. Entries without a pattern remain manually curated.

Before adding a ref, discovery checks that the configured action metadata file exists, including
sub-actions and `action.yaml` files. HTTP 404 skips that candidate; other network failures abort the
update without writing the registry. A successful update and a no-change run both exit zero.
The following generation step parses the metadata, and the workflow runs tests before opening or
updating its single `automation/generated-data` pull request. The action code is never executed.

To update only the registry, use `go run ./scripts/generate-popular-actions -u -r FILE`.
For a read-only report, use `-d` (exit 2 means new versions were found).
Discovery requires Git and network access; normal actionlint runs use the checked-in data offline.
See `-help` for the remaining options.

## The data source file

The data source of the popular actions is defined in [`popular_actions.json`](./popular_actions.json). This file contains an array
of each action registry. Each registry is a JSON object containing the following keys:

| Key            | Description                                                     | Example                    | Required? |
|----------------|-----------------------------------------------------------------|----------------------------|-----------|
| `slug`         | GitHub repository slug                                          | `"actions/checkout"`       | Yes       |
| `tags`         | Known release tags                                              | `["v1", "v2", "v3", "v4"]` | Yes       |
| `version_pattern` | Ref pattern with one numeric version capture; omitted disables discovery | `"v([0-9]+)"` | No |
| `path`         | Absolute path to the action from the repository root            | `"/path/to/action"`        | No        |
| `skip_inputs`  | Skipping checking inputs of this action or not                  | `true`                     | No        |
| `skip_outputs` | Skipping checking outputs of this action or not                 | `true`                     | No        |
| `file_ext`     | File extension of action metadata file. The default is `"yml"`  | `"yaml"`                   | No        |

Alternative actions registry JSON file can be used via `-r` option.
