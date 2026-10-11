import { Phase } from '@/lib/playerState.ts';
import { usePlayerStore } from '@/store/player.ts';

/** A pulsing line along the top of the card while the stream plays. */
export function PlayingBar() {
  const playing = usePlayerStore((s) => s.phase === Phase.Playing);
  if (!playing) {
    return null;
  }
  return (
    <div className="absolute top-0 right-0 left-0 h-1 bg-gradient-to-r from-sky-500 via-indigo-500 to-sky-500 motion-safe:animate-pulse" />
  );
}
