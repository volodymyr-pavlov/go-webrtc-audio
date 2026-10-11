# Running, flags and the API

This page is the reference for running the server. It covers the requirements, the commands, the flags, the modes,
the API, the Docker stand, privacy notes, the tests and the load tests.

## Requirements

- Go 1.27.
- `ffmpeg` with libopus on the PATH. The server runs it at start-up (`brew install ffmpeg`).
- Node.js 24 or newer, for the page.
- Docker, for the images, the stand and the Linux measurements.

## Commands

| Command               | What it does                                                                           |
| --------------------- | -------------------------------------------------------------------------------------- |
| `make serve`          | Builds the page and the server, writes three test tracks, and serves both on `:8080`.  |
| `make dev`            | Runs the server on `:8080` and the page on `:5173` with hot reload.                    |
| `make check`          | Backend: `gofmt`, `go vet` and the tests with the race detector.                       |
| `make frontend-check` | Page: type check, unit tests and the Prettier check.                                   |
| `make frontend-e2e`   | Page: Chromium against a real backend. The first run may need `npx playwright install chromium`. |
| `make lint`           | `golangci-lint`, with the rules in `.golangci.yml`.                                    |
| `make staticcheck`    | `staticcheck`.                                                                         |
| `make vulncheck`      | `govulncheck`: known vulnerabilities in the code the backend calls.                    |
| `make check-style`    | The style checks of `apps/backend/tools/check`: a line for each field of a literal, no `var` groups, and every error in `errors.go`. |
| `make shellcheck`     | `shellcheck` on the scripts.                                                           |
| `make verify`         | Every check above except `make frontend-e2e` and `make shellcheck`, and the production build of the page. |

### Which audio is played

The script `scripts/samples.sh` writes three synthetic tracks. Each is two sine tones, and they last 30, 25 and 20
seconds. It writes them only if `audio/` holds no audio file.

- A fresh clone has something to play.
- A project with real songs plays those and nothing else.
- Other audio goes into `audio/`, or `-audio` points the server at another folder.

### Listening from the same machine or behind a NAT

`make serve` passes `-loopback`. It offers the loopback address, so a browser on the same machine can connect.

Behind a NAT, the server is given the address that listeners use:

```shell
.bin/server -ice-host 203.0.113.7
```

## Flags

### Serving

| Flag       | Default | Meaning                                                              |
| ---------- | ------- | -------------------------------------------------------------------- |
| `-http`    | `:8080` | Address of the HTTP server.                                          |
| `-web`     | none    | Built page to serve on `/`, for example `apps/frontend/.dist`. Without it the server has only the API. |
| `-audio`   | `audio` | Folder with the audio files.                                         |
| `-ffmpeg`  | `ffmpeg` | The ffmpeg program.                                                 |
| `-log`     | `info`  | Log level: `debug`, `info`, `warn` or `error`.                       |

### WebRTC

| Flag                | Default | Meaning                                                               |
| ------------------- | ------- | --------------------------------------------------------------------- |
| `-ice-port`         | `5005`  | The one UDP port of every WebRTC connection.                          |
| `-ice-host`         | none    | Public address for listeners behind a NAT, comma separated.           |
| `-loopback`         | off     | Also offer the loopback address, to listen from this machine.         |
| `-connect-timeout`  | `15s`   | Time a listener has to connect before it is dropped.                  |
| `-evict-after`      | `5s`    | Time a connected listener may lose every packet before it is dropped. |

### The stream

| Flag           | Default  | Meaning                                                                                   |
| -------------- | -------- | ----------------------------------------------------------------------------------------- |
| `-mode`        | `fanout` | How the stream reaches listeners: `fanout`, `track` or `naive` (see below).               |
| `-frame`       | `20ms`   | Audio in each packet: `20ms`, `40ms` or `60ms`.                                           |
| `-batch`       | `0`      | Packets sent with one system call (`sendmmsg`, Linux). `0` sends one at a time.           |
| `-batch-delay` | `0`      | Time to wait for more packets to join a batch. `0` sends at once.                         |
| `-shards`      | `0`      | Shards of the router. `0` is one for each CPU core.                                       |
| `-queue`       | `0`      | Packets a listener may have waiting. `0` is 32, or none with `-batch`. Below `0` has no queues. |

### Limits

| Flag              | Default | Meaning                                                                         |
| ----------------- | ------- | ------------------------------------------------------------------------------- |
| `-max-listeners`  | `0`     | The largest number of listeners at once, connected or connecting. `0` is 5000, a negative value has no limit. |
| `-max-connecting` | `0`     | The largest number of listeners at once that have not connected yet. `0` is 1000, a negative value has no limit. |

### The AI presenter

