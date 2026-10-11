import { Mic } from 'lucide-react';

import { PlaylistFlag } from '@/api/client.ts';
import { has } from '@/lib/bitflags.ts';
import { usePlayerStore } from '@/store/player.ts';

/** What the presenter says when it has not said anything yet. */
const waiting = 'Introduces each track a few seconds before it starts.';

/**
 * The place of the AI presenter: always on the page when the server has one, with the last words it said until it says
 * the next, and a mark while its voice is on. It says that the voice is a computer's, in a label that is always there.
 */
export function Presenter() {
  const bits = usePlayerStore((s) => s.presenter);
  const said = usePlayerStore((s) => s.said);
  if (!has(bits, PlaylistFlag.Presenter)) {
    return null;
  }
  const speaking = has(bits, PlaylistFlag.Speaking);
  return (
    <section
      aria-label="AI presenter"
      className={`mt-4 w-full rounded-xl border px-3.5 py-3 text-left transition-colors ${
        speaking ? 'border-violet-300 bg-violet-50' : 'border-slate-200 bg-white/60'
      }`}
    >
      <div className="flex items-center gap-2">
        <Mic aria-hidden className={`h-3.5 w-3.5 ${speaking ? 'text-violet-700' : 'text-slate-500'}`} />
        <span className="rounded-full bg-violet-100 px-2 py-0.5 text-[10px] font-semibold tracking-widest text-violet-800 uppercase">
          AI presenter
        </span>
        {speaking && (
          <span className="ml-auto inline-flex items-center gap-1.5 text-[10px] font-semibold tracking-widest text-rose-700 uppercase">
            <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-rose-600 motion-safe:animate-pulse" />
            Speaking
          </span>
        )}
      </div>
      <p aria-live="polite" className={`mt-1.5 min-h-10 text-sm ${said === '' ? 'text-slate-500' : 'text-slate-800'}`}>
        {said === '' ? waiting : said}
      </p>
    </section>
  );
}
