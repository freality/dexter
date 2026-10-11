#!/bin/sh
# Fails unless every function outside the two one-statement main.go entrypoints is 100% covered.
set -eu
profile="$1"
grep -v -e '/cmd/dex/main.go:' -e '/adapters/docker/cmd/dex-adapter-docker/main.go:' -e '/cmd/dex/internal/cli/plugins.go:' -e '/pkg/jsenv/sdk/gen/main.go:' -e '/pkg/plugin/pb/' "$profile" > "$profile.filtered"
go tool cover -func="$profile.filtered" > coverage.txt
if awk '$1 != "total:" && $3 != "100.0%" { print "UNCOVERED " $0; bad=1 } END { exit bad+0 }' coverage.txt; then
  echo "coverage: 100.0% of statements"
else
  echo "coverage gate failed (see uncovered lines above)" >&2
  exit 1
fi
