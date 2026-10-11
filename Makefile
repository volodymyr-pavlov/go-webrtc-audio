include scripts/versions.env

BACKEND := apps/backend
FRONTEND := apps/frontend
GO ?= go
BACKEND_IMAGE ?= go-webrtc-audio-backend
FRONTEND_IMAGE ?= go-webrtc-audio-frontend
MODE ?= fanout
LISTENERS ?= 500
SOURCE_URL ?= https://github.com/volodymyr-pavlov/go-webrtc-audio
GOVULNCHECK_VERSION ?= v1.8.0

VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null | sed 's/^v//')
ifeq ($(VERSION),)
VERSION := 0.0.0-dev
endif

.PHONY: help build backend-build samples serve dev backend-run test test-race vet fmt check vulncheck lint staticcheck check-binary check-style shellcheck fix-style hooks licenses \
	frontend-install frontend-dev frontend-check frontend-build frontend-e2e verify verify-all \
	loadtest profile perf runtime-health evict-time docker-build docker-build-backend docker-build-frontend stand-up stand-up-ai ai-up ai-down turn-secret stand-up-turn stand-check stand-load stand-down version clean

help: ## list the targets
	@awk -F':.*## ' '/^[a-z0-9-]+:.*##/ { printf "%-22s %s\n", $$1, $$2 }' $(firstword $(MAKEFILE_LIST))

build: backend-build frontend-build ## the backend binaries into .bin/ and the page into apps/frontend/.dist

backend-build: ## the server and the load generator into .bin/
	cd $(BACKEND) && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o ../../.bin/server ./cmd/server
	cd $(BACKEND) && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o ../../.bin/loadgen ./cmd/loadgen

samples: ## write the synthetic test tracks into audio/, if it holds no audio file
	sh scripts/samples.sh

serve: backend-build frontend-build samples ## the server with the page on http://localhost:8080, in MODE (fanout, track or naive)
	.bin/server -loopback -mode $(MODE) -web $(FRONTEND)/.dist

dev: ## the backend and the Vite dev server (http://localhost:5173) together
	$(MAKE) -j2 backend-run frontend-dev

backend-run: backend-build samples ## run the backend, API only, on :8080
	.bin/server -loopback -mode $(MODE)

test: ## go test ./...
	cd $(BACKEND) && $(GO) test ./...

test-race: ## backend tests with the race detector
	cd $(BACKEND) && $(GO) test -race -count=1 ./...

vet: ## go vet ./...
	cd $(BACKEND) && $(GO) vet ./...

fmt: ## fail if a Go file is not gofmt-formatted
	@test -z "$$(cd $(BACKEND) && gofmt -l .)" || (cd $(BACKEND) && gofmt -l . && exit 1)

check: fmt vet test-race ## backend: fmt, vet and race tests

vulncheck: ## govulncheck of the backend: known vulnerabilities in the code it calls
	cd $(BACKEND) && $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

lint: ## golangci-lint, from the module apps/backend/tools
	sh scripts/lint.sh golangci

staticcheck: ## staticcheck only
	sh scripts/lint.sh staticcheck

check-binary: ## build the style checks of apps/backend/tools/check into .bin/check
	cd $(BACKEND)/tools && $(GO) build -o ../../../.bin/check ./check

check-style: check-binary ## the style checks: a line for each field of a literal, and no var groups
	cd $(BACKEND) && ../../.bin/check ./...

hooks: ## install the git hooks of lefthook.yml into .git/hooks
	cd $(BACKEND)/tools && $(GO) build -o ../../../.bin/lefthook github.com/evilmartians/lefthook/v2
	LEFTHOOK_BIN=$(CURDIR)/.bin/lefthook .bin/lefthook install

