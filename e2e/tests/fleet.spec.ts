import { expect, test } from '@playwright/test';

const snapshot = {
  drones: [
    {
      id: 'uav-1',
      model: 'quad',
      status: 'idle',
      battery: 100,
      base: { latitude: 50, longitude: 30 },
      latitude: 50,
      longitude: 30,
      firmware: '1.0.0',
    },
  ],
  missions: [
    {
      id: 'mission-001',
      name: 'recon',
      droneId: 'uav-1',
      waypoints: [{ latitude: 50.1, longitude: 30.1 }],
      state: 'planned',
      progress: 0,
    },
  ],
};

function sse(payload: unknown): { contentType: string; body: string } {
  return {
    contentType: 'text/event-stream',
    body: `retry: 3600000\ndata: ${JSON.stringify(payload)}\n\n`,
  };
}

test.beforeEach(async ({ page }) => {
  await page.route('**/api/**', (route) =>
    route.fulfill({ contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/stream', (route) => route.fulfill(sse({ drones: [], stats: {} })));
  await page.route('**/api/alert-stream', (route) => route.fulfill(sse([])));
  await page.route('**/api/zones', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ type: 'FeatureCollection', features: [] }),
    }),
  );
  await page.route('**/api/fleet', (route) =>
    route.fulfill({ contentType: 'application/json', body: JSON.stringify(snapshot) }),
  );
});

test('fleet view lists the roster and missions', async ({ page }) => {
  await page.goto('/fleet');
  await expect(page.locator('.drone-id')).toHaveText('uav-1');
  await expect(page.locator('.mission-name')).toHaveText('recon');
});

test('launching a mission calls the fleet C2 API', async ({ page }) => {
  await page.goto('/fleet');
  const [request] = await Promise.all([
    page.waitForRequest(
      (r) => r.url().endsWith('/api/fleet/missions/mission-001/launch') && r.method() === 'POST',
    ),
    page.getByRole('button', { name: 'launch' }).click(),
  ]);
  expect(request).toBeTruthy();
});

test('switches between monitoring and fleet views', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'UAV Telemetry Monitor' })).toBeVisible();

  await page.getByRole('link', { name: 'Fleet' }).click();
  await expect(page.getByText('Fleet roster')).toBeVisible();

  await page.getByRole('link', { name: 'Monitoring' }).click();
  await expect(page.getByRole('tab', { name: 'Threats' })).toBeVisible();
});