| Flag                       | Default                  | Meaning                                                           |
| -------------------------- | ------------------------ | ----------------------------------------------------------------- |
| `-presenter`               | off                      | An AI presenter introduces the next track in a voice.             |
| `-presenter-tts`           | none                     | The speech program, with `{out}` for the file it writes. The text goes to its standard input. |
| `-presenter-tts-ext`       | `.wav`                   | Extension of the file the speech program writes.                  |
| `-presenter-model`         | `phi3.5`                 | The Ollama model that writes what the presenter says.             |
| `-presenter-ollama`        | `http://127.0.0.1:11434` | The Ollama server. It must be on this machine unless `-presenter-remote-model` is set. |
| `-presenter-remote-model`  | off                      | Allow an Ollama server that is not on this machine.               |
| `-presenter-name`          | `Go radio`               | The name of the station that the presenter says.                  |
| `-presenter-margin`        | `3s`                     | Time kept before the end of a track for the clip to be picked up. |
| `-presenter-seed`          | none                     | Seed for the choices of the model, where the model supports one.  |

### TURN

| Flag                  | Default | Meaning                                                                  |
| --------------------- | ------- | ------------------------------------------------------------------------ |
| `-turn-url`           | none    | `turn:` or `turns:` addresses of a TURN server, comma separated, for `GET /api/ice`. |
| `-turn-secret-file`   | none    | File with the secret shared with the TURN server, at least 16 bytes.     |
| `-turn-ttl`           | `1h`    | How long the TURN credentials that are handed out last.                  |

### Diagnostics

| Flag                | Default | Meaning                                                                        |
| ------------------- | ------- | ------------------------------------------------------------------------------ |
| `-runtime-interval` | `1s`    | Time between readings of the runtime health. `0` or less reads none.           |
| `-runtime-csv`      | none    | Directory to write the last readings to, as CSV, when the server stops.        |
| `-pprof`            | off     | Serve `/debug/pprof`. It must not be reachable by the public.                  |

## The three modes

`-mode` exists so that the design can be measured against alternatives. The default, `fanout`, is the recommended mode.

| Mode     | What it runs                                                              | Trade-off                                                   |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `fanout` | One playout and the router.                                                 | The recommended design.                                     |
| `track`  | One playout, and one Pion track that every listener is bound to.            | Needs no router and no queues. A listener that blocks the write holds up everyone. |
| `naive`  | A playout for each listener, each with its own goroutine, timer and place in the playlist. | The baseline: a service that handles every listener on its own. The listeners do not hear the same moment. |

## The API

| Method   | Path                | Answer                                                                                  |
| -------- | ------------------- | --------------------------------------------------------------------------------------- |
| `GET`    | `/`                 | The listener page, when the server is given `-web`.                                     |
| `POST`   | `/api/listen`       | Takes the SDP offer of the browser. Answers with the SDP answer and `Location: listen/<id>`. The answer holds every ICE candidate, because the browser and the server do not trickle them. |
| `DELETE` | `/api/listen/<id>`  | Ends that listener.                                                                     |
| `GET`    | `/api/playlist`     | JSON `data`: the track now playing, `flags`, `said` (the last words of the presenter) and the list of tracks. |
| `GET`    | `/api/ice`          | JSON `data.iceServers`: new, short-lived TURN credentials, or an empty list.            |
| `GET`    | `/api/metrics`      | Counters and gauges as JSON, with the health of the Go runtime under `runtime`.         |
| `GET`    | `/api/runtime`      | The last 300 readings of the health of the Go runtime, as CSV: five minutes at the default interval of a second. |
| `GET`    | `/debug/pprof/...`  | The Go profiler, only with `-pprof`.                                                    |

A JSON answer is an envelope: `{"data": ...}` on success, and `{"error": {"status", "num", "code", "message"}}` on
failure, with `params` in the error when it has any. `/api/metrics` is the exception: it writes its JSON without the
envelope, because a program reads it, and `/api/runtime` is CSV. A body larger than 64 KB is refused with 413 and the code
`TOO_LARGE_BODY`.

## Docker and the stand

| Command                         | What it does                                                                           |
| ------------------------------- | -------------------------------------------------------------------------------------- |
| `make docker-build`             | Builds both images.                                                                    |
| `make docker-build-backend`     | Builds `go-webrtc-audio-backend`: the backend, ffmpeg and the sample tracks (about 235 MB). |
| `make docker-build-frontend`    | Builds `go-webrtc-audio-frontend`: the built page on the official nginx image (about 90 MB). |
| `make stand-up`                 | Starts the stand with docker compose. The page is on `http://localhost:8080`.          |
| `make stand-up-ai`              | Starts the stand with Ollama and the AI presenter.                                     |
| `make stand-up-turn`            | Starts the stand with a TURN server (coturn).                                          |
| `make stand-check`              | Checks what is public, what is not, the headers and the user nginx runs as.            |
| `make stand-load`               | Runs the load generator inside the stand (`LISTENERS=500 HOLD=60s` to change it).      |
| `make stand-down`               | Stops the stand.                                                                       |

