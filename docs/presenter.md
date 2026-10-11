# The AI presenter

With the flag `-presenter`, the server has a host, like a radio station. The host introduces each next track while the
current track plays.

## What the listener sees

The page has a panel marked "AI presenter". It is always there when the server has a presenter.

- The panel shows the last words of the presenter until the next introduction.
- It shows "Speaking" while the voice is on.
- The answer of `GET /api/playlist` carries the same facts. In `flags`, bit 1 means that the server has a presenter, and bit
  2 means that the voice is on. The last words are in `said`.

## How an introduction is made

1. **Facts.** `ffprobe` and a loudness check give the next track its length, loudness and credits.
2. **Text.** A prompt template, built with LangChain for Go, asks a local model for a sentence or two. The model is
   `phi3.5`, running in Ollama. The prompt has worked examples, because a small model often ignores a plain rule but
   follows an example.
3. **Check.** The answer is treated as untrusted text. If the check fails twice, the server builds a plain line without
   the model.
4. **Voice.** A speech program reads the line. `ffmpeg` turns the sound into the Opus packets of the stream.
5. **Splice.** The finished clip goes into an atomic slot. At the start of a track, the playout looks at the slot and
   plays the clip in front of the track.

## The rules

The presenter works in real time, so the design is about deadlines.

| Rule               | What it means                                                                                          |
| ------------------ | ------------------------------------------------------------------------------------------------------ |
| Its own goroutines | The work runs in `ai/host.go`. The clip reaches the playout through one atomic pointer, `stream.Interludes`. The playout looks at it only where a track begins. Nothing on the path of a packet takes a lock or waits for the model. |
| A deadline         | Every introduction has a deadline: the length of the track that plays, minus 3 seconds. One that is not ready by then is dropped and counted in `late` under `presenter` in `/api/metrics`. The stream goes on without it. A track that begins while the earlier introduction is still being made cancels it. If the playout has skipped audio after a stall, a track can end before the deadline; a clip finished after that is counted as late too, but it is kept and plays when the playlist comes round to its track again. |
| Part of the stream | The packets of the clip take the next sequence numbers and RTP timestamps, so a listener cannot hear the splice. A test checks this. |
| Untrusted text     | The text must be one or two plain sentences. It may have no link, no markup, no number that the facts do not have, no "by" when the data gives no artist, and no welcome after the first track. |
| A fallback         | A text that fails is asked for again once. Then the presenter says a plain line, such as "Next on <station>: <title>." The plain line needs no model. |
| Safe speech        | The speech program gets the text on its standard input, never as an argument. It starts without a shell. |
| A local model      | The model server must be on this machine, because track titles go to the model. The server refuses another one unless `-presenter-remote-model` is set. |
| A warm model       | The model is warmed up at the start, because loading it takes longer than a short track. The files are read in the background. A track that has not been read yet is introduced by its file name. |

The default model, `phi3.5`, is under the MIT licence.

## Running it

| Command                          | What it does                                                                 |
| -------------------------------- | ---------------------------------------------------------------------------- |
| `make stand-up-ai`               | Starts the stand with Ollama and the presenter. The voice is `espeak-ng`, which is robotic but small and offline. |
| `go test -tags live -run Live ./ai` | Asks the real model for introductions and says how many pass the checks.  |

## What it costs

One run lasted 10 minutes on a Linux virtual machine with 14 CPUs. It was made once, and its raw data is not in the
repository. It had 100 listeners and three tracks of 1:57, 2:41
and 2:49.

| Measure                    | Result                                                                                   |
| -------------------------- | ---------------------------------------------------------------------------------------- |
| Introductions              | 5 prepared. 4 had played when the run ended. None was late, none failed, none used the plain line. |
| Time for one introduction  | 2.7 to 5.2 seconds. The deadlines were 114 to 166 seconds.                               |
| Memory of the model        | 4.2 to 4.6 GiB for the whole run.                                                        |
| CPU of the model           | Almost none between introductions. About 14 cores during one.                            |
| The stream                 | The 100 listeners lost no packet.                                                        |

The run was not repeated without the presenter, so there is no number for how much it adds to the server.

At start-up, the model described two of the three tracks. For the third track, the server used a description made only
from the loudness.

The model needs about 4 GB of memory. It does not fit on a small machine such as an `e2-micro`.
