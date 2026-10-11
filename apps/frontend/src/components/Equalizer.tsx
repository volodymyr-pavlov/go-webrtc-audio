import { useEffect, useRef, type CSSProperties } from 'react';

import { Phase, Stage, stageOf } from '@/lib/playerState.ts';
import { pick } from '@/lib/bitflags.ts';
import { createWave } from '@/lib/wave.ts';

const count = 17;

// A bar that stands still is at the height the wave would give it at one moment, low while nothing plays, so the row is
// a wave at rest: a slow sine over the row, between these two heights.
const restLow = 0.18;
const restHigh = 0.5;
const restWavelength = 11.5;
const restPeakBar = 6;

/** Returns the height of bar `i` in a row at rest, as a fraction of the full height. */
function restingHeight(i: number): number {
  const cycle = Math.cos((2 * Math.PI * (i - restPeakBar)) / restWavelength);
  return restLow + (restHigh - restLow) * (0.5 + 0.5 * cycle);
}

const resting = Array.from({ length: count }, (_, i) => restingHeight(i));

// While the signal is lost or coming back the bars lie flat and turn amber, as the status line does.
const look: Record<Stage, string> = {
  [Stage.Stopped]: 'bg-white/70',
  [Stage.Connecting]: 'bg-white/85',
  [Stage.Playing]: 'bg-white motion-reduce:scale-y-[0.6]',
  [Stage.Trouble]: 'bg-amber-200',
};

// The longest step of the wave in one frame. A tab that was in the background returns after minutes, and the wave must
// not leap.
const longestStep = 0.1;

/**
 * A row of bars whose heights make a wave that travels along them while the stream plays. The wave is random: its
 * length, speed, direction and strength keep changing (see `lib/wave.ts`). The bars are moved from here, one frame at a
 * time, because the heights are no longer a pattern that the style sheet could repeat. For people who have asked their
 * system to reduce motion they stand still. It is decoration: the page says the same in words.
 */
export function Equalizer({ phase }: { phase: Phase }) {
  const bars = useRef<(HTMLSpanElement | null)[]>([]);
  const playing = phase === Phase.Playing;

  useEffect(() => {
    if (!playing || matchMedia('(prefers-reduced-motion: reduce)').matches) {
      return;
    }
    const wave = createWave();
    let last = performance.now();
    let frame = requestAnimationFrame(function tick(now) {
      wave.step(Math.min((now - last) / 1000, longestStep));
      last = now;
      const heights = wave.heights(count);
      bars.current.forEach((bar, i) => {
        if (bar !== null) {
          bar.style.transform = `scaleY(${heights[i]})`;
        }
      });
      frame = requestAnimationFrame(tick);
    });
    const row = bars.current;
    return () => {
      cancelAnimationFrame(frame);
      // Give the bars back to the style sheet, which holds the heights of the other states.
      row.forEach((bar) => {
        if (bar !== null) {
          bar.style.transform = '';
        }
      });
    };
  }, [playing]);

  const stage = stageOf(phase);
  const flat = stage === Stage.Trouble;
  return (
    <div aria-hidden className="flex h-16 items-center gap-[2px]">
      {resting.map((height, i) => (
        <span
          key={i}
          ref={(el) => {
            bars.current[i] = el;
          }}
          style={{ '--rest': flat ? restLow : height } as CSSProperties}
          className={`h-16 w-[3px] origin-center rounded-full transition-colors will-change-transform ${pick(look, stage)} ${
            playing ? '' : 'scale-y-(--rest)'
          }`}
        />
      ))}
    </div>
  );
}
