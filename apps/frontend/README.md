# The listener page

The page is a small React app with one screen. The screen is a station card.

## What the page shows

- A picture with bars. The bars move like a random travelling wave while the stream plays.
- A badge for the state: OFF AIR, TUNING, LIVE, SIGNAL LOST or RETUNING.
- One button that starts and stops the stream.
- A line that says what is happening.
- The track that is playing now.
- The connection state and the packet counts.
- A list of all the tracks, like a list of radio stations. The track on the air is marked.
- A panel for the AI presenter, when the server has one.

## Look and feel

The page uses a light theme. It is built with Tailwind, and the icons come from lucide-react.

Behind the card, a soft gradient from green to violet moves slowly across the page. It stops moving when the system asks
for reduced motion.

The page is called "Go radio". The header is the wordmark: the name set as spaced capitals in the page font, so it
takes its colour from the text and is sharp at any size.

## Where things are

| File                           | What it does                                                                                                                                                                                                                                 |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/lib/player.ts`            | The `Player` class. It starts a connection, keeps the track name and the packet counts up to date, replaces a connection that dropped after it had worked, and stops. It has no React in it, so its unit tests run in Node with fake timers. |
| `src/store/player.ts`          | The zustand store that holds what the page shows.                                                                                                                                                                                            |
| `src/lib/playerState.ts`       | The phases of the player, the stages that look alike, the issues and the connection states, all as bits.                                                                                                                                     |
| `src/lib/bitflags.ts`          | `has` and `pick`, which test and look up bits.                                                                                                                                                                                               |
| `src/lib/poller.ts`            | Polls the playlist and the connection statistics, and says when the polls keep failing.                                                                                                                                                      |
| `src/lib/phaseText.ts`         | The words on the button and in the line under it, for each phase.                                                                                                                                                                            |
| `src/lib/session.ts`           | The one place that uses `RTCPeerConnection`.                                                                                                                                                                                                 |
| `src/api/client.ts`            | The one place that calls the server.                                                                                                                                                                                                         |
| `src/lib/wave.ts`              | The random travelling wave of the bars.                                                                                                                                                                                                      |
| `src/components/Equalizer.tsx` | Moves the bars one frame at a time.                                                                                                                                                                                                          |
| `src/components/Wordmark.tsx`  | The station wordmark.                                                                                                                                                                                                                        |

## Scripts

| Command             | What it does                                                                                                             |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `npm run dev`       | Starts the development server on port 5173.                                                                              |
| `npm run build`     | Type-checks and builds into `.dist`.                                                                                     |
| `npm run preview`   | Serves the built files from `.dist`.                                                                                     |
| `npm run typecheck` | Runs `tsc` for the app, the Vite settings and the browser tests.                                                         |
| `npm run test:unit` | Runs the unit tests once with Vitest.                                                                                    |
| `npm run test:e2e`  | Runs the Playwright browser tests in `e2e/`. They use the dev server and a backend that they start on `127.0.0.1:18081`. |
| `npm run check`     | Runs the type check, the unit tests and the Prettier check.                                                              |
| `npm run format`    | Reformats every file with Prettier.                                                                                      |

From the repository root, these commands are shortcuts:

| Command                 | What it runs                    |
| ----------------------- | ------------------------------- |
| `make frontend-install` | `npm ci`                        |
| `make frontend-check`   | `npm run check`                 |
| `make frontend-build`   | `npm run build`                 |
| `make dev`              | The backend and the Vite server |

## API prefix

The build setting `VITE_API_BASE` is the prefix for the API. The default is `/api`.

- The prefix is used for `listen`, `listen/<id>`, `ice` and `playlist`.
- The development server sends everything under `/api` to the backend at `http://127.0.0.1:8080`.
  `API_PROXY_TARGET` changes this address.
- The backend serves the API under `/api`, so it can serve the page and the API from one address, with
  `server -web apps/frontend/.dist`.

## The image

The `Dockerfile` builds the page with Node and serves it with the official nginx image. nginx runs as the `nginx` user.

nginx does these things:

- It serves the files and caches `/assets/` for a year.
- It adds the security headers and a Content-Security-Policy.
- It passes only four routes to the backend at `BACKEND_URL`. The list is in
  [docs/running.md](../../docs/running.md#docker-and-the-stand).
- It answers every other route under `/api/` with a 404. This includes `/api/metrics` and `/api/runtime`.

nginx passes the request as it came, because the backend serves the API under `/api` itself.

| Command                      | What it does                                  |
| ---------------------------- | --------------------------------------------- |
| `make docker-build-frontend` | Builds the image.                             |
| `make stand-up`              | Runs the image with the backend.              |
| `make stand-check`           | Tests what nginx passes and what it does not. |

## Browser tests

The tests need the backend built by `make backend-build`, which puts it in `.bin/server`.

`e2e/listen.spec.ts` drives the real page in Chromium against a real backend. The test starts and stops the backend on
port 18081. These cases are covered:

- Before anyone listens, the page is off air, and the list of tracks is there with nothing on the air.
- Pressing Listen connects, names the track and puts it on the air.
- The packet count grows, and the server counts one listener.
- The backend is killed and started again, and the page gets the stream back by itself.
- Stop leaves the page off air and the server without a listener.
- A refusal on the first try is shown and is not retried.
- Stop while connecting cancels the connection.
- Closing the tab leaves no listener.
- The place of the AI presenter is always there, with its last words, when the server has a presenter, and a server
  without one has no place for it.

`e2e/turn.spec.ts` runs a Chromium that may only use relay candidates. It needs the stand with a TURN server and the
setting `STAND_URL`. The test checks that the page still hears the stream.
