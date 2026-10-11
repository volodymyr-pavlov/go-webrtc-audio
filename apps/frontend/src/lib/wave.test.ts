import { describe, expect, it } from 'vitest';

import { createWave, lowest, type Random } from '@/lib/wave.ts';

/** A small seeded generator, so that a test gets the same numbers every time. */
function seeded(seed: number): Random {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Runs a wave for some seconds at 60 frames a second, and returns the heights of 17 bars after each second. */
function run(random: Random, seconds: number): number[][] {
  const wave = createWave(random);
  const frames: number[][] = [];
  for (let second = 0; second < seconds; second++) {
    for (let frame = 0; frame < 60; frame++) {
      wave.step(1 / 60);
    }
    frames.push(wave.heights(17));
  }
  return frames;
}

const argmax = (heights: number[]) => heights.indexOf(Math.max(...heights));

describe('createWave', () => {
  it('keeps every bar between the lowest height and the full height', () => {
    for (const frame of run(seeded(1), 30)) {
      expect(frame).toHaveLength(17);
      for (const h of frame) {
        expect(h).toBeGreaterThanOrEqual(lowest - 1e-9);
        expect(h).toBeLessThanOrEqual(1 + 1e-9);
      }
    }
  });

  it('is a wave: neighbouring bars differ by little, and the bars are not all the same height', () => {
    for (const frame of run(seeded(2), 20)) {
      const spread = Math.max(...frame) - Math.min(...frame);
      expect(spread).toBeGreaterThan(0.1);
      for (let i = 1; i < frame.length; i++) {
        expect(Math.abs(frame[i]! - frame[i - 1]!)).toBeLessThan(0.45);
      }
    }
  });

  it('travels: the highest bar does not stay where it is', () => {
    const peaks = new Set(run(seeded(3), 20).map(argmax));
    expect(peaks.size).toBeGreaterThan(4);
  });

  it('is random: other seeds give other waves, the same seed gives the same wave', () => {
    expect(run(seeded(4), 5)).toEqual(run(seeded(4), 5));
    expect(run(seeded(4), 5)).not.toEqual(run(seeded(5), 5));
  });

  it('changes its own character over time and not only its place', () => {
    // The spread of the heights and the distance between two crests would be the same for ever in a plain sine.
    const spreads = run(seeded(6), 30).map((f) => Math.max(...f) - Math.min(...f));
    expect(Math.max(...spreads) - Math.min(...spreads)).toBeGreaterThan(0.1);
  });

  it('does not move when no time passes, and does not jump when much time passes at once', () => {
    const wave = createWave(seeded(7));
    const before = wave.heights(17);
    wave.step(0);
    wave.step(-1);
    expect(wave.heights(17)).toEqual(before);
    wave.step(30);
    for (const h of wave.heights(17)) {
      expect(h).toBeGreaterThanOrEqual(lowest - 1e-9);
      expect(h).toBeLessThanOrEqual(1 + 1e-9);
    }
  });
});
