#!/bin/sh
# Checks a running stand (make stand-up): the page and the routes the page uses are served, nothing else of the backend
# is public, the security headers are there, and nginx does not run as root. It exits with an error at the first
# failure.
set -eu

cd "$(dirname "$0")/.."

PAGE=${PAGE:-http://localhost:8080}
API=${API:-http://127.0.0.1:8081}
: "${COMPOSE:?is not set: the Makefile sets it}"

check() { # description expected actual
	if [ "$2" = "$3" ]; then
		echo "ok    $1"
	else
		echo "FAIL  $1: expected $2, got $3" >&2
		exit 1
	fi
}

status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

check "the page" 200 "$(status "$PAGE/")"
check "an unknown path is 404, not the page" 404 "$(status "$PAGE/nope")"
check "/api/playlist" 200 "$(status "$PAGE/api/playlist")"
check "/api/playlist rejects POST" 403 "$(status -X POST "$PAGE/api/playlist")"
check "/api/listen rejects GET" 403 "$(status "$PAGE/api/listen")"
check "/api/listen/<id> takes only a number" 404 "$(status -X DELETE "$PAGE/api/listen/abc")"
check "/api/listen/<id> of nobody" 404 "$(status -X DELETE "$PAGE/api/listen/1")"
check "/api/metrics is not public" 404 "$(status "$PAGE/api/metrics")"
check "/api/debug/pprof/ is not public" 404 "$(status "$PAGE/api/debug/pprof/")"
check "/metrics is not served by the page" 404 "$(status "$PAGE/metrics")"

headers=$(curl -sI "$PAGE/")
for header in x-content-type-options x-frame-options referrer-policy content-security-policy; do
	check "header $header" 1 "$(printf '%s' "$headers" | grep -ci "^$header:")"
done
check "no header is split across lines" 0 "$(printf '%s\n' "$headers" | tr -d '\r' | sed 1d | grep -v '^$' | grep -vc '^[A-Za-z][A-Za-z0-9-]*: ')"
check "the server version is hidden" 0 "$(printf '%s' "$headers" | grep -ci '^server: nginx/')"
check "the page is not cached" 1 "$(printf '%s' "$headers" | grep -ci '^cache-control: no-cache')"
check "the API answer is not cached" 1 "$(curl -sI "$PAGE/api/playlist" | grep -ci '^cache-control: no-store')"

check "nginx does not run as root" 101 "$($COMPOSE exec -T frontend id -u)"
check "the backend's own API answers on the loopback interface" 200 "$(status "$API/api/playlist")"
check "the backend's metrics are there for whoever may read them" 200 "$(status "$API/api/metrics")"
check "the runtime health samples are there too" 200 "$(status "$API/api/runtime")"
check "the runtime health samples are not public" 404 "$(status "$PAGE/api/runtime")"
# The audio does not come through the page's nginx, so one real listener connects from this machine through the published
# UDP port (see scripts/udp-check.sh for why).
udp=ok
sh scripts/udp-check.sh >/dev/null 2>&1 || udp=broken
check "a listener on this machine connects through the published UDP port" ok "$udp"

# A burst of offers from one address meets the rate limit (20 a second, burst of 40) before it reaches the backend.
limited=0
for _ in $(seq 1 150); do
	[ "$(status -X POST -d x "$PAGE/api/listen")" = 429 ] && limited=$((limited + 1))
done
check "a burst of offers from one address is limited" yes "$([ "$limited" -gt 0 ] && echo yes || echo no)"
sleep 3

echo "the stand is as it should be"
