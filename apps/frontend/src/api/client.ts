const base = (import.meta.env.VITE_API_BASE ?? '/api').replace(/\/$/, '');

// How long a call may take before the page gives up on it. A connection that hangs would otherwise leave the page on
// "Connecting…" for ever, or stop it from trying again.
const offerTimeoutMs = 15_000;
const leaveTimeoutMs = 5_000;
const trackTimeoutMs = 4_000;
const iceTimeoutMs = 3_000;

export type Answer = {
  /** The SDP answer. */
  sdp: string;
  /** The address of the listener on the server, which DELETE ends. */
  location: string;
};

/** The reason a call was refused: the message of the error in the JSON of the server if it is short, otherwise the status. */
async function refusal(response: Response): Promise<string> {
  const fallback = `HTTP ${response.status}`;
  if (!(response.headers.get('Content-Type')?.startsWith('application/json') ?? false)) {
    return fallback;
  }
  try {
    const { error } = (await response.json()) as { error?: { message?: unknown } };
    const message = error?.message;
    return typeof message === 'string' && message !== '' && message.length <= 200 ? message : fallback;
  } catch {
    return fallback;
  }
}

/**
 * Posts the browser's offer to the server and returns its answer. It throws the reason when the server refuses, and
 * drops the call when the signal aborts, so that a listener who stopped does not get a place on the server.
 */
export async function postOffer(offer: string, signal?: AbortSignal): Promise<Answer> {
  const address = `${base}/listen`;
  const timeout = AbortSignal.timeout(offerTimeoutMs);
  const response = await fetch(address, {
    method: 'POST',
    headers: { 'Content-Type': 'application/sdp' },
    body: offer,
    signal: signal === undefined ? timeout : AbortSignal.any([signal, timeout]),
  });
  if (!response.ok) {
    throw new Error(await refusal(response));
  }
  const location = response.headers.get('Location');
  if (location === null) {
    throw new Error('The server did not say where the listener is');
  }
  // The server answers with a path relative to the address that was posted to, so it is right behind a proxy that adds
  // or removes the /api prefix.
  return { sdp: await response.text(), location: new URL(location, new URL(address, document.baseURI)).href };
}

/** Tells the server that the listener has left. A failure is ignored: the server drops a silent listener by itself. */
export async function deleteListener(location: string): Promise<void> {
  try {
    await fetch(location, { method: 'DELETE', keepalive: true, signal: AbortSignal.timeout(leaveTimeoutMs) });
  } catch {
    // Nothing to do. The server removes the listener when its connection times out.
  }
}

/** A track of the playlist. */
export type TrackInfo = {
  /** The file name of the track. */
  name: string;
  /** How long it is, in whole seconds. */
  seconds: number;
};

/**
 * The bits of the `flags` of the server's playlist answer. The server sets `Presenter` when it has an AI presenter and
 * `Speaking` while the presenter's voice is on.
 */
export const PlaylistFlag = {
  Presenter: 1 << 0,
  Speaking: 1 << 1,
} as const;

export type Playlist = {
  /** The name of the track that plays now. */
  track: string;
  /** The [PlaylistFlag] bits: whether the server has an AI presenter, and whether its voice is on now. */
  flags: number;
  /** The last thing the presenter said, which stays until it says the next; empty if it has not spoken yet. */
  said: string;
  /** All the tracks, in the order they play. */
  tracks: TrackInfo[];
};

/** Returns the track that plays now and the list of all the tracks. */
export async function fetchPlaylist(): Promise<Playlist> {
  const response = await fetch(`${base}/playlist`, { cache: 'no-store', signal: AbortSignal.timeout(trackTimeoutMs) });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
  const { data: body = {} } = (await response.json()) as {
    data?: { track?: string; flags?: number; said?: string; tracks?: TrackInfo[] };
  };
  return {
    track: body.track ?? '',
    flags: body.flags ?? 0,
    said: body.said ?? '',
    tracks: body.tracks ?? [],
  };
}

/**
 * Returns the servers that the browser may use to reach the server through a relay: TURN credentials that the server
 * makes for this connection and that stop working after a while. It returns none when the server has no TURN server, and
 * also when the call fails or is slow, because a listener who can connect directly should not wait for a relay.
 */
export async function fetchIceServers(signal?: AbortSignal): Promise<RTCIceServer[]> {
  try {
    const timeout = AbortSignal.timeout(iceTimeoutMs);
    const response = await fetch(`${base}/ice`, {
      cache: 'no-store',
      signal: signal === undefined ? timeout : AbortSignal.any([signal, timeout]),
    });
    if (!response.ok) {
      return [];
    }
    const { data } = (await response.json()) as { data?: { iceServers?: RTCIceServer[] } };
    return data?.iceServers ?? [];
  } catch {
    return [];
  }
}
