#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${SPOTIFY_CLIENT_ID:-}" || -z "${SPOTIFY_CLIENT_SECRET:-}" ]]; then
  echo "SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET are required."
  echo "Example:"
  echo "  export SPOTIFY_CLIENT_ID='your_client_id'"
  echo "  export SPOTIFY_CLIENT_SECRET='your_client_secret'"
  exit 1
fi

REDIRECT_URI="${SPOTIFY_REDIRECT_URI:-http://127.0.0.1:8787/callback}"
REFRESH_TOKEN_FILE="${SPOTIFY_REFRESH_TOKEN_FILE:-}"

args=(
  --client-id "${SPOTIFY_CLIENT_ID}"
  --client-secret "${SPOTIFY_CLIENT_SECRET}"
  --redirect-uri "${REDIRECT_URI}"
)

if [[ -n "${REFRESH_TOKEN_FILE}" ]]; then
  args+=(--refresh-token-file "${REFRESH_TOKEN_FILE}")
fi

go run ./cmd/spotify-auth "${args[@]}" "$@"
