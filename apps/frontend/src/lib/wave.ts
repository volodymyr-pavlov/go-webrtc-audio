/** Returns a number from 0 up to, not including, 1. `Math.random` is one; a seeded one makes a test repeatable. */
export type Random = () => number;

/** A wave that travels along a row of bars, and changes at random as it goes. */
export type Wave = {
  /** Moves the wave on by this many seconds. */
  step(seconds: number): void;
  /** Returns the height of each of `count` bars, from `lowest` to 1. */
  heights(count: number): number[];
};

/** The height of the lowest bar, as a fraction of the highest. */
export const lowest = 0.12;

const components = 3;

// The sum of the waves swings between minus and plus its total, which maps to the range from 0 to 1 around this middle.
const middle = 0.5;

// The wavelength of a component, in bars: from the shortest to the shortest plus the spread.
const shortestLength = 5;
const lengthSpread = 16;
// A component takes this long to cross one cycle, in seconds: from the quickest to the quickest plus the spread.
const quickestCycle = 0.8;
const cycleSpread = 1.6;
// The strength of a component, as a fraction of the full height.
const weakest = 0.3;
const strengthSpread = 0.7;
// The wave draws new targets after this many seconds, at first from the shortest wait plus the spread, and then again.
const firstWait = 1;
const firstWaitSpread = 2;
const nextWait = 1;
const nextWaitSpread = 2.5;
// The share of the components that draw a new target at each change.
const changeShare = 0.6;

// How fast the wave moves towards a new target, as a fraction of the way per second: it takes about a second.
const eagerness = 1.5;

type Component = {
  /** How many bars a wavelength spans. */
  length: number;
  /** Cycles a second, and which way: negative runs the other way. */
  speed: number;
  /** How much of the wave this component makes. */
  amplitude: number;
  /** Where in its cycle the component is. */
  phase: number;
  target: { length: number; speed: number; amplitude: number };
};

/**
 * Makes a wave of three overlapping waves. Every second or two some of them draw a new wavelength, speed, direction and
 * strength at random, and move towards them smoothly. A speed passes through zero on its way from one direction to the
 * other, so a wave can slow down, stop and turn round. The phase is added up from the speed rather than set from the
 * time, so a change of speed never makes the wave jump.
 */
export function createWave(random: Random = Math.random): Wave {
  const draw = () => ({
    length: shortestLength + random() * lengthSpread,
    speed: (random() < 0.5 ? -1 : 1) / (quickestCycle + random() * cycleSpread),
    amplitude: weakest + random() * strengthSpread,
  });
  const parts: Component[] = Array.from({ length: components }, () => {
    const target = draw();
    return { ...target, phase: random() * 2 * Math.PI, target: draw() };
  });
  let untilChange = firstWait + random() * firstWaitSpread;

  return {
    step(seconds) {
      if (seconds <= 0) {
        return;
      }
      untilChange -= seconds;
      if (untilChange <= 0) {
        for (const part of parts) {
          if (random() < changeShare) {
            part.target = draw();
          }
        }
        untilChange = nextWait + random() * nextWaitSpread;
      }
      const ease = 1 - Math.exp(-eagerness * seconds);
      for (const part of parts) {
        part.length += (part.target.length - part.length) * ease;
        part.speed += (part.target.speed - part.speed) * ease;
        part.amplitude += (part.target.amplitude - part.amplitude) * ease;
        part.phase += 2 * Math.PI * part.speed * seconds;
      }
    },
    heights(count) {
      const total = parts.reduce((sum, part) => sum + part.amplitude, 0);
      return Array.from({ length: count }, (_, i) => {
        const sum = parts.reduce(
          (s, part) => s + part.amplitude * Math.sin((2 * Math.PI * i) / part.length + part.phase),
          0,
        );
        return lowest + (1 - lowest) * (middle + (middle * sum) / total);
      });
    },
  };
}
