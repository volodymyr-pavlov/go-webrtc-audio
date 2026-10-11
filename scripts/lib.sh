# shellcheck shell=sh
# Helpers that the scripts in this folder share. A script reads this file with `. scripts/lib.sh`, after it has changed
# to the root of the repository.

# fetch URL prints the body of URL, fetched from this machine. A script that reaches the server another way, such as
# inside a container, defines its own function with the same arguments and passes its name to the functions below.
fetch() {
	curl -s "$1"
}

# listeners_via FETCH URL prints how many listeners the server at URL counts, or nothing if it does not answer. FETCH is
# the name of a function that prints the body of the URL it is given.
listeners_via() {
	"$1" "$2/api/metrics" | sed -n 's/.*"listeners":\([0-9]*\).*/\1/p'
}

# wait_for_listeners_via FETCH URL COUNT returns when the server counts at least COUNT listeners.
wait_for_listeners_via() {
	until [ "$(listeners_via "$1" "$2")" -ge "$3" ] 2>/dev/null; do
		sleep 1
	done
}

# wait_for_listeners URL COUNT returns when the server at URL counts at least COUNT listeners.
wait_for_listeners() {
	wait_for_listeners_via fetch "$1" "$2"
}
