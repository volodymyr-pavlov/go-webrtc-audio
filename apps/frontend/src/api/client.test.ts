import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { deleteListener, fetchIceServers, fetchPlaylist, postOffer } from '@/api/client.ts';

function respond(status: number, body: string, headers: Record<string, string> = {}) {
  return vi.fn(async () => new Response(body, { status, headers }));
}

beforeEach(() => {
  vi.stubGlobal('document', { baseURI: 'http://stand.test/' });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('postOffer', () => {
  it('posts the offer as SDP and resolves the relative Location against the address it posted to', async () => {
    const fetch = respond(201, 'v=0 answer', { Location: 'listen/42' });
    vi.stubGlobal('fetch', fetch);
    const answer = await postOffer('v=0 offer');
    expect(answer).toEqual({ sdp: 'v=0 answer', location: 'http://stand.test/api/listen/42' });
    expect(fetch).toHaveBeenCalledWith(
      '/api/listen',
      expect.objectContaining({ method: 'POST', body: 'v=0 offer', headers: { 'Content-Type': 'application/sdp' } }),
    );
  });

  it('shows the words of a server that refuses with a JSON error', async () => {
    vi.stubGlobal(
      'fetch',
      respond(503, '{"error":{"status":503,"code":"FULL","message":"The server has too many listeners."}}', {
        'Content-Type': 'application/json',
      }),
    );
    await expect(postOffer('x')).rejects.toThrow('The server has too many listeners.');
  });

  it('shows only the status when the refusal is a page of HTML, as a proxy sends', async () => {
    vi.stubGlobal('fetch', respond(502, '<html><body>502 Bad Gateway</body></html>', { 'Content-Type': 'text/html' }));
    await expect(postOffer('x')).rejects.toThrow('HTTP 502');
  });

  it('shows only the status when the message is long or the JSON is broken', async () => {
    const long = JSON.stringify({ error: { message: 'x'.repeat(500) } });
    vi.stubGlobal('fetch', respond(500, long, { 'Content-Type': 'application/json' }));
    await expect(postOffer('x')).rejects.toThrow('HTTP 500');
    vi.stubGlobal('fetch', respond(500, '{broken', { 'Content-Type': 'application/json' }));
    await expect(postOffer('x')).rejects.toThrow('HTTP 500');
  });

  it('refuses an answer that does not say where the listener is', async () => {
    vi.stubGlobal('fetch', respond(201, 'v=0 answer'));
    await expect(postOffer('x')).rejects.toThrow('did not say where');
  });

  it('drops the call when the signal aborts, so a listener who stopped gets no place', async () => {
    const fetch = vi.fn(
      (_address: string, init: { signal: AbortSignal }) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal.addEventListener('abort', () => reject(init.signal.reason as Error));
        }),
    );
    vi.stubGlobal('fetch', fetch);
    const controller = new AbortController();
    const call = postOffer('x', controller.signal);
    controller.abort(new DOMException('stopped', 'AbortError'));
    await expect(call).rejects.toThrow('stopped');
  });
});

describe('deleteListener', () => {
  it('sends a DELETE that survives the page closing', async () => {
    const fetch = respond(204, '');
    vi.stubGlobal('fetch', fetch);
    await deleteListener('http://stand.test/api/listen/42');
    expect(fetch).toHaveBeenCalledWith(
      'http://stand.test/api/listen/42',
      expect.objectContaining({ method: 'DELETE', keepalive: true }),
    );
  });

  it('ignores a failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    await expect(deleteListener('http://stand.test/api/listen/42')).resolves.toBeUndefined();
  });
});

describe('fetchPlaylist', () => {
  it('returns the track that plays now and the list of all the tracks', async () => {
    vi.stubGlobal(
      'fetch',
      respond(
        200,
        '{"data":{"track":"01-third.wav","flags":3,"said":"Next, fifth.","tracks":[{"name":"01-third.wav","seconds":30},{"name":"02-fifth.wav","seconds":25}]}}',
      ),
    );
    await expect(fetchPlaylist()).resolves.toEqual({
      track: '01-third.wav',
      flags: 3,
      said: 'Next, fifth.',
      tracks: [
        { name: '01-third.wav', seconds: 30 },
        { name: '02-fifth.wav', seconds: 25 },
      ],
    });
  });

  it('copes with a server that sends no list', async () => {
    vi.stubGlobal('fetch', respond(200, '{"data":{"track":"01-third.wav"}}'));
    await expect(fetchPlaylist()).resolves.toEqual({
      track: '01-third.wav',
      flags: 0,
      said: '',
      tracks: [],
    });
  });

  it('throws on a status that is not a success', async () => {
    vi.stubGlobal('fetch', respond(404, 'no'));
    await expect(fetchPlaylist()).rejects.toThrow('HTTP 404');
  });
});

describe('fetchIceServers', () => {
  it('returns the servers the backend hands out', async () => {
    const servers = [{ urls: ['turn:turn.example.com:3478'], username: '1800000000:ab', credential: 'c3VwZXI=' }];
    vi.stubGlobal('fetch', respond(200, JSON.stringify({ data: { iceServers: servers } })));
    await expect(fetchIceServers()).resolves.toEqual(servers);
  });

  it('asks for new credentials every time and keeps none in the cache', async () => {
    const fetchMock = respond(200, '{"data":{"iceServers":[]}}');
    vi.stubGlobal('fetch', fetchMock);
    await fetchIceServers();
    expect(fetchMock).toHaveBeenCalledWith('/api/ice', expect.objectContaining({ cache: 'no-store' }));
  });

  it('returns none when the backend has no relay, answers with an error, or sends nonsense', async () => {
    for (const reply of [
      respond(200, '{"data":{"iceServers":[]}}'),
      respond(500, 'no'),
      respond(200, '{}'),
      respond(200, 'x'),
    ]) {
      vi.stubGlobal('fetch', reply);
      await expect(fetchIceServers()).resolves.toEqual([]);
    }
  });

  it('returns none, and does not throw, when the call fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')));
    await expect(fetchIceServers()).resolves.toEqual([]);
  });
});
