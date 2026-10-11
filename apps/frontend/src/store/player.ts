import { create } from 'zustand';

import { PlaylistFlag, type TrackInfo } from '@/api/client.ts';
import { Connection, Phase } from '@/lib/playerState.ts';
import { type Bits } from '@/lib/bitflags.ts';

export type PlayerState = {
  phase: Phase;
  /** Why the first attempt failed, when the phase is `Error`. */
  error: string;
  /** The name of the track that plays now. */
  track: string;
  /**
   * The [PlaylistFlag] bits of the server's last answer: whether it has an AI presenter, which the page shows a place for
   * for as long as it has, and whether the presenter's voice is on now.
   */
  presenter: Bits;
  /** The last thing the presenter said. It stays when the voice stops and when the player does. */
  said: string;
  /** All the tracks, in the order they play. The list stays when the player stops. */
  tracks: TrackInfo[];
  connection: Connection;
  /** The [Issue] bits that are set now. */
  issues: Bits;
  packets: number;
  lost: number;
};

const initial: PlayerState = {
  phase: Phase.Idle,
  error: '',
  track: '',
  presenter: 0,
  said: '',
  tracks: [],
  connection: Connection.Idle,
  issues: 0,
  packets: 0,
  lost: 0,
};

export const usePlayerStore = create<PlayerState>(() => initial);

export function setPlayer(patch: Partial<PlayerState>): void {
  usePlayerStore.setState(patch);
}

/** Sets or clears one [Issue] bit. */
export function setIssue(issue: Bits, on: boolean): void {
  const { issues } = usePlayerStore.getState();
  usePlayerStore.setState({ issues: on ? issues | issue : issues & ~issue });
}

/** Puts the list of tracks in the store, unless it is the list that is there, so that nothing redraws for nothing. */
export function setTracks(tracks: TrackInfo[]): void {
  const kept = usePlayerStore.getState().tracks;
  const same =
    kept.length === tracks.length &&
    kept.every((t, i) => t.name === tracks[i]?.name && t.seconds === tracks[i]?.seconds);
  if (!same && tracks.length > 0) {
    setPlayer({ tracks });
  }
}

/** Puts the whole store back to its initial state, the server's data included. */
export function clearPlayer(): void {
  usePlayerStore.setState(initial, true);
}

/**
 * Puts the page back to its first state, except for what belongs to the server and not to the listener: the list of
 * tracks, whether there is a presenter and what it said last. Those stay, and so does what `keep` sets.
 */
export function resetPlayer(keep: Partial<PlayerState> = {}): void {
  const { tracks, presenter, said } = usePlayerStore.getState();
  // The voice is not on any more once the listener has gone.
  usePlayerStore.setState({ ...initial, tracks, presenter: presenter & ~PlaylistFlag.Speaking, said, ...keep });
}
