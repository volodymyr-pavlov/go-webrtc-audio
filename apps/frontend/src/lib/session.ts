import { deleteListener, fetchIceServers, postOffer } from '@/api/client.ts';
import { connectionOf, type Connection } from '@/lib/playerState.ts';

export type Stats = { packets: number; lost: number };

/** One WebRTC connection to the server. */
export type Session = {
  /** Ends the connection and tells the server. */
  close(): Promise<void>;
  /** Reads what the browser has received so far. */
  stats(): Promise<Stats>;
  /** Tries to start the sound again. Call it from a click or a tap, which a browser that refused the sound wants. */
  resume(): Promise<void>;
};

/**
 * Connects to the server. The state callback is attached before the offer is made, so no change is missed. The blocked
 * callback gets true when the browser refuses to play the sound and false when it plays. When the signal aborts, the
 * connection that is being made is dropped, and the promise rejects.
 */
export type OpenSession = (
  audio: HTMLAudioElement,
  onState: (state: Connection) => void,
  signal: AbortSignal,
  onBlocked: (blocked: boolean) => void,
) => Promise<Session>;

// The server takes no trickled candidates, so the offer waits for all of them, but not for ever.
const gatherLimitMs = 5000;

// Servers fixed when the page is built, for example a STUN server, [{"urls": "stun:stun.l.google.com:19302"}], for a
// listener behind a NAT. The server's own TURN credentials, which change for every connection, are added to them.
function configuredIceServers(): RTCIceServer[] {
  const configured = import.meta.env.VITE_ICE_SERVERS;
  return configured ? (JSON.parse(configured) as RTCIceServer[]) : [];
}

// gathered resolves once every candidate is in, when the limit passes, or when the signal aborts. The caller checks
// the signal right after, so a stopped attempt does not wait the limit out.
function gathered(pc: RTCPeerConnection, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (pc.iceGatheringState === 'complete' || signal.aborted) {
      resolve();
      return;
    }
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener('abort', done);
      resolve();
    };
    const timer = setTimeout(done, gatherLimitMs);
    signal.addEventListener('abort', done);
    pc.addEventListener('icegatheringstatechange', () => {
      if (pc.iceGatheringState === 'complete') {
        done();
      }
    });
  });
}

/**
 * Starts the sound of the element and reports whether the browser allowed it. A play that a newer load of the element
 * interrupted is not a refusal.
 */
export async function startPlayback(audio: Pick<HTMLAudioElement, 'play'>, onBlocked: (blocked: boolean) => void) {
  try {
    await audio.play();
    onBlocked(false);
  } catch (error) {
    if (!(error instanceof DOMException && error.name === 'AbortError')) {
      onBlocked(true);
    }
  }
}

export const openSession: OpenSession = async (audio, onState, signal, onBlocked) => {
  const relays = await fetchIceServers(signal);
  signal.throwIfAborted();
  const pc = new RTCPeerConnection({ iceServers: [...configuredIceServers(), ...relays] });
  let location = '';
  let stream: MediaStream | null = null;
  pc.onconnectionstatechange = () => onState(connectionOf(pc.connectionState));
  pc.ontrack = (event) => {
    if (signal.aborted) {
      return;
    }
    stream = event.streams[0] ?? new MediaStream([event.track]);
    audio.srcObject = stream;
    // The click that started the session allows sound in most browsers. A browser that refuses leaves the element
    // paused, and the listener is asked to tap.
    void startPlayback(audio, onBlocked);
  };

  try {
    pc.addTransceiver('audio', { direction: 'recvonly' });
    await pc.setLocalDescription(await pc.createOffer());
    await gathered(pc, signal);
    signal.throwIfAborted();
    const answer = await postOffer(pc.localDescription?.sdp ?? '', signal);
    location = answer.location;
    signal.throwIfAborted();
    await pc.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
    signal.throwIfAborted();
  } catch (error) {
    pc.onconnectionstatechange = null;
    pc.ontrack = null;
    pc.close();
    if (location !== '') {
      await deleteListener(location);
    }
    throw error;
  }

  return {
    async close() {
      pc.onconnectionstatechange = null;
      pc.ontrack = null;
      pc.close();
      // A newer session may have taken the element over by now.
      if (stream !== null && audio.srcObject === stream) {
        audio.srcObject = null;
      }
      await deleteListener(location);
    },
    resume: () => startPlayback(audio, onBlocked),
    async stats() {
      let packets = 0;
      let lost = 0;
      (await pc.getStats()).forEach((report) => {
        if (report.type === 'inbound-rtp' && report.kind === 'audio') {
          packets = Number(report.packetsReceived ?? 0);
          lost = Number(report.packetsLost ?? 0);
        }
      });
      return { packets, lost };
    },
  };
};
