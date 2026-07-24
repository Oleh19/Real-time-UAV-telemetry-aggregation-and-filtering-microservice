import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, discardPeriodicTasks, fakeAsync, tick } from '@angular/core/testing';

import { SwarmFeedService, SwarmRecord } from './swarm-feed.service';

function swarm(overrides: Partial<SwarmRecord> = {}): SwarmRecord {
  return {
    id: 'swarm-001',
    droneIds: ['target-001', 'target-002', 'target-003'],
    latitude: 50.4,
    longitude: 30.5,
    detectedAt: '2026-07-24T10:00:00Z',
    ...overrides,
  };
}

describe('SwarmFeedService', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
  });

  it('polls the swarm endpoint and exposes the records with dashboard keys', fakeAsync(() => {
    const service = TestBed.inject(SwarmFeedService);
    expect(service.swarms()).toEqual([]);

    tick();
    http.expectOne((req) => req.url === '/api/swarms').flush([swarm()]);

    expect(service.swarms().length).toBe(1);
    expect(service.swarms()[0].id).toBe('swarm-001');
    expect(service.swarms()[0].droneIds.length).toBe(3);

    discardPeriodicTasks();
  }));

  it('keeps the last records when a poll fails', fakeAsync(() => {
    const service = TestBed.inject(SwarmFeedService);

    tick();
    http.expectOne((req) => req.url === '/api/swarms').flush([swarm()]);
    expect(service.swarms().length).toBe(1);

    tick(5000);
    http.expectOne((req) => req.url === '/api/swarms').error(new ProgressEvent('error'));
    expect(service.swarms().length).toBe(1);

    discardPeriodicTasks();
  }));
});
