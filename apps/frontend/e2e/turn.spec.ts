import { expect, test } from '@playwright/test';

// Runs against the stand with a TURN server (make stand-up-turn), whose page is on STAND_URL, for example
// http://localhost:8080. Without it the test is skipped, because the other tests start their own backend and none has
// a TURN server.
const stand = process.env.STAND_URL ?? '';

test.skip(stand === '', 'set STAND_URL to the page of a stand started with make stand-up-turn');

test('a listener that may only use a relay still hears the stream, through the TURN server', async ({ page }) => {
  // The page does not ask for a relay-only connection, because a listener with a working direct path should use it.
  // The test makes the browser refuse every other path, as a network that blocks UDP to the backend would.
  await page.addInitScript(() => {
    const Original = window.RTCPeerConnection;
    const made: RTCPeerConnection[] = [];
    (window as unknown as { __pcs: RTCPeerConnection[] }).__pcs = made;
    window.RTCPeerConnection = class extends Original {
      constructor(configuration?: RTCConfiguration) {
        super({ ...configuration, iceTransportPolicy: 'relay' });
        made.push(this);
      }
    };
  });

  const ice = await page.request.get(`${stand}/api/ice`);
  expect(ice.ok()).toBe(true);
  const servers = (
    (await ice.json()) as { data: { iceServers: { urls: string[]; username: string; credential: string }[] } }
  ).data.iceServers;
  expect(servers).toHaveLength(1);
  expect(servers[0]!.urls[0]).toMatch(/^turn:/);
  expect(servers[0]!.username).toMatch(/^\d+:[0-9a-f]+$/);

  await page.goto(stand);
  await page.getByRole('button', { name: /Listen/ }).click();
  await expect(page.getByRole('status')).toHaveText('Playing', { timeout: 30_000 });

  // The audio really came through the relay: the pair that carries it has a local candidate with a relay protocol, the
  // protocol the browser speaks to the TURN server. Chromium may call such a candidate prflx instead of relay, when the
  // server's answer to a check names the relay's address a little differently from the candidate, so the type is not
  // what is checked.
  const relayProtocols = await page.evaluate(async () => {
    const pc = (window as unknown as { __pcs: RTCPeerConnection[] }).__pcs[0]!;
    const stats = await pc.getStats();
    const found: string[] = [];
    stats.forEach((report) => {
      if (report.type === 'candidate-pair' && report.nominated && report.state === 'succeeded') {
        const local = stats.get(report.localCandidateId) as { relayProtocol?: string } | undefined;
        found.push(local?.relayProtocol ?? '');
      }
    });
    return found;
  });
  expect(relayProtocols.some((protocol) => protocol !== '')).toBe(true);

  await expect
    .poll(
      async () => {
        const packets = await page
          .locator('dt', { hasText: 'Packets' })
          .locator('xpath=following-sibling::dd[1]')
          .innerText();
        return Number(packets.replaceAll(',', ''));
      },
      { timeout: 15_000 },
    )
    .toBeGreaterThan(20);
});
