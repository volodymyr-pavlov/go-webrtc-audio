#!/bin/sh
# Runs the stand under LISTENERS fake listeners and writes what the backend's runtime monitor saw while they were
# connected: the samples as CSV to OUT_DIR/<NAME>.csv, and one summary line to stdout. The stand must be up
# (`make stand-up`); the backend is recreated with the environment of this call, so QUEUE, BATCH and the others of
# compose.yaml can be set:
#
#   NAME=queue32 QUEUE=32 scripts/runtime-health.sh
#   NAME=direct QUEUE=-1 scripts/runtime-health.sh
set -eu

cd "$(dirname "$0")/.."

LISTENERS=${LISTENERS:-500}
HOLD=${HOLD:-40s}
NAME=${NAME:-run}
OUT_DIR=${OUT_DIR:-out/runtime}
API=${API:-http://127.0.0.1:8081}
: "${COMPOSE:?is not set: the Makefile sets it}"

mkdir -p "$OUT_DIR"
export LISTENERS HOLD
$COMPOSE up -d --no-deps --force-recreate backend >/dev/null 2>&1
sleep 15
make -s stand-load >/dev/null 2>&1
curl -s "$API/api/runtime" >"$OUT_DIR/$NAME.csv"

# The samples taken while the listeners were connected are the ones with many goroutines. The CSV holds no mean of the
# scheduler wait, only its 99th percentile (column 12) and its maximum (column 13) for each second, so the line gives the
# worst of each. The summary line has these columns: name, samples, mean goroutines, mean heap (MiB), mean allocation
# (MiB/s), mean GC CPU (percent), mean mutex wait (ms), worst scheduler wait p99 (ms), worst scheduler wait (ms).
awk -F, -v name="$NAME" -v floor=$((LISTENERS * 5)) '
	NR > 1 && $2 >= floor {
		n++
		goroutines += $2; heap += $4; alloc += $9; gc += $10; mutex += $11
		if ($12 > p99) p99 = $12
		if ($13 > worst) worst = $13
	}
	END {
		if (n == 0) { print "| " name " | no sample with the listeners connected |"; exit 1 }
		printf "| %s | %d | %.0f | %.1f | %.1f | %.3f | %.3f | %.2f | %.2f |\n",
			name, n, goroutines / n, heap / n / 1048576, alloc / n / 1048576, gc / n, mutex / n * 1000,
			p99 * 1000, worst * 1000
	}' "$OUT_DIR/$NAME.csv"
