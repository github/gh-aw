#!/bin/sh
set -eu
exec python3 /tests/verify.py \
  --config /tests/mapping.json \
  --session /app/evidence/aw_session.jsonl
