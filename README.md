# DJ Go on AI Radio

<div>
<a href="https://github.com/volodymyr-pavlov/go-webrtc-audio/actions/workflows/ci.yml"><img src="https://github.com/volodymyr-pavlov/go-webrtc-audio/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
<a href="https://github.com/volodymyr-pavlov/go-webrtc-audio/releases"><img src="https://img.shields.io/github/v/release/volodymyr-pavlov/go-webrtc-audio" alt="Latest release"></a>
<a href="apps/backend/go.mod"><img src="https://img.shields.io/badge/go-1.27-00ADD8.svg?logo=go&amp;logoColor=white" alt="Go 1.27"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</div>

This is an internet radio in Go. The server plays a folder of audio files as one endless playlist. It sends the stream
to as many listeners as connect, over WebRTC. Everyone hears the same moment, like on a real radio.

An optional AI presenter introduces each next track. It can never stop the music.

```shell
make stand-up
make stand-load
make stand-down
```

`make stand-up` builds the backend and the page and starts them. The page is then on `http://localhost:8080`.
`make stand-load` connects 100 fake listeners and prints the report. `make stand-down` stops the stand.

## What is this?

A radio station does two different jobs.

The first job is to send sound to many listeners at a steady rhythm. It can never stop, because every listener would
hear it.

The second job is to decide what the voice between the songs says. This job can be slow, and nobody minds.

The radio keeps the two jobs apart. The first job is a metronome and a fan-out. One playout sends a packet every 20 ms,
and a router shares each packet with every listener. The second job is a local language model that writes a sentence about the
next track.

The two jobs meet in only one place, an atomic pointer. The voice has a deadline. If the voice is late, the track starts
without it.

The project began as an exercise. How far can a plain Go program go? It uses the standard library, Pion for WebRTC,
fasthttp for HTTP, goroutines, atomics and `io.Writer`. It is one static program, with no kernel tuning and no kernel
bypass.

## What it does

- **Listen** in the browser. The Listen button joins the stream where it is.
- **See** the track on the air, the list of tracks and a few connection numbers on the page.
- **Hear the AI presenter.** With `-presenter`, a panel shows the words, and a voice speaks them before each track.
- **Load it.** The load generator connects hundreds of real WebRTC listeners and reports what the server costs.
- **Change how it works.** A flag picks the stream design, the packet length, batched sends and the queue.
- **Add a TURN server** for listeners behind a strict network. The backend hands out short-lived credentials.

## How it is built

| Part          | What it does                                                                                       |
| ------------- | -------------------------------------------------------------------------------------------------- |
| Playlist      | `ffmpeg` makes Opus packets once for each file, at start-up. They stay in memory. Nothing is decoded or encoded while the stream runs. |
| Playout         | One goroutine sends the next packet every frame, by the monotonic clock.                           |
| Router        | Gives each packet to a few shards. Each shard sends it to its listeners. Nothing is copied for each listener. |
| WebRTC        | Pion, in pure Go. Each listener gets one Opus track that the browser only receives.                |
| Presenter     | A local model writes the introduction. A speech program speaks it. `ffmpeg` encodes it into the stream. |
| Page          | React, Vite, Tailwind and zustand. Its connection logic has no React in it, so unit tests run in Node. |
| Tests         | Go tests with the race detector and goroutine-leak checks, unit tests for the page, and Chromium tests against a real backend. |

The code avoids allocations on the hot path. The playout's send step makes none. The router makes one small copy of each
packet, and the cost is the same for one listener or for fifty. Tests check both.

The code uses bit flags for states, atomic counters and no mutexes. It uses standard interfaces such as `io.Writer`.
[docs/architecture.md](docs/architecture.md) has the details, the decisions and their trade-offs.

## Results

All figures come from one Google Cloud `e2-micro`. It has 2 shared vCPUs and 964 MB of memory. The tests ran in October
2026.

