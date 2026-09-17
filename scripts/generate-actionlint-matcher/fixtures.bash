#!/usr/bin/env bash

set -euo pipefail

# actionlint returns 1 for the intentional lint error in this fixture. All other
# statuses indicate that the fixture was not generated as expected.
generate() {
    local output="$1"
    shift
    local status=0
    ./actionlint "$@" ./testdata/err/one_error.yaml > "$output" || status=$?
    if [[ "$status" != 1 ]]; then
        echo "Expected actionlint to report a lint error; got exit status $status" >&2
        return 1
    fi
}

generate ./scripts/generate-actionlint-matcher/test/escape.txt -color
generate ./scripts/generate-actionlint-matcher/test/no_escape.txt -no-color
generate ./scripts/generate-actionlint-matcher/test/want.json -format '{{json .}}'
