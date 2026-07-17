#!/usr/bin/env bash

# Shared strict SemVer helpers for release workflows and packaging scripts.
# Numeric prerelease identifiers may not contain leading zeroes; build metadata
# follows the SemVer 2.0.0 character and dot-separation rules.
SEMVER_CORE='(0|[1-9][0-9]*)'
SEMVER_PRERELEASE_IDENTIFIER='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
SEMVER_BUILD_IDENTIFIER='[0-9A-Za-z-]+'
SEMVER_PATTERN="^${SEMVER_CORE}\.${SEMVER_CORE}\.${SEMVER_CORE}(-${SEMVER_PRERELEASE_IDENTIFIER}(\.${SEMVER_PRERELEASE_IDENTIFIER})*)?(\+${SEMVER_BUILD_IDENTIFIER}(\.${SEMVER_BUILD_IDENTIFIER})*)?$"

is_semver() {
    [[ "$1" =~ $SEMVER_PATTERN ]]
}

is_release_tag() {
    [[ "$1" == v* ]] && is_semver "${1#v}"
}

semver_is_prerelease() {
    local without_build="${1%%+*}"
    [[ "$without_build" == *-* ]]
}
