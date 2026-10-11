import { useId } from 'react';

import { Listening, Phase } from '@/lib/playerState.ts';
import { has } from '@/lib/bitflags.ts';
import { usePlayerStore } from '@/store/player.ts';

/** Three small bars that beat while their track is on the air. */
function Beat() {
  const bars = ['[animation-delay:0s]', '[animation-delay:-0.4s]', '[animation-delay:-0.8s]'];
  return (
    <span aria-hidden className="flex h-4 items-center gap-[2px]">
      {bars.map((delay) => (
        <span
          key={delay}
          className={`h-4 w-[3px] origin-center rounded-full bg-rose-600 motion-safe:equalizer-bar motion-reduce:scale-y-[0.6] ${delay}`}
        />
      ))}
    </span>
  );
}

/** Seconds as minutes and seconds, 0:30. */
function length(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}

/** All the tracks, as a list of stations is shown: a tile with a number, the name and the length, and the track on the air marked. */
export function TrackList() {
  const heading = useId();
  const tracks = usePlayerStore((s) => s.tracks);
  const track = usePlayerStore((s) => s.track);
  const phase = usePlayerStore((s) => s.phase);
  if (tracks.length === 0) {
    return null;
  }
  const listening = has(phase, Listening);
  return (
    <section aria-labelledby={heading} className="card mt-4 w-full p-3">
      <h2 id={heading} className="px-2 pt-1 pb-2 text-[11px] font-semibold tracking-widest text-slate-600 uppercase">
        Tracks · {tracks.length}
      </h2>
      <ul aria-labelledby={heading}>
        {tracks.map((t, i) => {
          const onAir = listening && t.name === track;
          return (
            <li
              key={t.name}
              aria-current={onAir ? 'true' : undefined}
              className={`flex items-center gap-3 rounded-xl px-2 py-2 transition-colors ${onAir ? 'bg-sky-100/80' : ''}`}
            >
              <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-brand font-mono text-sm font-semibold text-white shadow-sm">
                {String(i + 1).padStart(2, '0')}
              </span>
              <span className="min-w-0 flex-1 truncate font-mono text-sm text-slate-800">{t.name}</span>
              {onAir && phase === Phase.Playing && <Beat />}
              <span className="w-10 text-right font-mono text-xs text-slate-600">{length(t.seconds)}</span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
