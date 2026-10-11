#!/bin/sh
# Runs the server in each mode against the load generator, for each listener count. It writes one table to
# out/loadtest/results.md, and the raw output of each run to out/loadtest/. The server and the generator run on this
# machine, so read the numbers as a comparison between modes and not as a capacity.
#
#   scripts/loadtest.sh                          all modes, 10, 100 and 500 listeners
#   MODES="fanout" COUNTS="500" scripts/loadtest.sh
#   HOLD=60s scripts/loadtest.sh
set -eu

cd "$(dirname "$0")/.."

MODES=${MODES:-"naive track fanout"}
COUNTS=${COUNTS:-"10 100 500"}
HOLD=${HOLD:-30s}
DIR=${DIR:-out/loadtest}
OUT=${OUT:-$DIR/results.md}
HTTP_ADDR=${HTTP_ADDR:-127.0.0.1:18080}
SERVER_FLAGS=${SERVER_FLAGS:-}

mkdir -p "$DIR"
sh scripts/samples.sh
make -s backend-build
ulimit -n 65536 2>/dev/null || true

header=true
: >"$OUT.tmp"
for mode in $MODES; do
	for count in $COUNTS; do
		# shellcheck disable=SC2086
		.bin/server -http "$HTTP_ADDR" -loopback -mode "$mode" $SERVER_FLAGS >"$DIR/server-$mode-$count.log" 2>&1 &
		pid=$!
		sleep 3
		echo "== $mode, $count listeners" >&2
		.bin/loadgen -server "http://$HTTP_ADDR" -listeners "$count" -hold "$HOLD" -label "$mode" \
			-json "$DIR/$mode-$count.json" >"$DIR/$mode-$count.txt" || true
		# The first run writes the table header. Every later run adds only its last line.
		if [ "$header" = true ]; then
			cat "$DIR/$mode-$count.txt" >>"$OUT.tmp"
			header=false
		else
			tail -n 1 "$DIR/$mode-$count.txt" >>"$OUT.tmp"
		fi
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
		sleep 1
	done
done
mv "$OUT.tmp" "$OUT"
cat "$OUT"