shellcheck: ## shellcheck on the scripts, which needs shellcheck on the PATH
	shellcheck -x -e SC3045 scripts/*.sh

fix-style: check-binary ## rewrite what the style checks can fix, until nothing is left to fix
	cd $(BACKEND) && for pass in 1 2 3 4 5 6; do ../../.bin/check -fix ./... >/dev/null 2>&1 || true; done
	cd $(BACKEND) && gofmt -w .
	$(MAKE) check-style

licenses: ## rewrite the third-party licence notices of the page from the lock files and the Go modules
	node scripts/licenses.mjs

frontend-install: ## npm ci
	cd $(FRONTEND) && npm ci

frontend-dev: ## Vite dev server
	cd $(FRONTEND) && npm run dev

frontend-check: ## type check, unit tests and the Prettier check
	cd $(FRONTEND) && npm run check

frontend-build: ## production build of the page into apps/frontend/.dist
	cd $(FRONTEND) && npm run build

frontend-e2e: backend-build samples ## the Playwright test: the real page against the real backend
	cd $(FRONTEND) && npm run test:e2e

verify: check check-style lint staticcheck vulncheck frontend-check frontend-build ## every formatter, linter, check and test that needs no container

verify-all: ## every check and test, the end-to-end and Docker ones too, with a summary of what passed (needs Docker, takes minutes)
	sh scripts/verify-all.sh

loadtest: ## every mode at 10, 100 and 500 listeners; writes out/loadtest/results.md
	sh scripts/loadtest.sh

profile: ## CPU, heap and goroutine profiles of MODE under LISTENERS listeners, in out/pprof/
	MODE=$(MODE) LISTENERS=$(LISTENERS) sh scripts/profile.sh

perf: ## perf split of CPU time (Go, kernel, vDSO) of the running stand under LISTENERS listeners, in out/perf.md
	LISTENERS=$(LISTENERS) sh scripts/perf.sh

runtime-health: ## what the runtime monitor saw under LISTENERS listeners (NAME, QUEUE and the other compose variables apply), in out/runtime/
	LISTENERS=$(LISTENERS) sh scripts/runtime-health.sh

evict-time: ## how long the server takes to notice listeners that vanish (LISTENERS, default 20; needs the backend image)
	sh scripts/evict-time.sh

docker-build: docker-build-backend docker-build-frontend ## both images

docker-build-backend: ## the backend image BACKEND_IMAGE:VERSION, with ffmpeg and the sample tracks
	docker build -f $(BACKEND)/Dockerfile \
		--build-arg GO_ALPINE_IMAGE=$(GO_ALPINE_IMAGE) --build-arg ALPINE_IMAGE=$(ALPINE_IMAGE) \
		--build-arg VERSION=$(VERSION) --build-arg SOURCE_URL=$(SOURCE_URL) \
		-t $(BACKEND_IMAGE):$(VERSION) -t $(BACKEND_IMAGE):latest .

docker-build-frontend: ## the page image FRONTEND_IMAGE:VERSION, on nginx
	docker build -f $(FRONTEND)/Dockerfile \
		--build-arg NODE_ALPINE_IMAGE=$(NODE_ALPINE_IMAGE) --build-arg NGINX_IMAGE=$(NGINX_IMAGE) \
		--build-arg VERSION=$(VERSION) --build-arg SOURCE_URL=$(SOURCE_URL) \
		-t $(FRONTEND_IMAGE):$(VERSION) -t $(FRONTEND_IMAGE):latest .

# The commands that bring a stand up and down. If a stand comes up with the published UDP port forwarding nothing, it is
# brought down and up once more (see scripts/udp-check.sh). The macro `heal` does this. It takes the name of the variable
# that holds the command that brings the stand up.
# Compose stops reading the project .env as soon as an --env-file is given, so it is passed here as well, when it
# exists. The stand scripts take COMPOSE from the environment.
export COMPOSE = docker compose --env-file scripts/versions.env $(if $(wildcard .env),--env-file .env)
COMPOSE_AI = $(COMPOSE) -f compose.yaml -f compose.ai.yaml
COMPOSE_TURN = $(COMPOSE) -f compose.yaml -f compose.turn.yaml

STAND_UP = $(COMPOSE) up --build --detach --wait
STAND_UP_TURN = TURN_URL="turn:$${TURN_HOST:-127.0.0.1}:$${TURN_PORT:-3478}?transport=udp,turn:$${TURN_HOST:-127.0.0.1}:$${TURN_PORT:-3478}?transport=tcp" TURN_SECRET_FILE=/run/secrets/turn_secret $(COMPOSE_TURN) up --build --detach --wait
STAND_UP_AI = PRESENTER=true PRESENTER_OLLAMA=http://ollama:11434 PRESENTER_TTS="$(or $(PRESENTER_TTS),espeak-ng -v en-us -s 165 --stdin -w {out})" $(COMPOSE_AI) up --build --detach --wait
STAND_DOWN = TURN_SECRET="$${TURN_SECRET:-unset}" $(COMPOSE) --profile load -f compose.yaml -f compose.turn.yaml -f compose.ai.yaml down

# UDP_CHECK is the command that checks the published UDP port. A test can put a stand-in here.
UDP_CHECK ?= sh scripts/udp-check.sh

define heal
	@$(UDP_CHECK) || { echo 'the published UDP port forwards nothing: bringing the stand down and up again'; $(STAND_DOWN); $($(1)); $(UDP_CHECK); }
endef

stand-up: ## build and start the stand (backend, and the page on nginx) with docker compose; the page is on http://localhost:8080
	$(STAND_UP)
	$(call heal,STAND_UP)

turn-secret: ## write a random TURN_SECRET into .env, once
	@grep -qs '^TURN_SECRET=' .env || { printf 'TURN_SECRET=%s\n' "$$(openssl rand -hex 24)" >> .env; echo 'wrote TURN_SECRET to .env'; }

stand-up-turn: turn-secret ## the stand with a TURN server (coturn) next to the backend; see compose.turn.yaml
	$(STAND_UP_TURN)
	$(call heal,STAND_UP_TURN)

ai-up: ## start Ollama (the official image, on 127.0.0.1) and pull the model PRESENTER_MODEL (default phi3.5) once
	$(COMPOSE_AI) up --detach --wait ollama
	$(COMPOSE_AI) exec ollama ollama pull $(or $(PRESENTER_MODEL),phi3.5)

ai-down: ## stop Ollama; the model stays in its volume
	$(COMPOSE_AI) stop ollama

stand-up-ai: ai-up ## the stand with the AI presenter: a model on this machine writes and a voice speaks the introduction of each next track
	$(STAND_UP_AI)
	$(call heal,STAND_UP_AI)

stand-check: ## check the running stand: what is public, what is not, the headers, the user of nginx
	sh scripts/check-stand.sh

stand-load: ## run the load generator inside the stand: LISTENERS fake listeners (default 100), HOLD (default 30s)
	$(COMPOSE) --profile load run --rm loadgen

stand-down: ## stop the stand and remove its container
	$(STAND_DOWN)

version: ## the version a local build gets
	@echo $(VERSION)

clean: ## remove the build output and the output of the load tests
	rm -rf .bin out $(FRONTEND)/.dist
