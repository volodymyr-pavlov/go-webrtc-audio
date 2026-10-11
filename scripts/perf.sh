#!/bin/sh
# Samples the server of the running stand with perf while LISTENERS fake listeners are connected. It writes the split of
# the CPU time between the Go code, the kernel and the vDSO to OUT. The stand must be up (`make stand-up`).
#
#   LISTENERS=200 scripts/perf.sh
#
# perf runs in a privileged container of the pinned Debian image, which shares the process table of the Docker VM. The
# VM has no hardware performance counters, so the samples come from the software clock (cpu-clock) at 999 Hz.
set -eu

cd "$(dirname "$0")/.."
. scripts/lib.sh
. scripts/versions.env

LISTENERS=${LISTENERS:-200}
SECONDS_PERF=${SECONDS_PERF:-20}
OUT=${OUT:-out/perf.md}
: "${COMPOSE:?is not set: the Makefile sets it}"
METRICS=${METRICS:-http://127.0.0.1:8081/api/metrics}

hold=$((SECONDS_PERF + 40))
$COMPOSE exec -d backend loadgen -server http://127.0.0.1:8080 -listeners "$LISTENERS" -idle 1s -hold "${hold}s" \
	-settle 3s -label perf
wait_for_listeners "${METRICS%/api/metrics}" "$LISTENERS"
sleep 3

report=$(docker run --rm --privileged --pid=host "$DEBIAN_IMAGE" sh -c '
	apt-get update -qq >/dev/null 2>&1
	apt-get install -y -qq linux-perf >/dev/null 2>&1
	pid=$(pgrep -x server | head -1)
	echo "kernel $(uname -r) $(uname -m), perf $(perf --version | cut -d" " -f3), pid $pid"
	perf record -e cpu-clock -F 999 -g -p "$pid" -o /tmp/perf.data -- sleep '"$SECONDS_PERF"' 2>&1 | grep -o "[0-9]* samples"
	echo "--- by object"
	perf report -i /tmp/perf.data --no-children --stdio --sort dso -g none 2>/dev/null | grep -v "^#" | grep -v "^$"
	echo "--- kernel, grouped by the name of the function (a heuristic)"
	perf report -i /tmp/perf.data --no-children --stdio --sort symbol -g none --dsos "[kernel.kallsyms]" 2>/dev/null \
		| grep -v "^#" | grep -v "^$" | awk "{
			p = \$1; sub(/%/, \"\", p); s = \$3; g = \"other\"
			if (s ~ /nf_|conntrack|ipt_|xt_/) g = \"netfilter and conntrack (Docker NAT)\"
			else if (s ~ /wake|futex|schedule|task_switch|sched|hrtimer|unlock_irq/) g = \"scheduler, wake-ups and locks\"
			else if (s ~ /udp|ip_|sock|skb|net|dev_|veth|tcp|softirq|sk_/) g = \"network stack\"
			else if (s ~ /svc|syscall|seccomp|copy_/) g = \"syscall entry and copies\"
			a[g] += p
		} END { for (k in a) printf \"%6.2f%% %s\n\", a[k], k }" | sort -rn
')

mkdir -p "$(dirname "$OUT")"
{
	echo "# perf, fanout, $LISTENERS listeners, ${SECONDS_PERF} s"
	echo
	echo '```'
	echo "$report"
	echo '```'
} >"$OUT"
cat "$OUT"
