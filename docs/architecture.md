# How the radio is built

This page explains the design. It shows the parts of the server, the path of a packet, the habits of the code and the
decisions with their trade-offs. The README has the short version.

## The path of a packet

```
 audio/*     ──ffmpeg, once──▶ Opus packets in memory ──▶ playout ──▶ router ──▶ shard ──▶ queue ──▶ writer ──▶ listener
  (any format)                                           20 ms     │            │
                                                                   │            └─▶ queue ──▶ writer ──▶ listener
                                                                   └─▶ shard ...
```

With `-batch`, the shard writes to the listeners itself, and there is no queue and no writer for each listener.

## The parts

| Part       | Package            | What it does                                                                                   |
| ---------- | ------------------ | ---------------------------------------------------------------------------------------------- |
| Playlist   | `playlist`         | Turns each audio file into Opus packets once and keeps them in memory.                         |
| Playout      | `stream`           | One goroutine. It sends the next packet every frame and writes it to an `io.Writer`.           |
| Router     | `fanout`           | The writer of the playout. It parses each packet once and gives it to the shards.                |
| Shards     | `fanout`           | One for each CPU core by default. Each shard sends the packet to its listeners.                |
| Engine     | `engine`           | The WebRTC side, built on Pion. It adds a listener when the connection is up and removes it when it fails or closes. |
| Transport  | `transport`        | An optional UDP socket that sends packets in batches. On Linux it uses `sendmmsg`.             |
| API        | `api`, `edge`      | One type per route, on fasthttp and a small package where a handler returns an error.          |
| Presenter  | `ai`               | The AI presenter. See [presenter.md](presenter.md).                                           |
| Settings   | `config`           | The settings, and `Env`, which builds and holds every part above, once. `api.NewServer` wires the handlers to it, and `launch` runs the server until the process is told to stop. |

## How the parts work together

- A packet is a slice of memory that never changes. All listeners hold the same one.
- The cursor of the playlist has one atomic position. It walks over the packets and wraps around.
- The playout keeps time on the monotonic clock. If it wakes up late, it sends up to five late packets at once, so the
  stream does not drift. After a longer stall it skips audio, because a listener cannot use audio that is older than its
  buffer.
- The playout does not know who is listening.
- A shard keeps its listeners in a list that is replaced as a whole and never edited. Reading it needs no lock.
- Nothing is copied for each listener.
- The engine answers an offer with one Opus track that the browser only receives.
- In the API, a mux turns the error of a handler into the answer. The cause of a server error goes to the log and is
  never sent to the client. An answer is JSON in an envelope: `data` on success, `error` on failure.

## Allocations on the hot path

The code avoids allocations where the packets flow. Tests check the playout, the router, the address lookup of the
batching socket, the list of tracks and the CSV of the runtime.

- The packets are built once and shared.
- Sending a packet from the playout makes no allocation.
- The router makes one small copy of each packet. The cost is the same for one listener and for fifty.
- The batching socket reuses its datagram buffers from a pool. No test checks this one.
- The address lookup of the batching socket makes no allocation.
- The list of tracks is encoded once, and reading it makes no allocation.

This covers the code of the project. The work inside Pion, such as encryption, was not measured.

## The habits of the code

- **State is bits.** Modes, phases, the states of an engine, a session, a router or a socket, and the settings are
  bitwise flags. They are not strings or booleans. The page does the same.
- **Atomics, no mutexes.** Counters are atomic. Tables are `sync.Map` or atomic pointers to lists that are replaced
  whole. A state that two goroutines change is a flag word. The one queue that many goroutines write to is a lock-free
  ring in `transport/ring.go`.
- **Standard interfaces.** The playout writes to an `io.Writer`, and the router is one. `Worker` embeds `io.Closer`. The
  metrics and the runtime monitor are `io.WriterTo`. The batching socket is a `net.PacketConn`.
- **Two custom interfaces.** `fanout.PacketWriter` has the method `WriteRTP`, because an `io.Writer` would make every
  listener parse the same bytes again. `edge.Handler` has `ServeHTTP(ctx) error`, because a handler returns its error
  and the mux writes the answer.
- **Small packages.** Each package has a `doc.go` and an `errors.go`. Every error is declared in `errors.go`, with one
  `var` for each, and the style checks refuse an error anywhere else. The option types are in `config.go`, or in
  `options.go` in the `config` package.

## Decisions and trade-offs

