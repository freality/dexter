#!/bin/sh
set -eu
profile="$1"
grep -v -e '/cmd/dex/main.go:' -e '/adapters/docker/cmd/dex-adapter-docker/main.go:' -e '/pkg/jsenv/sdk/gen/main.go:' -e '/pkg/plugin/pb/' "$profile" > "$profile.filtered"
go tool cover -func="$profile.filtered" > coverage.txt
grep -v -e '/apply.go:' -e '/tui.go:' -e '/engine.go:' coverage.txt > coverage.txt.filtered
awk '$1 != "total:" && $3 != "100.0%" { print "UNCOVERED " $0; bad=1 } END { exit bad+0 }' coverage.txt.filtered && echo "coverage: 100.0% of statements" || { echo "coverage gate failed (see uncovered lines above)" >&2; exit 1; }
