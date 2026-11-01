#!/bin/sh
# Runs the engine/API logic tests on Linux with HID/Win32 stubbed out.
set -e
D=$(mktemp -d); cd "$(dirname "$0")/.."
for f in api.go engine.go store.go protocol.go i18n.go battery.go commands.go mousemem.go; do sed '1{/^\/\/go:build/d}' $f > $D/$f; done
cp -r web $D/; for f in stubs.go engine_test.go battery_test.go; do sed "/^\/\/go:build ignore/d" linuxtest/$f > $D/$f; done
printf 'module lt\n\ngo 1.24\n' > $D/go.mod
cd $D && GOFLAGS= go test -race -count=1 .
