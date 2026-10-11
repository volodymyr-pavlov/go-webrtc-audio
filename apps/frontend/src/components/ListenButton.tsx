import { Play, RefreshCw, Square } from 'lucide-react';

import { Phase, Stage, Stopped, stageOf } from '@/lib/playerState.ts';
import { has, pick } from '@/lib/bitflags.ts';
import { buttonLabel } from '@/lib/phaseText.ts';
import { usePlayerStore } from '@/store/player.ts';

type Props = {
  onListen: () => void;
  onStop: () => void;
};

const base =
  'flex w-full cursor-pointer items-center justify-center gap-2 rounded-xl px-6 py-4 text-base font-medium shadow-lg transition-all duration-200';

const stopped = 'bg-sky-700 text-white shadow-sky-700/30 hover:bg-sky-800 active:bg-sky-900';
const live = 'border border-rose-300 bg-rose-50 text-rose-700 hover:bg-rose-100';

const look: Record<Stage, string> = {
  [Stage.Stopped]: stopped,
  [Stage.Connecting]: 'bg-slate-100 text-slate-700 hover:bg-slate-200',
  [Stage.Playing]: live,
  [Stage.Trouble]: live,
};

function Icon({ phase }: { phase: Phase }) {
  if (phase === Phase.Connecting) {
    return <RefreshCw aria-hidden className="h-5 w-5 text-slate-600 motion-safe:animate-spin" />;
  }
  if (has(phase, Stopped)) {
    return <Play aria-hidden className="h-5 w-5 fill-current" />;
  }
  return <Square aria-hidden className="h-5 w-5 fill-current" />;
}

/** Listen when stopped, Stop while listening. While connecting it is still pressable, and cancels. */
export function ListenButton({ onListen, onStop }: Props) {
  const phase = usePlayerStore((s) => s.phase);
  return (
    <button
      type="button"
      onClick={has(phase, Stopped) ? onListen : onStop}
      className={`${base} ${pick(look, stageOf(phase))}`}
    >
      <Icon phase={phase} />
      <span>{buttonLabel(phase)}</span>
      {phase === Phase.Connecting && <span className="sr-only">, press to cancel</span>}
    </button>
  );
}
