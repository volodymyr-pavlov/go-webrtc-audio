const longestDelayMs = 10_000;

/** How long to wait before each attempt to get a dropped connection back, in milliseconds. The last value repeats. */
const retryDelays = [1000, 2000, 4000, 8000, longestDelayMs];

/** How long a connection may stay disconnected before it is replaced. Short blips heal by themselves. */
export const graceMs = 3000;

export function retryDelay(attempt: number): number {
  return retryDelays[Math.min(attempt, retryDelays.length - 1)] ?? longestDelayMs;
}