Docker facts:

- The base images are `golang`, `node`, `alpine`, `nginx` and `debian` (Docker Official Images), and `coturn` and
  `ollama` (the official images of those projects). They are pinned by digest in `scripts/versions.env`. The compose
  files have no copy of them, so docker compose needs `--env-file scripts/versions.env`. The Makefile passes it, and
  `--env-file .env` after it when the file is there, because compose skips `.env` once an `--env-file` is given.
- nginx runs as the `nginx` user and serves the page with a Content-Security-Policy.
- nginx passes only four routes to the backend: `POST /api/listen`, `DELETE /api/listen/<number>`, `GET /api/ice` and
  `GET /api/playlist`.
- The address of the backend is `BACKEND_URL`. nginx looks it up again every few seconds through `DNS_RESOLVER`. The
  default is the resolver of Docker.
- The audio does not pass through nginx. It goes from the backend to the browser over UDP, so the UDP port of the
  backend must be reachable.
- `compose.yaml` publishes that port. It also publishes the API of the backend on `127.0.0.1:8081`, for the metrics and
  the profiler.

Settings come from the environment or from a `.env` file:

| Setting                      | Default            | Meaning                                                        |
| ---------------------------- | ------------------ | -------------------------------------------------------------- |
| `ICE_HOST`                   | `127.0.0.1`        | The address that a browser reaches the UDP port of the backend on. Other devices need the address of the machine. |
| `MODE`, `FRAME`, `BATCH`     | `fanout`, `20ms`, `64` | The same settings as the flags `-mode`, `-frame` and `-batch`. |
| `QUEUE`                      | `0`                | The flag `-queue`. `0` means 32 packets, `-1` means no queue.  |
| `RUNTIME_INTERVAL`           | `1s`               | The flag `-runtime-interval`.                                  |
| `PRESENTER`                  | `false`            | The flag `-presenter`. `make stand-up-ai` turns it on.         |
| `PRESENTER_TTS`              | none               | The flag `-presenter-tts`. `make stand-up-ai` sets `espeak-ng`.|
| `PRESENTER_OLLAMA`           | `http://127.0.0.1:11434` | The flag `-presenter-ollama`. `make stand-up-ai` sets the `ollama` service. |
| `PRESENTER_MODEL`            | `phi3.5`           | The flag `-presenter-model`.                                   |
| `TURN_URL`                   | none               | The flag `-turn-url`. `make stand-up-turn` sets it.            |
| `TURN_SECRET_FILE`           | none               | The flag `-turn-secret-file`. `make stand-up-turn` sets `/run/secrets/turn_secret`. |
| `TURN_SECRET`                | none               | The secret itself. `make turn-secret` writes it into `.env`.   |
| `TURN_HOST`, `TURN_PORT`     | `127.0.0.1`, `3478` | The address and the port that a browser reaches coturn on.    |
| `LISTENERS`, `HOLD`          | `100`, `30s`       | The size and the length of `make stand-load`.                  |

### Licences of the images

- The backend image contains `ffmpeg`.
- The image that `make stand-up-ai` builds also contains `espeak-ng`, which is GPL-3.0.
- Both are separate programs under their own licences. The project does not link them.
- Whoever publishes an image takes on the duties of those licences, such as offering the source code.

### A known problem with the UDP port

A published UDP port can stop forwarding while the page still loads and the offers are answered. The cause is not known.

The targets `make stand-up`, `stand-up-ai` and `stand-up-turn` end with `scripts/udp-check.sh`. It connects one real
listener from this machine through the published ports.

- If it hears nothing, the stand is brought down and up once more and checked again.
- If that does not help, the target fails.
- `make stand-check` runs the same check.

## Privacy notes for operators

IP addresses are personal data under the GDPR. The server and the page handle them. An operator should know where they
appear.

| Part         | What it does with addresses and user data                                                              |
| ------------ | ------------------------------------------------------------------------------------------------------ |
| The backend  | It logs no listener address, user agent or SDP offer. The log lines of the HTTP server and of Pion have every address removed by `edge.AddressHandler`. While a connection lasts, it keeps the address in memory. It never writes it to disk. Its counters and `/api/runtime` are totals and readings of the Go runtime. |
| The page     | It sets no cookies, uses no local storage and has no analytics.                                        |
| nginx        | It writes an access log with the client IP and the user agent of every request. The log goes to the standard output of the container. The config has no `access_log` line, so the default applies. |
| coturn       | It logs the addresses of the peers that it relays. It uses `simple-log` and `log-file=stdout`.         |
| The presenter | It sends track titles and file names to the model. The server refuses a model that is not on the same machine, unless `-presenter-remote-model` is set. |

