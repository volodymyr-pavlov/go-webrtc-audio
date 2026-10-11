#!/bin/sh
# Connects one real listener from this machine to the running stand, through the published ports, and exits with 0 if it
# hears the stream and 1 if it does not.
#
# The audio does not come through the page's nginx: it goes from the backend to the browser over the published UDP port. A
# stand can be left with the page loading, the offers answered, and that port forwarding nothing, and the page then says
# that the connection could not be set up. That happens on Docker Desktop when the backend container is replaced on its
# own (docker compose up on a stand that is already running, after a change); a restart does not cure it, and bringing
# the whole stand down and up does. It exits with 0, and says so, if the Go toolchain is not here to build the load
# generator, because then there is nothing to ask.
set -eu

cd "$(dirname "$0")/.."

API=${API:-http://127.0.0.1:8081}

mkdir -p .bin
if ! (cd apps/backend && go build -o ../../.bin/loadgen ./cmd/loadgen) >/dev/null 2>&1; then
	echo "skip  the Go toolchain is not here to build loadgen"
	exit 0
fi

report=$(mktemp)
trap 'rm -f "$report"' EXIT
for _ in 1 2 3; do
	.bin/loadgen -server "$API" -listeners 1 -idle 0s -hold 4s -settle 1s -json "$report" >/dev/null 2>&1 || true
	connected=$(sed -n 's/.*"connected": *\([0-9]*\).*/\1/p' "$report" | head -1)
	if [ "${connected:-0}" = 1 ]; then
		exit 0
	fi
	sleep 3
done
exit 1
