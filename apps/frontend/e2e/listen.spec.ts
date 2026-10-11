import { spawn, type ChildProcess } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { expect, test, type Page } from '@playwright/test';

const root = fileURLToPath(new URL('../../..', import.meta.url));
const backend = 'http://127.0.0.1:18081';

let server: ChildProcess | null = null;

async function startBackend(): Promise<void> {
  server = spawn(
    `${root}.bin/server`,
    ['-http', '127.0.0.1:18081', '-loopback', '-audio', `${root}audio`, '-ice-port', '5015'],
    { stdio: 'ignore' },
  );
  await expect
    .poll(async () => (await fetch(`${backend}/api/playlist`).catch(() => null))?.status, { timeout: 15_000 })
    .toBe(200);
}

async function stopBackend(): Promise<void> {
  const running = server;
  server = null;
  if (running === null) {
    return;
  }
  const exited = new Promise((resolve) => running.once('exit', resolve));
  running.kill();
  await exited;
}

async function listeners(): Promise<number> {
  const response = await fetch(`${backend}/api/metrics`);
  return ((await response.json()) as { listeners: number }).listeners;
}

test.beforeEach(startBackend);
test.afterEach(stopBackend);

const playerButton = (page: Page) => page.getByRole('button', { name: /^(Listen|Stop|Connecting…)/ });

/** Reads the number of packets the page shows. */
const readPackets = (page: Page) => async () =>
  Number((await page.locator('dd').nth(1).innerText()).replaceAll(/\D/g, ''));

async function startListening(page: Page): Promise<void> {
  await playerButton(page).click();
  await expect(page.getByRole('status')).toHaveText('Playing', { timeout: 15_000 });
}

test('before anyone listens the page is off air', async ({ page }) => {
  await page.goto('/');
  await expect(playerButton(page)).toHaveText('Listen');
  await expect(page.getByRole('status')).toHaveText('');
  await expect(page.getByText('OFF AIR')).toBeVisible();
});

test('the list of tracks is there before anyone listens, with nothing on the air', async ({ page }) => {
  await page.goto('/');
  const { data: playlist } = (await (await page.request.get('/api/playlist')).json()) as {
    data: { tracks: { name: string }[] };
  };
  expect(playlist.tracks.length).toBeGreaterThan(0);
  const tracks = page.getByRole('list', { name: /Tracks/ }).getByRole('listitem');
  await expect(tracks).toHaveCount(playlist.tracks.length);
  await expect(tracks.first()).toContainText(playlist.tracks[0]!.name);
  await expect(page.locator('li[aria-current="true"]')).toHaveCount(0);
});

test('pressing Listen connects, names the track and puts it on the air', async ({ page }) => {
  await page.goto('/');
  await playerButton(page).click();
  await expect(playerButton(page)).toContainText('Connecting…');
  await expect(page.getByText(/^♪ .+\.(aac|flac|m4a|mp3|ogg|opus|wav)$/)).toBeVisible({ timeout: 5000 });
  await expect(page.getByRole('status')).toHaveText('Playing', { timeout: 15_000 });
  await expect(playerButton(page)).toHaveText('Stop');
  await expect(page.getByText('LIVE', { exact: true })).toBeVisible();
  await expect(page.getByText('Now playing')).toBeVisible();
  // Exactly one track is on the air, and it is the one that the page names.
  await expect(page.locator('li[aria-current="true"]')).toHaveCount(1);
});

test('the packet count grows and the server counts one listener', async ({ page }) => {
  await page.goto('/');
  await startListening(page);
  await expect.poll(readPackets(page), { timeout: 10_000 }).toBeGreaterThan(20);
  expect(await listeners()).toBe(1);
});

test('the stream comes back after the server restarts', async ({ page }) => {
  await page.goto('/');
  await startListening(page);
  const status = page.getByRole('status');

  await stopBackend();
  await expect(status).toHaveText(/Connection lost|Reconnecting/, { timeout: 30_000 });
  await startBackend();
  await expect(status).toHaveText('Playing', { timeout: 60_000 });
  const packets = readPackets(page);
  const before = await packets();
  await expect.poll(packets, { timeout: 10_000 }).toBeGreaterThan(before);
});

