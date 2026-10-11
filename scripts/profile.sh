#!/bin/sh
# Profiles the server in MODE while LISTENERS listeners are connected, and prints the top of the CPU profile. The
# profiles go to out/pprof/. Open one with `go tool pprof -http=: out/pprof/<name>.cpu.pb.gz`.
#
#   MODE=naive LISTENERS=500 scripts/profile.sh
#   NAME=after SERVER_FLAGS="-shards 4" scripts/profile.sh
set -eu

cd "$(dirname "$0")/.."
. scripts/lib.sh

MODE=${MODE:-fanout}
LISTENERS=${LISTENERS:-500}
SECONDS_CPU=${SECONDS_CPU:-20}
NAME=${NAME:-$MODE-$LISTENERS}
DIR=${DIR:-out/pprof}
HTTP_ADDR=${HTTP_ADDR:-127.0.0.1:18080}
SERVER_FLAGS=${SERVER_FLAGS:-}

mkdir -p "$DIR"
sh scripts/samples.sh
make -s backend-build
ulimit -n 65536 2>/dev/null || true

# shellcheck disable=SC2086
.bin/server -http "$HTTP_ADDR" -loopback -pprof -mode "$MODE" $SERVER_FLAGS >"$DIR/$NAME.server.log" 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true' EXIT
sleep 3

hold=$((SECONDS_CPU + 15))
.bin/loadgen -server "http://$HTTP_ADDR" -listeners "$LISTENERS" -idle 1s -hold "${hold}s" -settle 3s -label "$NAME" \
	>"$DIR/$NAME.loadgen.txt" &
loadgen=$!

# Wait until every listener is up, then profile the steady state.
wait_for_listeners "http://$HTTP_ADDR" "$LISTENERS"
sleep 3
curl -s -o "$DIR/$NAME.cpu.pb.gz" "http://$HTTP_ADDR/debug/pprof/profile?seconds=$SECONDS_CPU"
curl -s -o "$DIR/$NAME.heap.pb.gz" "http://$HTTP_ADDR/debug/pprof/heap"
curl -s -o "$DIR/$NAME.goroutine.txt" "http://$HTTP_ADDR/debug/pprof/goroutine?debug=1"
wait $loadgen || true

go tool pprof -top -nodecount=25 .bin/server "$DIR/$NAME.cpu.pb.gz" 2>/dev/null | tee "$DIR/$NAME.cpu.top.txt"
cat "$DIR/$NAME.loadgen.txt"
