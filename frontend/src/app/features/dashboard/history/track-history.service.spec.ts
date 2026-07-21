import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { DroneSample } from '../../../core/models/telemetry';
import { TrackHistoryService } from './track-history.service';

function samplePoint(overrides: Partial<DroneSample> = {}): DroneSample {
  return {
    DroneID: 'drone-001',
    Class: 'recon-uav',
    Timestamp: '2026-07-21T10:00:00Z',
    Latitude: 50.4,
    Longitude: 30.5,
    Altitude: 120,
    Speed: 20,
    Confidence: 90,
    ...overrides,
  };
}

describe('TrackHistoryService', () => {
  let service: TrackHistoryService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(TrackHistoryService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
  });

  function flushTrack(points: DroneSample[]): void {
    const request = http.expectOne(
      (req) => req.url === '/api/history' && req.params.get('drone_id') === 'drone-001',
    );
    request.flush(points);
  }

  it('loads a track and lands on its last point', () => {
    service.load('drone-001');
    expect(service.loading()).toBeTrue();

    flushTrack([
      samplePoint(),
      samplePoint({ Timestamp: '2026-07-21T10:00:01Z', Latitude: 50.41 }),
      samplePoint({ Timestamp: '2026-07-21T10:00:02Z', Latitude: 50.42 }),
    ]);

    expect(service.loading()).toBeFalse();
    expect(service.track().length).toBe(3);
    expect(service.index()).toBe(2);
    expect(service.currentPoint()?.Latitude).toBe(50.42);
  });

  it('falls back to an empty track when the request fails', () => {
    service.load('drone-001');
    http
      .expectOne((req) => req.url === '/api/history')
      .flush('boom', { status: 500, statusText: 'Internal Server Error' });

    expect(service.track()).toEqual([]);
    expect(service.loading()).toBeFalse();
  });

  it('clamps seek to the track bounds and pauses playback', () => {
    service.load('drone-001');
    flushTrack([samplePoint(), samplePoint({ Timestamp: '2026-07-21T10:00:01Z' })]);

    service.togglePlayback();
    expect(service.playing()).toBeTrue();

    service.seek(99);
    expect(service.index()).toBe(1);
    expect(service.playing()).toBeFalse();

    service.seek(-5);
    expect(service.index()).toBe(0);
  });

  it('restarts playback from the beginning when toggled at the end', () => {
    service.load('drone-001');
    flushTrack([
      samplePoint(),
      samplePoint({ Timestamp: '2026-07-21T10:00:01Z' }),
      samplePoint({ Timestamp: '2026-07-21T10:00:02Z' }),
    ]);

    service.togglePlayback();
    expect(service.index()).toBe(0);
    expect(service.playing()).toBeTrue();

    service.advance();
    expect(service.index()).toBe(1);
    service.advance();
    expect(service.index()).toBe(2);
    expect(service.playing()).toBeFalse();
  });

  it('clear resets all playback state', () => {
    service.load('drone-001');
    flushTrack([samplePoint()]);

    service.clear();
    expect(service.droneId()).toBeNull();
    expect(service.track()).toEqual([]);
    expect(service.index()).toBe(0);
    expect(service.playing()).toBeFalse();
  });
});
