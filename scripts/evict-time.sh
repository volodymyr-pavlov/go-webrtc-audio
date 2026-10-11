#!/bin/sh
# Measures how long the server takes to notice listeners that vanish without saying goodbye: it connects LISTENERS
# fake listeners inside a container, kills the load generator with SIGKILL so that nothing is sent on the way out, and
# prints the seconds until the server's own count of listeners is zero. Needs Docker and the backend image
# (make docker-build-backend).
set -eu

cd "$(dirname "$0")/.."
. scripts/lib.sh

IMAGE=${IMAGE:-go-webrtc-audio-backend:latest}
LISTENERS=${LISTENERS:-20}
NAME=evict-time

docker rm -f "$NAME" >/dev/null 2>&1 || true
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$NAME" "$IMAGE" -http :8080 -audio /app/audio -ice-port 5005 -ice-host 127.0.0.1 -loopback >/dev/null
sleep 3

# The server answers inside the container only, so the count is fetched from there.
fetch_in_container() {
	docker exec "$NAME" wget -qO- "$1"
}

count() {
	listeners_via fetch_in_container http://127.0.0.1:8080
}

docker exec -d "$NAME" loadgen -server http://127.0.0.1:8080 -listeners "$LISTENERS" -hold 300s -idle 1s -leave 0 -settle 0s
wait_for_listeners_via fetch_in_container http://127.0.0.1:8080 "$LISTENERS"
sleep 3
echo "$(count) listeners connected; killing the load generator"

docker exec "$NAME" pkill -9 loadgen
start=$(date +%s)
while [ "$(count)" -gt 0 ]; do
	sleep 0.5
	if [ $(($(date +%s) - start)) -gt 60 ]; then
		echo "still $(count) listeners after 60 s" >&2
		exit 1
	fi
done
echo "the server noticed after $(($(date +%s) - start)) s"
