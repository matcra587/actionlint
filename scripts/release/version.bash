#!/usr/bin/env bash

# Keep the tag grammar shared by local preflight and the publishing workflow.
release_version() {
    local number='(0|[1-9][0-9]*)'
    local pattern="^v(${number}\\.${number}\\.${number})$"
    if [[ ! "${1:-}" =~ $pattern ]]; then
        echo 'Expected a stable release tag such as v1.9.0, without leading zeros' >&2
        return 1
    fi
    printf '%s\n' "${BASH_REMATCH[1]}"
}
