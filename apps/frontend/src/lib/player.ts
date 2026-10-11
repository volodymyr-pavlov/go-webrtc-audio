import type { Playlist } from '@/api/client.ts';
import { Connection, Issue, Phase } from '@/lib/playerState.ts';
import { has, type Bits } from '@/lib/bitflags.ts';
import { graceMs, retryDelay } from '@/lib/backoff.ts';
import { errorMessage } from '@/lib/errorMessage.ts';
import { Poller } from '@/lib/poller.ts';
import type { OpenSession, Session } from '@/lib/session.ts';
import { resetPlayer, setIssue, setPlayer, setTracks } from '@/store/player.ts';

const trackEveryMs = 2000;
const statsEveryMs = 1000;

/** What the player knows about itself. */
const Flag = {
  /** The listener asked for the stream and has not stopped it. */
  Wanted: 1 << 0,
  /** A connection has come up at least once in this run, so a drop is worth retrying. */
  Established: 1 << 1,
} as const;

export type PlayerDeps = {
  open: OpenSession;
  fetchPlaylist: () => Promise<Playlist>;
  audio: () => HTMLAudioElement | null;
};

type Timer = ReturnType<typeof setTimeout>;

/**
 * Player runs the listener's connection and keeps the store in step with it.
 *
 * It connects when started. A connection that has worked once and then drops is replaced by a new one, after a short
 * wait if it was only disconnected and at once if it failed, with growing pauses between attempts, until {@link
 * Player.stop} is called. A connection that never worked is not retried: the person sees why it failed and decides.
 */
export class Player {
  private readonly deps: PlayerDeps;
  private flags: Bits = 0;
  private attempt = 0;
  // Every connection has a number. Answers and events that belong to an earlier connection are ignored.
  private generation = 0;
  private abort: AbortController | null = null;
  private session: Session | null = null;
  private retryTimer: Timer | null = null;
  private graceTimer: Timer | null = null;
  private readonly playlistPoller: Poller;
  private readonly statsPoller: Poller;

  constructor(deps: PlayerDeps) {
    this.deps = deps;
    this.playlistPoller = new Poller({
      everyMs: trackEveryMs,
      immediate: true,
      run: (live) => this.pollPlaylist(live),
      onStale: (stale) => setIssue(Issue.NoPlaylist, stale),
    });
    this.statsPoller = new Poller({
      everyMs: statsEveryMs,
      immediate: false,
      run: (live) => this.pollStats(live),
      onStale: (stale) => setIssue(Issue.NoStats, stale),
    });
  }

  /** Reads the list of tracks, so that it can be shown before anyone presses Listen. A failure is ignored. */
  load(): void {
    this.deps.fetchPlaylist().then(
      ({ tracks, flags, said }) => {
        setTracks(tracks);
        setPlayer({ presenter: flags, said });
      },
      () => {
        // The list is shown as soon as a poll while listening gets it.
      },
    );
  }

  /** Starts listening. It does nothing if the player is already running. */
  start(): void {
    if (has(this.flags, Flag.Wanted)) {
      return;
    }
    this.flags = Flag.Wanted;
    this.attempt = 0;
    resetPlayer({ phase: Phase.Connecting });
    this.playlistPoller.start();
    this.statsPoller.start();
    void this.connect((error) => this.fail(errorMessage(error)));
  }

  /** Tries to start the sound again after the browser refused it. Call it from a click or a tap. */
  resume(): void {
    void this.session?.resume();
  }

  /** Stops listening and puts the page back to its first state. */
  stop(): void {
    this.end();
    resetPlayer();
  }

  private end(): void {
    this.flags = 0;
    this.generation++;
    this.clearTimers();
    this.dropSession();
  }

  /** Cancels the waits for a retry and for the grace period, and stops the polling. */
  private clearTimers(): void {
    for (const timer of [this.retryTimer, this.graceTimer]) {
      if (timer !== null) {
        clearTimeout(timer);
      }
    }
    this.retryTimer = this.graceTimer = null;
    this.playlistPoller.stop();
    this.statsPoller.stop();
  }

  /** Ends the connection that is up or being made. */
  private dropSession(): void {
    this.abort?.abort();
    this.abort = null;
    const session = this.session;
    this.session = null;
    void session?.close();
  }

  private fail(message: string): void {
    this.end();
    resetPlayer({ phase: Phase.Error, error: message });
  }

  /** Opens a connection. A failure goes to `onFailure`, which decides whether the listener sees it or it is retried. */
  private async connect(onFailure: (error: unknown) => void): Promise<void> {
    const audio = this.deps.audio();
    if (audio === null) {
      this.fail('The page has no audio element');
      return;
    }
    const generation = ++this.generation;
    const controller = new AbortController();
    this.abort = controller;
    try {
      const session = await this.deps.open(
        audio,
        (state) => this.onConnectionState(generation, state),
        controller.signal,
        (blocked) => {
          if (generation === this.generation) {
            setIssue(Issue.Blocked, blocked);
          }
        },
      );
      if (generation !== this.generation) {
        // Stopped or replaced while the connection was being made.
        void session.close();
        return;
      }
      this.session = session;
    } catch (error) {
      if (generation !== this.generation) {
        return;
      }
      onFailure(error);
    }
  }

  private onConnectionState(generation: number, state: Connection): void {
    if (generation !== this.generation || !has(this.flags, Flag.Wanted)) {
      return;
    }
    setPlayer({ connection: state });
    const established = has(this.flags, Flag.Established);
    switch (state) {
      case Connection.Connected:
        this.clearGrace();
        this.flags |= Flag.Established;
        this.attempt = 0;
        setPlayer({ phase: Phase.Playing, error: '' });
        break;
      case Connection.Disconnected:
        if (established && this.graceTimer === null) {
          setPlayer({ phase: Phase.Lost });
          this.graceTimer = setTimeout(() => {
            this.graceTimer = null;
            this.reconnect();
          }, graceMs);
        }
        break;
      case Connection.Failed:
        if (established) {
          this.reconnect();
        } else {
          this.fail('The connection could not be set up');
        }
        break;
    }
  }

  private clearGrace(): void {
    if (this.graceTimer !== null) {
      clearTimeout(this.graceTimer);
      this.graceTimer = null;
    }
  }

  private reconnect(): void {
    if (!has(this.flags, Flag.Wanted)) {
      return;
    }
    this.generation++;
    this.clearGrace();
    this.dropSession();
    // The numbers of the old connection mean nothing for the new one.
    setPlayer({ packets: 0, lost: 0, connection: Connection.Idle });
    this.scheduleRetry();
  }

  private scheduleRetry(): void {
    if (!has(this.flags, Flag.Wanted) || this.retryTimer !== null) {
      return;
    }
    setPlayer({ phase: Phase.Reconnecting });
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.connect(() => this.scheduleRetry());
    }, retryDelay(this.attempt++));
  }

  private pollPlaylist(live: () => boolean): Promise<void> {
    return this.deps.fetchPlaylist().then(({ track, flags, said, tracks }) => {
      if (live()) {
        setPlayer({ track, presenter: flags, said });
        setTracks(tracks);
      }
    });
  }

  private pollStats(live: () => boolean): Promise<void> | null {
    const session = this.session;
    if (session === null) {
      return null;
    }
    return session.stats().then(({ packets, lost }) => {
      if (live() && this.session === session) {
        setPlayer({ packets, lost });
      }
    });
  }
}
