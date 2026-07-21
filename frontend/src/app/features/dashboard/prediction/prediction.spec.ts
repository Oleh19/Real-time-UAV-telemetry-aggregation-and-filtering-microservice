import { DroneSample, ZoneFeatureCollection } from '../../../core/models/telemetry';
import { indexZones, pointInPolygon, zoneContains } from './geometry';
import { etaStepSeconds, firstZoneOnCourse, velocityBetween } from './prediction.service';

function sample(overrides: Partial<DroneSample> = {}): DroneSample {
  return {
    DroneID: 'drone-001',
    Class: 'recon-uav',
    Timestamp: '2026-07-21T10:00:00Z',
    Latitude: 50.0,
    Longitude: 30.0,
    Altitude: 120,
    Speed: 20,
    Confidence: 90,
    ...overrides,
  };
}

const squareZone: ZoneFeatureCollection = {
  type: 'FeatureCollection',
  features: [
    {
      type: 'Feature',
      properties: { id: 1, name: 'Test Square' },
      geometry: {
        type: 'Polygon',
        coordinates: [
          [
            [31.0, 50.5],
            [32.0, 50.5],
            [32.0, 51.5],
            [31.0, 51.5],
            [31.0, 50.5],
          ],
        ],
      },
    },
  ],
};

describe('prediction geometry', () => {
  it('detects points inside and outside a polygon', () => {
    const ring = squareZone.features[0].geometry as { coordinates: number[][][] };
    expect(pointInPolygon(31.5, 51.0, ring.coordinates as never)).toBeTrue();
    expect(pointInPolygon(30.0, 50.0, ring.coordinates as never)).toBeFalse();
  });

  it('indexes zones with bounding boxes for fast rejection', () => {
    const zones = indexZones(squareZone);
    expect(zones.length).toBe(1);
    expect(zoneContains(zones[0], 31.5, 51.0)).toBeTrue();
    expect(zoneContains(zones[0], 29.0, 49.0)).toBeFalse();
  });
});

describe('velocityBetween', () => {
  it('derives degrees per second from two distinct samples', () => {
    const previous = sample();
    const current = sample({
      Timestamp: '2026-07-21T10:00:10Z',
      Latitude: 50.1,
      Longitude: 30.2,
    });
    const velocity = velocityBetween(previous, current);
    expect(velocity).not.toBeNull();
    expect(velocity?.latitudePerSecond).toBeCloseTo(0.01, 6);
    expect(velocity?.longitudePerSecond).toBeCloseTo(0.02, 6);
  });

  it('returns null for stationary drones or non-positive time deltas', () => {
    expect(velocityBetween(sample(), sample())).toBeNull();
    expect(velocityBetween(sample({ Timestamp: '2026-07-21T10:00:10Z' }), sample())).toBeNull();
  });
});

describe('firstZoneOnCourse', () => {
  const zones = indexZones(squareZone);

  it('finds the ETA to a zone lying on the flight path', () => {
    const eta = firstZoneOnCourse(
      sample({ Latitude: 51.0, Longitude: 30.0 }),
      { latitudePerSecond: 0, longitudePerSecond: 0.01 },
      zones,
    );
    expect(eta).not.toBeNull();
    expect(eta?.zoneName).toBe('Test Square');
    expect(eta!.seconds).toBeGreaterThanOrEqual(etaStepSeconds);
    expect(eta!.seconds).toBeLessThanOrEqual(110);
  });

  it('returns null when the course misses every zone', () => {
    const eta = firstZoneOnCourse(
      sample({ Latitude: 49.0, Longitude: 30.0 }),
      { latitudePerSecond: -0.01, longitudePerSecond: 0 },
      zones,
    );
    expect(eta).toBeNull();
  });

  it('ignores zones the drone is already inside', () => {
    const eta = firstZoneOnCourse(
      sample({ Latitude: 51.0, Longitude: 31.5 }),
      { latitudePerSecond: 0, longitudePerSecond: 0.0001 },
      zones,
    );
    expect(eta).toBeNull();
  });
});