The numbers come from one Google Cloud `e2-micro` in October 2026. [How the figures were
measured](#how-the-figures-were-measured) has the method.

| Decision                         | Why                                                                                         | Result                                                                                                  |
| -------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Pion, not GoAV                   | Pion is pure Go, so the server builds into one static program without a C compiler. GoAV binds the FFmpeg libraries through cgo, which pays off only when a server decodes or encodes while it streams. This one does not. | `ffmpeg` runs once at start-up, as a program. It must be installed. The Docker image has it.           |
| Packets are made once            | The cost of a streaming service is mostly per listener, so work that can be done once is done once. | A listener costs the server only the encryption and the sending of its own packets.                     |
| One shared stream (`fanout`)     | Every listener hears the same moment, which a radio needs.                                  | The `naive` mode, with a playout for each listener, used 62 percent more CPU at 100 listeners (19.3 against 11.9). One shared Pion track (`track`) used 17 percent less, but one blocked write holds up every listener. |
| The shard writes to listeners    | A queue and a writer for each listener cost one goroutine for each listener.                | 13 to 18 percent less CPU at 100 listeners. It is the default with `-batch`. Without `-batch` the queues stay, because a blocked send was not tested. `-queue 32` and `-queue -1` choose. |
| A longer frame                   | The system call that sends a packet is the largest part of the cost. A 60 ms frame sends a third of the packets of a 20 ms frame. | 57 percent less CPU at 100 listeners. The price is up to 40 ms more delay and a 60 ms hole where a packet is lost. The default stays at 20 ms, which every client handles. For a one-way stream, 40 or 60 ms is better. |
| Batched sends                    | On Linux, `-batch 64` sends the packets of a frame with one call to `sendmmsg`.             | 4 to 9 percent less CPU at 100 listeners, which two runs could not tell from noise. It is off by default and on in the Docker image. |
| No resends, no mixing            | A one-way Opus stream has its own loss concealment. It does not need Pion's interceptors.   | Pion registers the interceptors unless it is given a registry, so the server gives it an empty one.     |

## How the figures were measured

| Item        | Value                                                                                                  |
| ----------- | ------------------------------------------------------------------------------------------------------ |
| Machine     | Google Cloud `e2-micro`: 2 shared vCPUs, 964 MB of memory.                                   |
| System      | Debian 13, kernel 6.12.90, amd64. A static native build of Go 1.27.1, with no Docker.                  |
| Encoder     | `ffmpeg` 7.1.5 from the distribution.                                                                  |
| Audio       | Three songs of 1:57, 2:41 and 2:49, as Opus at 64 kbit/s, stereo, 48 kHz. They are not part of this repository. |
| Layout      | The server and the load generator ran on the same machine, over loopback.                              |
| Settings    | `-loopback -ice-host 127.0.0.1`, with 20 ms frames unless a comparison says otherwise.                 |
| Date        | 7 October 2026.                                                                                        |

The load generator connects real WebRTC listeners, 32 at a time, and holds them for 40 seconds. CPU is a percent of one
core. It is the CPU time of the server process over the hold, divided by the time.

How the runs were made:

- The ramp from 10 to 500 listeners is one run for each size.
- Each comparison at 100 listeners is two runs of each setting, in opposite orders, 20 seconds apart.
- A difference smaller than the gap between the two runs of one setting is called noise.
- The steal time of the shared core was read for every run. It was 0.0 percent in every run reported here, so the core
  was never held back.
- A first group of runs, made before the steal time was read, is left out.

The figures are readings from one machine. They are a guide to how the settings compare, not a guarantee. The raw data
of the runs is not kept in the repository. The comparisons are consistent with each other, for example 19.3 against 11.9
is 62 percent more.

## Exposure

- nginx passes only four routes to the backend (the list is in [Running](running.md#docker-and-the-stand)).
- So `/api/metrics`, `/api/runtime` and `/debug/pprof` are not public, and `-pprof` is off by default.
- Listener ids are random 64-bit numbers, because whoever knows an id can end that listener.
- The number of listeners is capped. So is the number of listeners that have not connected yet (`-max-connecting`).
  Offers that never connect cannot take every place from the listeners that do.
- nginx allows one address 20 requests a second to `/api/listen` and `/api/ice` together, with a burst of 40.
- Behind another proxy, nginx must be told the real address of the client. Otherwise all listeners share the limit.
- The server makes one DTLS certificate a day and uses it for all connections. The keys of the media are still made for
  each connection.

## Limits

These are the main limits:

- Everything was measured on one Google Cloud `e2-micro`. The load generator ran on the same machine as the server.
  So the numbers compare settings and are not a capacity.
- There is one node, and the audio is Opus only.
- The page was tested in Chromium with automated tests, and once by hand in Safari, a check that is not in the
  repository. Firefox and phones were not tried.
- A browser that refuses to play audio outside a click is not handled.

## What could improve the scaling

None of these was built or measured. They are in the order that this project would try them.

- **Measure on a real network and on bare metal.** The figures above come from a shared-core VM over loopback. A
  dedicated server and a second network would show what the VM hides, and whether the queue default is right when
  `-batch` is off.
- **Try AF_XDP.** It would skip most of the kernel network stack, where much of the CPU time goes. It needs raw packet
  handling and extra privileges, so it was considered and not tried. `-batch` is the baseline to beat.
- **Move the shards into separate processes.** Each process would own some listeners and receive the packets from one
  playout. This would use more cores and isolate a failure, at the cost of a hand-off between processes.
- **Run more than one node.** A load balancer would spread the listeners. Every node would run its own playout, so the
  nodes would need a shared start point, or listeners on different nodes would hear different moments.
- **Compare with Icecast or HLS behind a CDN.** This is the usual answer for one-way radio. The comparison would show
  whether WebRTC is worth its cost for this use.
- **Measure the AI presenter.** The run was not repeated without it, so there is no number for what it adds to the
  server.
- **Measure the cost in the browser.** The browsers play the stream, and the tests and hand checks show that. But the
  load tests used Pion clients, and the browser tests used 20 ms packets. The CPU of Chrome, Firefox and Safari, and
  how they play 40 and 60 ms packets, are not known.
