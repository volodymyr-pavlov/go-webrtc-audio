import { has, type Bits } from '@/lib/bitflags.ts';

/**
 * What the page shows. Exactly one bit is set. `Lost` is a connection that dropped and may still heal, `Reconnecting` is
 * one that was replaced and is being rebuilt, and `Error` is a first attempt that the server or the browser refused.
 */
export const Phase = {
  Idle: 1 << 0,
  Connecting: 1 << 1,
  Playing: 1 << 2,
  Lost: 1 << 3,
  Reconnecting: 1 << 4,
  Error: 1 << 5,
} as const;
export type Phase = (typeof Phase)[keyof typeof Phase];

/** The phases in which the button starts the stream. */
export const Stopped: Bits = Phase.Idle | Phase.Error;

/** The phases in which the signal is gone and may come back. */
export const Trouble: Bits = Phase.Lost | Phase.Reconnecting;

/** The phases in which the listener has a connection, or is getting one back. */
export const Listening: Bits = Phase.Playing | Trouble;

/** The phases that look alike on the page. Exactly one bit is set. */
export const Stage = {
  Stopped: 1 << 0,
  Connecting: 1 << 1,
  Playing: 1 << 2,
  Trouble: 1 << 3,
} as const;
export type Stage = (typeof Stage)[keyof typeof Stage];

/** Returns the stage a phase belongs to. */
export function stageOf(phase: Phase): Stage {
  if (has(phase, Stopped)) {
    return Stage.Stopped;
  }
  if (has(phase, Trouble)) {
    return Stage.Trouble;
  }
  return phase === Phase.Playing ? Stage.Playing : Stage.Connecting;
}

/** What is wrong while the page runs, apart from the phase. Any number of bits may be set. */
export const Issue = {
  /** The browser refused to play the sound, so the page says "Playing" over silence. */
  Blocked: 1 << 0,
  /** The last polls of the playlist failed, so the track name may be old. */
  NoPlaylist: 1 << 1,
  /** The last polls of the connection statistics failed, so the numbers may be old. */
  NoStats: 1 << 2,
} as const;

/** The state of the connection to the server. Exactly one bit is set. */
export const Connection = {
  Idle: 1 << 0,
  New: 1 << 1,
  Connecting: 1 << 2,
  Connected: 1 << 3,
  Disconnected: 1 << 4,
  Failed: 1 << 5,
  Closed: 1 << 6,
} as const;
export type Connection = (typeof Connection)[keyof typeof Connection];

/** Turns the state name the browser reports into its bit. */
export function connectionOf(state: RTCPeerConnectionState): Connection {
  switch (state) {
    case 'new':
      return Connection.New;
    case 'connecting':
      return Connection.Connecting;
    case 'connected':
      return Connection.Connected;
    case 'disconnected':
      return Connection.Disconnected;
    case 'failed':
      return Connection.Failed;
    case 'closed':
      return Connection.Closed;
  }
}
