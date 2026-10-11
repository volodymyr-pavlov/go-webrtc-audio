import { useEffect, useRef, useState, type RefObject } from 'react';

import { fetchPlaylist } from '@/api/client.ts';
import { Player } from '@/lib/player.ts';
import { openSession } from '@/lib/session.ts';

/**
 * Builds the player for the life of the page. It loads the list of tracks, and stops the player when the page goes.
 * The caller puts the returned ref on the audio element.
 */
export function usePlayer(): { player: Player; audio: RefObject<HTMLAudioElement | null> } {
  const audio = useRef<HTMLAudioElement>(null);
  const [player] = useState(() => new Player({ open: openSession, fetchPlaylist, audio: () => audio.current }));
  useEffect(() => {
    player.load();
  }, [player]);
  useEffect(() => {
    // Closing the tab does not run the cleanup of an effect, but it does fire pagehide, and stop() sends the request that
    // tells the server the listener has gone.
    const leave = () => player.stop();
    window.addEventListener('pagehide', leave);
    return () => {
      window.removeEventListener('pagehide', leave);
      player.stop();
    };
  }, [player]);
  return { player, audio };
}