The load generator ran on the same machine, over loopback. The figures compare settings with each other. They are a
guide and not a promise of capacity. Each figure is from a single run, and the raw data is not kept in the repository.
[docs/architecture.md](docs/architecture.md#how-the-figures-were-measured) has the method and the limits.

- The machine served about 400 listeners without problems. It used 33 percent of one CPU core and 124 MB, and no packet
  was lost.
- At 500 listeners, 9.4 percent of the packets were lost.
- One listener costs about 0.08 percent of a core, 0.23 MB and 14 goroutines.
- The memory and the goroutines are the price of one WebRTC connection.
- At 100 listeners, a 60 ms packet instead of a 20 ms packet cut the CPU use by 57 percent. The price is up to 40 ms
  more delay.
- At 100 listeners, letting each shard write to its listeners used 13 to 18 percent less CPU. There is no queue and no
  writer for each listener.
- At 100 listeners, a separate playout for each listener used 62 percent more CPU than the router.
- In a test, a listener that vanished without a goodbye was removed after about 11 seconds. The ICE timeouts are 5
  seconds to the state "disconnected" and 5 more to "failed".

## Running it

It needs Go 1.27, `ffmpeg` with libopus on the PATH, Node.js 24 or newer, and Docker for the stand.

| Command            | What it does                                                                       |
| ------------------ | ---------------------------------------------------------------------------------- |
| `make serve`       | Builds the page and the server, writes three test tracks, and serves both on `:8080`. |
| `make dev`         | Runs the server on `:8080` and the page on `:5173` with hot reload.                |
| `make check`       | Backend: `gofmt`, `go vet` and the tests with the race detector.                   |
| `make verify`      | All the checks and the production build of the page.                               |
| `make stand-up-ai` | Starts the stand with Ollama and the AI presenter.                                 |

## More in docs

- [docs/architecture.md](docs/architecture.md): the path of a packet, the habits of the code, and the decisions with
  their trade-offs.
- [docs/running.md](docs/running.md): the requirements, every flag, the three modes, the API, Docker, privacy notes for
  operators, the tests and the load tests.
- [docs/presenter.md](docs/presenter.md): the AI presenter, its rules and what it costs.
- [docs/turn.md](docs/turn.md): TURN for listeners behind a strict network.

## Where things are

```
.
├── apps/
│   ├── backend/           the Go program, and its style checks in tools/
│   └── frontend/          the listener page, its Dockerfile and nginx config
├── audio/                 the audio the server streams
├── docs/                  the pages listed above
├── scripts/               samples, load tests, profiles, stand checks, lint, licences, pinned images
├── release/               semantic-release and its plugins, pinned for the release job
├── lefthook.yml           the git hooks that make hooks installs
├── compose.yaml           the stand: the backend, the page on nginx, and the load generator on request
├── compose.turn.yaml      the stand with a TURN server (coturn): make stand-up-turn
├── compose.ai.yaml        the stand with Ollama and the AI presenter: make stand-up-ai
├── .github/workflows/     ci.yml: the checks, and the release job
├── .golangci.yml          the rules of the linter
├── .releaserc.yml         the plugins of semantic-release, in the order they run
├── CHANGELOG.md           written by the release job
└── Makefile
```

Sample tracks are written to `audio/` only if the folder holds none.

The limits are in [docs/architecture.md](docs/architecture.md#limits). Everything was measured on one small machine,
with the load generator on the same machine as the server.

Fixes are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md). The changes of each release are in
[CHANGELOG.md](CHANGELOG.md).

MIT licensed. See [LICENSE](LICENSE). The licences of the libraries that the page and the server carry are in
[apps/frontend/public/third-party-licenses.txt](apps/frontend/public/third-party-licenses.txt). The Docker images also
contain `ffmpeg` and, for the presenter, `espeak-ng`. They have their own licences. See
[docs/running.md](docs/running.md).
