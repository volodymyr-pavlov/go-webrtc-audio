import { Volume2 } from 'lucide-react';

import { Issue, Phase, Trouble } from '@/lib/playerState.ts';
import { has } from '@/lib/bitflags.ts';
import { noPlaylistText, statusText } from '@/lib/phaseText.ts';
import { usePlayerStore } from '@/store/player.ts';

type Props = {
  /** Called when the listener taps to turn on a sound that the browser refused. */
  onResume: () => void;
};

function tone(phase: Phase, blocked: boolean): string {
  if (phase === Phase.Error) {
    return 'text-rose-700';
  }
  if (has(phase, Trouble)) {
    return 'text-amber-700 motion-safe:animate-pulse';
  }
  return blocked ? 'text-amber-700' : 'text-slate-600';
}

/**
 * The line that says what is happening, and the track that plays now. While the stream plays the line is not shown: the
 * LIVE badge, the "Now playing" label and the track say the same. It stays in the page for screen readers, which need to
 * hear that the state changed.
 */
export function Status({ onResume }: Props) {
  const phase = usePlayerStore((s) => s.phase);
  const error = usePlayerStore((s) => s.error);
  const track = usePlayerStore((s) => s.track);
  const issues = usePlayerStore((s) => s.issues);
  const blocked = phase === Phase.Playing && has(issues, Issue.Blocked);
  return (
    <div className="mt-5 flex min-h-20 flex-col items-center justify-center gap-1.5">
      <p
        role="status"
        aria-live={phase === Phase.Error ? 'assertive' : 'polite'}
        className={`text-sm font-medium ${phase === Phase.Playing && !blocked ? 'sr-only' : tone(phase, blocked)}`}
      >
        {statusText(phase, error, issues)}
      </p>
      {blocked && (
        <button
          type="button"
          onClick={onResume}
          className="cursor-pointer rounded-lg bg-amber-100 px-3 py-1.5 text-sm font-medium text-amber-900 hover:bg-amber-200"
        >
          Tap to turn the sound on
        </button>
      )}
      {has(issues, Issue.NoPlaylist) && <p className="text-xs text-slate-600">{noPlaylistText}</p>}
      {track !== '' && (
        <div className="mt-1 flex flex-col items-center gap-1">
          <span className="text-[11px] font-semibold tracking-widest text-slate-600 uppercase">Now playing</span>
          <div className="flex items-center gap-2 rounded-md border border-sky-200 bg-sky-50 px-3 py-1 font-mono text-sm tracking-wide text-sky-800">
            <Volume2 aria-hidden className="h-4 w-4" />
            <span>♪ {track}</span>
          </div>
        </div>
      )}
    </div>
  );
}
