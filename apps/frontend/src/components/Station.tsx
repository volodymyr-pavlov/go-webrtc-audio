import { Equalizer } from '@/components/Equalizer.tsx';
import { Stage, stageOf } from '@/lib/playerState.ts';
import { pick } from '@/lib/bitflags.ts';
import { onAirLabel } from '@/lib/phaseText.ts';
import { usePlayerStore } from '@/store/player.ts';

const badge: Record<Stage, string> = {
  [Stage.Stopped]: 'bg-slate-200 text-slate-700',
  [Stage.Connecting]: 'bg-slate-200 text-slate-700',
  [Stage.Playing]: 'bg-rose-600 text-white',
  [Stage.Trouble]: 'bg-amber-200 text-amber-900',
};

const dot: Record<Stage, string> = {
  [Stage.Stopped]: 'bg-slate-500',
  [Stage.Connecting]: 'bg-slate-500 motion-safe:animate-pulse',
  [Stage.Playing]: 'bg-white motion-safe:animate-pulse',
  [Stage.Trouble]: 'bg-amber-700 motion-safe:animate-pulse',
};

/** The station: its artwork with the equalizer, its name, and the on-air badge. */
export function Station() {
  const phase = usePlayerStore((s) => s.phase);
  const stage = stageOf(phase);
  return (
    <div className="flex w-full items-center gap-4 text-left">
      <div className="relative grid h-24 w-24 shrink-0 place-items-center overflow-hidden rounded-2xl bg-brand shadow-lg shadow-violet-500/25">
        <Equalizer phase={phase} />
      </div>
      <div className="min-w-0">
        <h2 className="text-lg leading-tight font-semibold tracking-tight text-slate-900">Live audio stream</h2>
        <p className="mt-0.5 text-sm text-slate-600">Everyone hears the same moment.</p>
        <span
          className={`mt-2 inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-semibold tracking-wider ${pick(badge, stage)}`}
        >
          <span aria-hidden className={`h-1.5 w-1.5 rounded-full ${pick(dot, stage)}`} />
          {onAirLabel(phase)}
        </span>
      </div>
    </div>
  );
}
