import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, discardPeriodicTasks, fakeAsync, tick } from '@angular/core/testing';

import { BreachRecord } from '../../../core/models/telemetry';
import { BreachFeedService } from './breach-feed.service';

function record(overrides: Partial<BreachRecord> = {}): BreachRecord {
  return {
    DroneID: 'drone-001',
    ZoneID: 7,
    ZoneName: 'Kyiv Oblast',
    Event: 'entered',
    OccurredAt: '2026-07-21T10:00:00Z',
    Latitude: 50.45,
    Longitude: 30.52,
    Altitude: 120,
    ...overrides,
  };
}

describe('BreachFeedService', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
  });

  it('polls the breach journal and exposes the records', fakeAsync(() => {
    const service = TestBed.inject(BreachFeedService);
    expect(service.breaches()).toEqual([]);

    tick();
    http.expectOne((req) => req.url === '/api/breaches').flush([record()]);
    expect(service.breaches().length).toBe(1);
    expect(service.breaches()[0].ZoneName).toBe('Kyiv Oblast');

    tick(5000);
    http
      .expectOne((req) => req.url === '/api/breaches')
      .flush([record({ Event: 'exited', OccurredAt: '2026-07-21T10:05:00Z' }), record()]);
    expect(service.breaches().length).toBe(2);
    expect(service.breaches()[0].Event).toBe('exited');

    discardPeriodicTasks();
  }));

  it('keeps the last good records when a poll fails', fakeAsync(() => {
    const service = TestBed.inject(BreachFeedService);

    tick();
    http.expectOne((req) => req.url === '/api/breaches').flush([record()]);
    expect(service.breaches().length).toBe(1);

    tick(5000);
    http
      .expectOne((req) => req.url === '/api/breaches')
      .flush('boom', { status: 500, statusText: 'Internal Server Error' });
    expect(service.breaches().length).toBe(1);

    discardPeriodicTasks();
  }));
});
