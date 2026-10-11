import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { PlaylistFlag, type Playlist } from '@/api/client.ts';
import { Connection, Issue, Phase } from '@/lib/playerState.ts';
import { has } from '@/lib/bitflags.ts';
import { graceMs, retryDelay } from '@/lib/backoff.ts';
import { Player } from '@/lib/player.ts';
import { staleAfter } from '@/lib/poller.ts';
import type { OpenSession, Session } from '@/lib/session.ts';
import { clearPlayer, usePlayerStore } from '@/store/player.ts';

type Pending = {
  session: Session;
  close: ReturnType<typeof vi.fn<() => Promise<void>>>;
  onState: (state: Connection) => void;
  onBlocked: (blocked: boolean) => void;
  signal: AbortSignal;
  resolve: () => void;
  reject: (error: unknown) => void;
};

type Options = {
  /** When true, a connection stays in the making until the test resolves or rejects it. */
  manual?: boolean;
  /** Makes the connections that are made automatically fail. */
  failOpen?: () => boolean;
  /** Makes the page have no audio element. */
  noAudio?: boolean;
};

/** A player whose connections are fakes, and the list of them. */
function newPlayer(options: Options = {}) {
  const opened: Pending[] = [];
  const open = vi.fn<OpenSession>(
    (_audio, onState, signal, onBlocked) =>
      new Promise<Session>((resolve, reject) => {
        const close = vi.fn(async () => {});
        const session: Session = { close, stats: async () => ({ packets: 7, lost: 1 }), resume: vi.fn(async () => {}) };
        const pending: Pending = {
          session,
          close,
          onState,
          onBlocked,
          signal,
          resolve: () => resolve(session),
          reject,
        };
        opened.push(pending);
        if (options.manual !== true) {
          if (options.failOpen?.()) {
            reject(new Error('refused'));
          } else {
            resolve(session);
          }
        }
      }),
  );
  const fetchPlaylist = vi.fn<() => Promise<Playlist>>(async () => ({
    track: '02-fifth.wav',
    flags: 0,
    said: '',
    tracks: [
      { name: '01-third.wav', seconds: 30 },
      { name: '02-fifth.wav', seconds: 25 },
    ],
  }));
  const player = new Player({
    open,
    fetchPlaylist,
    audio: () => (options.noAudio === true ? null : ({} as HTMLAudioElement)),
  });
  return {
    player,
    open,
    opened,
    fetchPlaylist,
    /** Sends a connection state from the newest connection. */
    emit: (state: Connection) => opened.at(-1)?.onState(state),
  };
}

const phase = () => usePlayerStore.getState().phase;
const settle = () => vi.advanceTimersByTimeAsync(0);

