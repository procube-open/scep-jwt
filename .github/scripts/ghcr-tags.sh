#!/usr/bin/env bash
set -euo pipefail

image="${GITHUB_REPOSITORY_OWNER,,}/${PACKAGE_NAME}"
token_response=$(curl --silent --show-error --fail \
  --user "${GITHUB_ACTOR}:${GITHUB_TOKEN}" \
  --get --data-urlencode "service=ghcr.io" \
  --data-urlencode "scope=repository:${image}:pull" \
  https://ghcr.io/token)
registry_token=$(jq --exit-status --raw-output \
  '.token | select(type == "string" and length > 0)' <<< "$token_response")
echo "::add-mask::${registry_token}"

response_file=$(mktemp)
trap 'rm -f "$response_file"' EXIT
status=$(curl --silent --show-error \
  --header "Authorization: Bearer ${registry_token}" \
  --output "$response_file" --write-out '%{http_code}' \
  "https://ghcr.io/v2/${image}/tags/list")

case "$status" in
  200)
    taglist=$(jq --compact-output --exit-status '
      if type == "object" and has("tags") and
        (.tags == null or (.tags | type == "array" and all(.[]; type == "string")))
      then {tags: (.tags // [])}
      else error("Invalid GHCR tags response")
      end
    ' "$response_file")
    ;;
  404)
    echo "::notice::GHCR image ${image} does not exist yet."
    taglist='{"tags":[]}'
    ;;
  *)
    echo "::error::Failed to list GHCR tags for ${image} (HTTP ${status})." >&2
    exit 1
    ;;
esac

echo "taglist=${taglist}" >> "$GITHUB_OUTPUT"