test('pressing Stop leaves the page off air and the server without a listener', async ({ page }) => {
  await page.goto('/');
  await startListening(page);
  await playerButton(page).click();
  await expect(playerButton(page)).toHaveText('Listen');
  await expect(page.getByRole('status')).toHaveText('');
  await expect.poll(listeners, { timeout: 5000 }).toBe(0);
});

// The tests of the presenter answer /api/playlist themselves, so that they need no model: what matters here is what the page
// does with the answer.
// The bits of the flags of the playlist answer, as the server writes them.
const Present = 1;
const Speaking = 2;
type Presenter = { flags: number; said: string };

async function stubPresenter(page: Page, current: () => Presenter): Promise<void> {
  await page.route('**/api/playlist', async (route) => {
    const real = await route.fetch();
    const { data } = (await real.json()) as { data: Record<string, unknown> };
    await route.fulfill({ response: real, json: { data: { ...data, ...current() } } });
  });
}

test('the place of the AI presenter is always there, with its last words, when the server has a presenter', async ({
  page,
}) => {
  let state: Presenter = { flags: Present, said: '' };
  await stubPresenter(page, () => state);
  await page.goto('/');

  // Before anyone listens, and before the presenter has said anything: the place, its label and what it will do.
  const panel = page.getByRole('region', { name: 'AI presenter' });
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('Introduces each track a few seconds before it starts.');
  await expect(panel.getByText('Speaking')).toHaveCount(0);

  await page.getByRole('button', { name: 'Listen' }).click();
  await expect(page.getByRole('status')).toHaveText('Playing', { timeout: 20_000 });

  state = { flags: Present | Speaking, said: 'Up next, octave. A calm one for the evening.' };
  await expect(panel).toContainText('Up next, octave. A calm one for the evening.', { timeout: 6_000 });
  await expect(panel.getByText('Speaking')).toBeVisible();

  // The voice stops, and the words stay.
  state = { flags: Present, said: 'Up next, octave. A calm one for the evening.' };
  await expect(panel.getByText('Speaking')).toHaveCount(0, { timeout: 6_000 });
  await expect(panel).toContainText('Up next, octave. A calm one for the evening.');

  // And they stay when the listener stops.
  await page.getByRole('button', { name: 'Stop' }).click();
  await expect(page.getByRole('button', { name: 'Listen' })).toBeVisible();
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('Up next, octave. A calm one for the evening.');
});

test('a server without a presenter has no place for one on the page', async ({ page }) => {
  await stubPresenter(page, () => ({ flags: 0, said: '' }));
  await page.goto('/');
  await expect(page.getByRole('button', { name: 'Listen' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'AI presenter' })).toHaveCount(0);
  await expect(page.getByText('AI presenter')).toHaveCount(0);
});

test('a refusal on the first try is shown and not retried', async ({ page }) => {
  await stopBackend();
  await page.goto('/');
  await page.getByRole('button', { name: 'Listen' }).click();
  await expect(page.getByRole('status')).toContainText('Could not connect:');
  await expect(page.getByRole('button', { name: 'Listen' })).toBeVisible();
});

test('Stop while connecting cancels it and leaves no listener', async ({ page }) => {
  await page.goto('/');
  const button = playerButton(page);
  await button.click();
  await expect(button).toContainText('Connecting…');
  await button.click();
  await expect(button).toHaveText('Listen');
  await expect(page.getByRole('status')).toHaveText('');
  // Whatever was being made is dropped, and the page does not come back to "Playing" by itself.
  await expect.poll(listeners, { timeout: 15_000 }).toBe(0);
  await expect(page.getByRole('status')).toHaveText('');
  await expect(button).toHaveText('Listen');
});

test('closing the tab leaves no listener on the server', async ({ page }) => {
  await page.goto('/');
  await startListening(page);
  expect(await listeners()).toBe(1);
  await page.close({ runBeforeUnload: true });
  await expect.poll(listeners, { timeout: 20_000 }).toBe(0);
});
