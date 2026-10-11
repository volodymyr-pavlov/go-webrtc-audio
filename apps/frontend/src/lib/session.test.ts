import { describe, expect, it, vi } from 'vitest';

import { startPlayback } from '@/lib/session.ts';

describe('startPlayback', () => {
  it('reports that the sound plays', async () => {
    const onBlocked = vi.fn();
    await startPlayback({ play: async () => {} }, onBlocked);
    expect(onBlocked).toHaveBeenCalledExactlyOnceWith(false);
  });

  it('reports a refusal and does not swallow it', async () => {
    const onBlocked = vi.fn();
    const refuse = async () => {
      throw new DOMException('not allowed', 'NotAllowedError');
    };
    await startPlayback({ play: refuse }, onBlocked);
    expect(onBlocked).toHaveBeenCalledExactlyOnceWith(true);
  });

  it('does not take a play that a newer load interrupted for a refusal', async () => {
    const onBlocked = vi.fn();
    const interrupt = async () => {
      throw new DOMException('interrupted', 'AbortError');
    };
    await startPlayback({ play: interrupt }, onBlocked);
    expect(onBlocked).not.toHaveBeenCalled();
  });
});
