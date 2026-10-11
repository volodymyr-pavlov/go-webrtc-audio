#!/bin/sh
# Runs every check and test of the project, one step after another, and prints what passed and what failed. It does not
# stop at the first failure. It needs Docker, because the stand steps build and start the containers, and it leaves no
# stand running. The AI step pulls the model of the presenter (about 2 GB) on the first run.
set -u

cd "$(dirname "$0")/.." || exit 1

RESULTS=$(mktemp)
trap 'rm -f "$RESULTS"; make stand-down >/dev/null 2>&1' EXIT

step() { # name command...
	name=$1
	shift
	echo
	echo "=== $name"
	if "$@"; then
		echo "pass  $name" >>"$RESULTS"
	else
		echo "FAIL  $name" >>"$RESULTS"
	fi
}

stand_check() {
	make stand-up && make stand-check && make stand-load
	status=$?
	make stand-down
	return $status
}

stand_turn() {
	make stand-up-turn && (cd apps/frontend && STAND_URL=http://localhost:8080 npx playwright test e2e/turn.spec.ts)
	status=$?
	make stand-down
	return $status
}

stand_ai() {
	make ai-up && (cd apps/backend && go test -count=1 -tags live -run Live ./ai) && make stand-up-ai && make stand-check
	status=$?
	make stand-down
	make ai-down
	return $status
}

step "backend: fmt, vet and race tests" make check
step "backend: style checks" make check-style
step "backend: golangci-lint" make lint
step "backend: staticcheck" make staticcheck
step "backend: known vulnerabilities" make vulncheck
step "scripts: shellcheck" make shellcheck
step "page: types, unit tests and Prettier" make frontend-check
step "page: production build" make frontend-build
step "page: end-to-end tests" make frontend-e2e
step "docker: stand, stand checks and load" stand_check
step "docker: stand with TURN and the relay test" stand_turn
step "docker: Ollama, the live model test and the stand with the AI presenter" stand_ai

echo
echo "=== result"
cat "$RESULTS"
if grep -q '^FAIL' "$RESULTS"; then
	exit 1
fi
echo "everything passed"
