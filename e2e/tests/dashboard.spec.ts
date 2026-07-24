import { expect, test } from '@playwright/test';

const drone = {
  DroneID: 'drone-e2e-1',
  Class: 'recon-uav',
  Timestamp: '2026-07-24T10:00:00Z',
  Latitude: 50.4,
  Longitude: 30.5,
  Altitude: 120,
  Speed: 30,
  Confidence: 88,
  Quality: 80,
  Anomaly: false,
  Squawk: '',
  Friendly: false,
};

const stats = { Received: 1, Dropped: 0, Published: 1, Failed: 0, Rejected: 0 };
const alerts = [{ id: 1, name: 'Kyiv Oblast', alarmed: true, drones: 1 }];

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
  await page.route('**/api/zones', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ type: 'FeatureCollection', features: [] }),
    }),
  );
  await page.route('**/api/alert-stream', (route) => route.fulfill(sse(alerts)));
  await page.route('**/api/stream', (route) => route.fulfill(sse({ drones: [drone], stats })));
});

test('renders the shell and streams a live target into the table', async ({ page }) => {
  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'UAV Telemetry Monitor' })).toBeVisible();

  await page.getByRole('tab', { name: 'Targets' }).click();
  await expect(page.getByText('drone-e2e-1')).toBeVisible();
});

test('toggles between light and dark themes', async ({ page }) => {
  await page.goto('/');

  const root = page.locator('html');
  const before = await root.getAttribute('data-theme');

  await page.locator('.theme-toggle').click();

  await expect.poll(async () => root.getAttribute('data-theme')).not.toBe(before);
});

test('highlights a custom zone the geofence reports as alarmed', async ({ page }) => {
  await page.route('**/api/alert-stream', (route) =>
    route.fulfill(
      sse([{ id: 5000001, name: 'Test zone', kind: 'custom', alarmed: true, drones: 1 }]),
    ),
  );
  await page.route('**/api/custom-zones', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        type: 'FeatureCollection',
        features: [
          {
            type: 'Feature',
            properties: { id: 5000001, name: 'Test zone' },
            geometry: {
              type: 'Polygon',
              coordinates: [
                [
                  [30.4, 50.3],
                  [30.6, 50.3],
                  [30.6, 50.5],
                  [30.4, 50.5],
                  [30.4, 50.3],
                ],
              ],
            },
          },
        ],
      }),
    }),
  );

  await page.goto('/');

  await expect(page.locator('path[stroke="#d64545"]').first()).toBeVisible({ timeout: 10000 });
});

test('shows the custom zone name on hover, not the oblast beneath it', async ({ page }) => {
  await page.route('**/api/stream', (route) => route.fulfill(sse({ drones: [], stats })));
  await page.route('**/api/zones', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        type: 'FeatureCollection',
        features: [
          {
            type: 'Feature',
            properties: { id: 1, name: 'Kyiv Oblast' },
            geometry: {
              type: 'Polygon',
              coordinates: [
                [
                  [29, 49],
                  [33, 49],
                  [33, 52],
                  [29, 52],
                  [29, 49],
                ],
              ],
            },
          },
        ],
      }),
    }),
  );
  await page.route('**/api/custom-zones', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        type: 'FeatureCollection',
        features: [
          {
            type: 'Feature',
            properties: { id: 5000001, name: 'HQ perimeter' },
            geometry: {
              type: 'Polygon',
              coordinates: [
                [
                  [30.4, 50.3],
                  [30.6, 50.3],
                  [30.6, 50.5],
                  [30.4, 50.5],
                  [30.4, 50.3],
                ],
              ],
            },
          },
        ],
      }),
    }),
  );

  await page.goto('/');

  await page.locator('.leaflet-customZones-pane path').first().hover();
  const tooltip = page.locator('.leaflet-tooltip');
  await expect(tooltip).toHaveText('HQ perimeter');
});

test('filters the target table', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('tab', { name: 'Targets' }).click();

  await expect(page.getByText('drone-e2e-1')).toBeVisible();

  await page.getByRole('button', { name: 'Friendly', exact: true }).click();
  await expect(page.getByText('drone-e2e-1')).toHaveCount(0);

  await page.getByRole('button', { name: 'All', exact: true }).click();
  await expect(page.getByText('drone-e2e-1')).toBeVisible();
});
