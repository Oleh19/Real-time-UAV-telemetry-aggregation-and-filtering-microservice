import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { EVENT_SOURCE_FACTORY } from './event-source';
import { DroneSample, IngestStats, OblastAlert } from './models/telemetry';
import { TelemetryService } from './telemetry.service';

class FakeEventSource {
  static instances = new Map<string, FakeEventSource>();
  onmessage: ((event: MessageEvent) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeEventSource.instances.set(url, this);
  }

  emit(data: unknown): void {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent);
  }

  fail(): void {
    this.onerror?.(new Event('error'));
  }

  close(): void {
    this.closed = true;
  }

  static get(url: string): FakeEventSource {
    const instance = FakeEventSource.instances.get(url);
    if (!instance) {
      throw new Error(`no EventSource opened for ${url}`);
    }
    return instance;
  }
}

const sample: DroneSample = {
  DroneID: 'drone-001',
  Timestamp: '2026-07-17T10:00:00Z',
  Latitude: 50.45,
  Longitude: 30.52,
  Altitude: 120,
  Speed: 15,
  Confidence: 80,
};

const stats: IngestStats = { Received: 10, Dropped: 1, Published: 9, Failed: 0, Rejected: 0 };

const alerts: OblastAlert[] = [
  { id: 1, name: 'Kyiv Oblast', alarmed: true, drones: 2 },
  { id: 2, name: 'Lviv Oblast', alarmed: false, drones: 0 },
];

describe('TelemetryService', () => {
  beforeEach(() => {
    FakeEventSource.instances.clear();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        {
          provide: EVENT_SOURCE_FACTORY,
          useValue: (url: string) => new FakeEventSource(url) as unknown as EventSource,
        },
      ],
    });
  });

  afterEach(() => {
    const http = TestBed.inject(HttpTestingController);
    http.match(() => true);
    http.verify();
  });

  it('loads oblast boundaries once over HTTP', () => {
    TestBed.inject(TelemetryService);
    const http = TestBed.inject(HttpTestingController);
    const collection = {
      type: 'FeatureCollection',
      features: [
        {
          type: 'Feature',
          properties: { id: 1, name: 'Kyiv Oblast' },
          geometry: { type: 'Polygon', coordinates: [[[30.5, 50.44]]] },
        },
      ],
    };
    http.expectOne('/api/zones').flush(collection);

    const service = TestBed.inject(TelemetryService);
    expect(service.zones().features.length).toBe(1);
  });

  it('exposes drones, stats and connection state from the telemetry stream', () => {
    const service = TestBed.inject(TelemetryService);

    FakeEventSource.get('/api/stream').emit({ drones: [sample], stats });

    expect(service.drones()).toEqual([sample]);
    expect(service.stats()).toEqual(stats);
    expect(service.connected()).toBeTrue();
  });

  it('keeps last known drones but reports offline when the stream errors', () => {
    const service = TestBed.inject(TelemetryService);
    const stream = FakeEventSource.get('/api/stream');

    stream.emit({ drones: [sample], stats });
    stream.fail();

    expect(service.connected()).toBeFalse();
    expect(service.drones()).toEqual([sample]);
  });

  it('derives alarmed oblast ids from the alert stream', () => {
    const service = TestBed.inject(TelemetryService);

    FakeEventSource.get('/api/alert-stream').emit(alerts);

    expect(service.alerts()).toEqual(alerts);
    expect(service.alarmedOblastIds().has(1)).toBeTrue();
    expect(service.alarmedOblastIds().has(2)).toBeFalse();
  });
});