beforeEach(() => {
  vi.useFakeTimers();
  clearPlayer();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('Player', () => {
  it('shows connecting and the track name at once, then playing when the connection is up', async () => {
    const fake = newPlayer();
    fake.player.start();
    expect(phase()).toBe(Phase.Connecting);
    await settle();
    expect(usePlayerStore.getState().track).toBe('02-fifth.wav');
    expect(usePlayerStore.getState().tracks.map((t) => t.name)).toEqual(['01-third.wav', '02-fifth.wav']);

    fake.emit(Connection.Connected);
    expect(phase()).toBe(Phase.Playing);
    expect(usePlayerStore.getState().connection).toBe(Connection.Connected);
    fake.player.stop();
  });

  it('starts only once however often it is told to', async () => {
    const fake = newPlayer();
    fake.player.start();
    fake.player.start();
    await settle();
    expect(fake.open).toHaveBeenCalledTimes(1);
    fake.player.stop();
  });

  it('shows why the first attempt failed and does not try again', async () => {
    const fake = newPlayer({ failOpen: () => true });
    fake.player.start();
    await settle();
    expect(phase()).toBe(Phase.Error);
    expect(usePlayerStore.getState().error).toBe('refused');

    await vi.advanceTimersByTimeAsync(60_000);
    expect(fake.open).toHaveBeenCalledTimes(1);
  });

  it('shows an error when the page has no audio element', async () => {
    const fake = newPlayer({ noAudio: true });
    fake.player.start();
    await settle();
    expect(phase()).toBe(Phase.Error);
    expect(fake.open).not.toHaveBeenCalled();
  });

  it('does not retry a connection that failed before it ever worked', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Failed);
    expect(phase()).toBe(Phase.Error);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fake.open).toHaveBeenCalledTimes(1);
  });

  it('takes a connection state that arrives before the connection is open', async () => {
    const fake = newPlayer({ manual: true });
    fake.player.start();
    fake.opened[0]!.onState(Connection.Connected);
    expect(phase()).toBe(Phase.Playing);
    fake.opened[0]!.resolve();
    await settle();
    expect(phase()).toBe(Phase.Playing);
    fake.player.stop();
  });

  it('keeps a connection that heals within the grace period', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.emit(Connection.Disconnected);
    expect(phase()).toBe(Phase.Lost);
    fake.emit(Connection.Disconnected);

    await vi.advanceTimersByTimeAsync(graceMs - 1000);
    fake.emit(Connection.Connected);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(phase()).toBe(Phase.Playing);
    expect(fake.open).toHaveBeenCalledTimes(1);
    fake.player.stop();
  });

  it('replaces a connection that stays disconnected, waiting longer after each failed attempt', async () => {
    let refuse = false;
    const fake = newPlayer({ failOpen: () => refuse });
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    refuse = true;
    fake.emit(Connection.Disconnected);

    await vi.advanceTimersByTimeAsync(graceMs);
    expect(phase()).toBe(Phase.Reconnecting);
    expect(fake.opened[0]!.close).toHaveBeenCalledTimes(1);
    expect(fake.open).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(retryDelay(0));
    expect(fake.open).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(retryDelay(1) - 1);
    expect(fake.open).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(fake.open).toHaveBeenCalledTimes(3);

    refuse = false;
    await vi.advanceTimersByTimeAsync(retryDelay(2));
    expect(fake.open).toHaveBeenCalledTimes(4);
    fake.emit(Connection.Connected);
    expect(phase()).toBe(Phase.Playing);
    fake.player.stop();
  });

  it('waits one second again after a connection that was got back drops', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.emit(Connection.Failed);
    await vi.advanceTimersByTimeAsync(retryDelay(0));
    expect(fake.open).toHaveBeenCalledTimes(2);
    fake.emit(Connection.Connected);

    fake.emit(Connection.Failed);
    await vi.advanceTimersByTimeAsync(retryDelay(0) - 1);
    expect(fake.open).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(fake.open).toHaveBeenCalledTimes(3);
    fake.player.stop();
  });

  it('replaces a failed connection without waiting for the grace period, and clears its numbers', async () => {
    const fake = newPlayer();
    fake.player.start();
    await vi.advanceTimersByTimeAsync(1000);
    fake.emit(Connection.Connected);
    await vi.advanceTimersByTimeAsync(1000);
    expect(usePlayerStore.getState().packets).toBe(7);

    fake.emit(Connection.Failed);
    expect(phase()).toBe(Phase.Reconnecting);
    expect(usePlayerStore.getState()).toMatchObject({ packets: 0, lost: 0, connection: Connection.Idle });
    await vi.advanceTimersByTimeAsync(retryDelay(0));
    expect(fake.open).toHaveBeenCalledTimes(2);
    fake.player.stop();
  });

  it('ignores a failure that a replaced connection reports late', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.emit(Connection.Failed);
    await vi.advanceTimersByTimeAsync(retryDelay(0));
    fake.emit(Connection.Connected);

    fake.opened[0]!.onState(Connection.Failed);
    expect(phase()).toBe(Phase.Playing);
    fake.player.stop();
  });

  it('stop closes the connection, clears the page and stops polling', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.player.stop();

    expect(fake.opened[0]!.close).toHaveBeenCalledTimes(1);
    expect(usePlayerStore.getState()).toMatchObject({ phase: Phase.Idle, track: '', packets: 0 });
    const polls = fake.fetchPlaylist.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(fake.fetchPlaylist).toHaveBeenCalledTimes(polls);
  });

  it.each([
    ['lost', Connection.Disconnected],
    ['reconnecting', Connection.Failed],
  ])('stop while the connection is %s leaves no timer running', async (_name, state) => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.emit(state);
    fake.player.stop();
    expect(vi.getTimerCount()).toBe(0);
    expect(phase()).toBe(Phase.Idle);
  });

  it('closes a connection that finishes opening after stop, and tells it to give up', async () => {
    const fake = newPlayer({ manual: true });
    fake.player.start();
    fake.player.stop();
    expect(fake.opened[0]!.signal.aborted).toBe(true);
    fake.opened[0]!.resolve();
    await settle();
    expect(fake.opened[0]!.close).toHaveBeenCalledTimes(1);
    expect(phase()).toBe(Phase.Idle);
  });

  it('keeps a stale connection from touching the page after stop and start', async () => {
    const fake = newPlayer({ manual: true });
    fake.player.start();
    fake.player.stop();
    fake.player.start();
    expect(fake.opened).toHaveLength(2);

    fake.opened[0]!.onState(Connection.Connected);
    expect(phase()).toBe(Phase.Connecting);
    fake.opened[0]!.resolve();
    await settle();
    expect(fake.opened[0]!.close).toHaveBeenCalledTimes(1);
    expect(fake.opened[1]!.close).not.toHaveBeenCalled();

    fake.opened[1]!.resolve();
    await settle();
    fake.opened[1]!.onState(Connection.Connected);
    expect(phase()).toBe(Phase.Playing);
    fake.player.stop();
  });

  it('ignores a track name that arrives after stop and start', async () => {
    let answer: (name: string) => void = () => {};
    const fake = newPlayer({ manual: true });
    fake.fetchPlaylist.mockImplementationOnce(
      () =>
        new Promise<Playlist>(
          (resolve) => (answer = (name) => resolve({ track: name, flags: 0, said: '', tracks: [] })),
        ),
    );
    fake.player.start();
    fake.player.stop();
    fake.player.start();
    answer('stale.wav');
    await settle();
    expect(usePlayerStore.getState().track).not.toBe('stale.wav');
    fake.player.stop();
  });

  it('keeps the last track name and numbers when a poll fails', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    await vi.advanceTimersByTimeAsync(1000);
    fake.fetchPlaylist.mockRejectedValue(new Error('down'));
    fake.opened[0]!.session.stats = async () => {
      throw new Error('down');
    };
    await vi.advanceTimersByTimeAsync(4000);
    expect(usePlayerStore.getState()).toMatchObject({ track: '02-fifth.wav', packets: 7 });
    fake.player.stop();
  });

  it('reads the packet counts while listening', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    await vi.advanceTimersByTimeAsync(1000);
    expect(usePlayerStore.getState()).toMatchObject({ packets: 7, lost: 1 });
    fake.player.stop();
  });

  it('loads the list of tracks before anyone listens, and keeps it when the player stops', async () => {
    const fake = newPlayer();
    fake.player.load();
    await settle();
    expect(fake.open).not.toHaveBeenCalled();
    expect(usePlayerStore.getState().tracks).toHaveLength(2);
    expect(usePlayerStore.getState().phase).toBe(Phase.Idle);

    fake.player.start();
    await settle();
    fake.player.stop();
    expect(usePlayerStore.getState().tracks).toHaveLength(2);
    expect(usePlayerStore.getState().track).toBe('');
  });

  it('does not replace the list of tracks with one that is the same, so that nothing is redrawn', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    const first = usePlayerStore.getState().tracks;
    await vi.advanceTimersByTimeAsync(6000);
    expect(usePlayerStore.getState().tracks).toBe(first);
    fake.player.stop();
  });

  it('ignores a failure to load the list', async () => {
    const fake = newPlayer();
    fake.fetchPlaylist.mockRejectedValueOnce(new Error('down'));
    fake.player.load();
    await settle();
    expect(usePlayerStore.getState().tracks).toEqual([]);
  });

  it('knows about the presenter before anyone listens, and keeps what it said when the player stops', async () => {
    const fake = newPlayer();
    fake.fetchPlaylist.mockResolvedValue({
      track: '02-fifth.wav',
      flags: PlaylistFlag.Presenter,
      said: 'Up next, fifth.',
      tracks: [{ name: '02-fifth.wav', seconds: 25 }],
    });
    // The page asks once when it opens: the place of the presenter is there, with its last words, before Listen.
    fake.player.load();
    await settle();
    expect(usePlayerStore.getState()).toMatchObject({ presenter: PlaylistFlag.Presenter, said: 'Up next, fifth.' });

    fake.player.start();
    await settle();
    fake.fetchPlaylist.mockResolvedValue({
      track: '03-octave.wav',
      flags: PlaylistFlag.Presenter | PlaylistFlag.Speaking,
      said: 'Coming up, octave.',
      tracks: [{ name: '03-octave.wav', seconds: 20 }],
    });
    await vi.advanceTimersByTimeAsync(2100);
    expect(usePlayerStore.getState()).toMatchObject({
      presenter: PlaylistFlag.Presenter | PlaylistFlag.Speaking,
      said: 'Coming up, octave.',
    });

    // The voice stops, and the words stay.
    fake.fetchPlaylist.mockResolvedValue({
      track: '03-octave.wav',
      flags: PlaylistFlag.Presenter,
      said: 'Coming up, octave.',
      tracks: [{ name: '03-octave.wav', seconds: 20 }],
    });
    await vi.advanceTimersByTimeAsync(2100);
    expect(usePlayerStore.getState()).toMatchObject({ presenter: PlaylistFlag.Presenter, said: 'Coming up, octave.' });

    // Stopping the player takes away what is the listener's, and keeps what is the server's.
    fake.player.stop();
    expect(usePlayerStore.getState()).toMatchObject({
      presenter: PlaylistFlag.Presenter,
      said: 'Coming up, octave.',
      track: '',
    });
  });

  it('takes the voice away when the player stops while the presenter speaks', async () => {
    const fake = newPlayer();
    fake.fetchPlaylist.mockResolvedValue({
      track: '02-fifth.wav',
      flags: PlaylistFlag.Presenter | PlaylistFlag.Speaking,
      said: 'Up next, fifth.',
      tracks: [{ name: '02-fifth.wav', seconds: 25 }],
    });
    fake.player.start();
    await settle();
    expect(has(usePlayerStore.getState().presenter, PlaylistFlag.Speaking)).toBe(true);
    fake.player.stop();
    const state = usePlayerStore.getState();
    expect(has(state.presenter, PlaylistFlag.Speaking)).toBe(false);
    expect(has(state.presenter, PlaylistFlag.Presenter)).toBe(true);
    expect(state.said).toBe('Up next, fifth.');
  });

  it('shows no place for a presenter when the server has none', async () => {
    usePlayerStore.setState({ presenter: PlaylistFlag.Presenter | PlaylistFlag.Speaking, said: 'old' });
    const fake = newPlayer();
    fake.player.load();
    await settle();
    expect(usePlayerStore.getState()).toMatchObject({ presenter: 0, said: '' });
  });

  it('says so when the browser blocks the sound, and clears it when the sound plays', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.opened[0]!.onBlocked(true);
    expect(has(usePlayerStore.getState().issues, Issue.Blocked)).toBe(true);
    expect(phase()).toBe(Phase.Playing);

    fake.player.resume();
    expect(fake.opened[0]!.session.resume).toHaveBeenCalledTimes(1);
    fake.opened[0]!.onBlocked(false);
    expect(usePlayerStore.getState().issues).toBe(0);
    fake.opened[0]!.onBlocked(true);
    fake.player.stop();
    expect(usePlayerStore.getState().issues).toBe(0);
  });

  it('ignores a report of blocked sound from a replaced connection', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.emit(Connection.Failed);
    fake.opened[0]!.onBlocked(true);
    expect(usePlayerStore.getState().issues).toBe(0);
    fake.player.stop();
  });

  it('marks the track data stale after repeated poll failures and clears it at the next success', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.fetchPlaylist.mockRejectedValue(new Error('down'));
    await vi.advanceTimersByTimeAsync(2000 * (staleAfter - 1));
    expect(has(usePlayerStore.getState().issues, Issue.NoPlaylist)).toBe(false);
    await vi.advanceTimersByTimeAsync(2000);
    expect(has(usePlayerStore.getState().issues, Issue.NoPlaylist)).toBe(true);

    fake.fetchPlaylist.mockResolvedValue({ track: '02-fifth.wav', flags: 0, said: '', tracks: [] });
    await vi.advanceTimersByTimeAsync(2000);
    expect(has(usePlayerStore.getState().issues, Issue.NoPlaylist)).toBe(false);
    fake.player.stop();
  });

  it('marks the connection numbers stale after repeated stats failures', async () => {
    const fake = newPlayer();
    fake.player.start();
    await settle();
    fake.emit(Connection.Connected);
    fake.opened[0]!.session.stats = async () => {
      throw new Error('down');
    };
    await vi.advanceTimersByTimeAsync(1000 * staleAfter);
    expect(has(usePlayerStore.getState().issues, Issue.NoStats)).toBe(true);
    expect(usePlayerStore.getState().packets).toBe(0);
    fake.player.stop();
    expect(usePlayerStore.getState().issues).toBe(0);
  });
});