What an operator can change:

- `access_log off;` in `apps/frontend/nginx.conf.template` switches the nginx log off.
- Docker keeps container logs without a size limit, unless a logging option sets one.
- A real deployment of coturn lowers the log level or sets a retention limit.

## Tests

| Area       | What the tests check                                                                                       |
| ---------- | ---------------------------------------------------------------------------------------------------------- |
| Engine     | A client connects in each of the three modes, reads audio and leaves. A listener that leaves while it joins is never left behind. The limit refuses, and ids are not sequential. |
| Router     | Order and delivery. A stuck listener loses its own packets and is evicted without holding back the others. The router keeps none of the bytes of the caller. |
| Playout      | Sequence numbers and timestamps, one packet every 20 ms, and no allocation when a packet is sent.          |
| Transport  | Every queued packet arrives. A packet that cannot be sent does not stop the rest. The address lookup makes no allocation. The lock-free ring loses nothing with many writers. |
| API        | The `/api` prefix, the refusals and the relative `Location`.                                               |
| Presenter  | With a fake model and a fake voice: the deadline, a track that is too short, the fallback line, failures, and a newer clip that replaces an older one. |
| Settings   | The rules of the settings, the flags and their defaults, and the presenter and the TURN issuer that `Env` builds. A bad setting stops the start with a message that names it. |
| Style      | The analyzers of `apps/backend/tools/check`, with their own test files and their fixes. |
| Leaks      | The tests of the playlist, the playout, the router, the transport, the HTTP plumbing and the API run under `goleak`. |
| Page       | The connection logic has no React in it, so its unit tests run in Node with fake timers: a first failed attempt is shown and not retried, a connection that heals within three seconds is kept, a lost one is replaced after 1, 2, 4, 8 and then 10 seconds, stale answers are ignored, and Stop ends everything. |
| Browser    | Chromium presses Listen and waits for sound and counted packets. It kills and restarts the backend and waits for the stream to return. Stop, a Stop while connecting and a closed tab leave no listener on the server. |
| The stand  | `scripts/check-stand.sh` asserts 25 things about the running stand.                                        |

The backend tests start real Pion connections inside the test process.

## Load tests and profiles

| Command                                        | What it does                                                                  |
| ---------------------------------------------- | ----------------------------------------------------------------------------- |
| `make loadtest`                                | Runs every mode at 10, 100 and 500 listeners and writes the table `out/loadtest/results.md`. |
| `make profile MODE=fanout LISTENERS=500`       | Writes CPU, heap and goroutine profiles into `out/pprof/` (kept local, not in git). |
| `make perf LISTENERS=200`                      | Splits the CPU time of the running stand into Go, kernel and vDSO.            |
| `make runtime-health NAME=run QUEUE=-1`        | Shows what the runtime monitor of the server saw under load.                  |
| `make evict-time LISTENERS=20`                 | Times how long the server takes to notice listeners that vanish (Docker).     |

A single run with chosen settings:

```shell
MODES=fanout COUNTS=500 SERVER_FLAGS="-frame 60ms" sh scripts/loadtest.sh
```

`scripts/loadtest.sh` starts a server in each mode and runs `loadgen` against it. Then it writes a table.

`loadgen` connects the listeners with real WebRTC and holds them. Then it disconnects some or all of them and waits for
the server to settle.

| `loadgen` flag      | Meaning                                                                             |
| ------------------- | ----------------------------------------------------------------------------------- |
| `-listeners`        | Number of listeners.                                                                |
| `-hold`             | How long all listeners stay connected (30 s by default).                            |
| `-leave`            | Fraction of the listeners that disconnect after the hold (all by default).          |
| `-concurrency`      | Listeners that connect at once.                                                     |
| `-all-interfaces`   | Gather candidates on every interface, not only on loopback.                         |
| `-json`             | Write the report as JSON to this file.                                              |

The load generator reads the CPU time, the memory and the goroutines of the server from `/api/metrics` once a second. From
every listener, it reads the time to the first packet, the packets lost and the longest silence.

### The health of the runtime

- `GET /api/metrics` carries the latest reading of the Go runtime.
- `GET /api/runtime` returns the last 300 readings as CSV. At the default `-runtime-interval` of one second, that is five
  minutes.
- The readings come from `runtime/metrics`, which does not stop the program.
- The nginx of the page does not pass either route.

The CSV has these columns of readings:

- the time of the reading, in seconds since the Unix epoch;
- goroutines, the number of CPUs that run Go code (`procs`), heap, stacks and total memory;
- the heap size that starts the next collection, and the number of collections so far;
- the allocation rate;
- the share of CPU used by the garbage collector;
- the time spent waiting for mutexes;
- the 99th percentile and the longest value of the waiting time of the scheduler and of the pauses of the collector.
