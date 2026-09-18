generate-actionlint-matcher
===========================

This script generates [`actionlint-matcher.json`](../../.github/actionlint-matcher.json).

## Usage

```sh
mise run matcher:generate
```

or directly run the script

```sh
bun ./scripts/generate-actionlint-matcher/main.mjs .github/actionlint-matcher.json
```

## Test

```sh
mise run matcher:test
```

The test uses test data at `./scripts/generate-actionlint-matcher/test/*.txt`. They should be updated when actionlint changes
the default error message format. To update them:

```sh
mise run matcher:fixtures
```
