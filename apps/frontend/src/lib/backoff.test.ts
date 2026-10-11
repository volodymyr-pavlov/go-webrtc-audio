import { describe, expect, it } from 'vitest';

import { retryDelay } from '@/lib/backoff.ts';

describe('retryDelay', () => {
  it('doubles from one second to eight, then stays at ten', () => {
    expect([0, 1, 2, 3, 4, 5, 20].map(retryDelay)).toEqual([1000, 2000, 4000, 8000, 10000, 10000, 10000]);
  });
});
