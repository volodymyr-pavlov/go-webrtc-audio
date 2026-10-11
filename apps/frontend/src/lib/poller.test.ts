import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Poller, staleAfter } from '@/lib/poller.ts';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

function newPoller(run: () => Promise<void> | null, immediate = false) {
  const onStale = vi.fn<(stale: boolean) => void>();
  const poller = new Poller({ everyMs: 1000, immediate, run, onStale });
  return { poller, onStale };
}

describe('Poller', () => {
  it('polls at the interval, and at once if asked', async () => {
    const run = vi.fn(async () => {});
    const { poller } = newPoller(run, true);
    poller.start();
    expect(run).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(2000);
    expect(run).toHaveBeenCalledTimes(3);
    poller.stop();
    await vi.advanceTimersByTimeAsync(5000);
    expect(run).toHaveBeenCalledTimes(3);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('reports stale after the failures in a row, and fresh at the next success', async () => {
    let fail = true;
    const { poller, onStale } = newPoller(async () => {
      if (fail) {
        throw new Error('down');
      }
    });
    poller.start();
    await vi.advanceTimersByTimeAsync(1000 * (staleAfter - 1));
    expect(onStale).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(onStale).toHaveBeenCalledExactlyOnceWith(true);
    await vi.advanceTimersByTimeAsync(3000);
    expect(onStale).toHaveBeenCalledTimes(1);

    fail = false;
    await vi.advanceTimersByTimeAsync(1000);
    expect(onStale).toHaveBeenLastCalledWith(false);
    poller.stop();
  });

  it('counts only failures in a row', async () => {
    let n = 0;
    const { poller, onStale } = newPoller(async () => {
      if (++n % staleAfter !== 0) {
        throw new Error('down');
      }
    });
    poller.start();
    await vi.advanceTimersByTimeAsync(1000 * staleAfter * 3);
    expect(onStale).not.toHaveBeenCalled();
    poller.stop();
  });

  it('counts a poll with nothing to do as neither', async () => {
    const { poller, onStale } = newPoller(() => null);
    poller.start();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(onStale).not.toHaveBeenCalled();
    poller.stop();
  });
});
