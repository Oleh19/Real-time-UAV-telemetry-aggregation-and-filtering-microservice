import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, discardPeriodicTasks, fakeAsync, tick } from '@angular/core/testing';

import { ReplayStatus } from '../../../core/models/telemetry';
import { ReplayService } from './replay.service';

function status(overrides: Partial<ReplayStatus> = {}): ReplayStatus {
  return {
    id: 'replay-001',
    state: 'running',
    speed: 10,
    paused: false,
    from: '2026-07-25T10:00:00Z',
    to: '2026-07-25T10:15:00Z',
    total: 100,
    published: 20,
    startedAt: '2026-07-25T12:00:00Z',
    ...overrides,
  };
}

describe('ReplayService', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
  });

  it('polls the replay list', fakeAsync(() => {
    const service = TestBed.inject(ReplayService);
    tick();
    http.expectOne('/api/replays').flush([status()]);
    expect(service.replays().length).toBe(1);
    expect(service.replays()[0].id).toBe('replay-001');
    discardPeriodicTasks();
  }));

  it('pauses, resumes and rescales a running replay via PATCH', fakeAsync(() => {
    const service = TestBed.inject(ReplayService);
    tick();
    http.expectOne('/api/replays').flush([status()]);

    service.pause('replay-001');
    const paused = http.expectOne('/api/replays/replay-001');
    expect(paused.request.method).toBe('PATCH');
    expect(paused.request.body).toEqual({ paused: true });
    paused.flush(status({ paused: true }));

    service.resume('replay-001');
    http.expectOne('/api/replays/replay-001').flush(status({ paused: false }));

    service.setSpeed('replay-001', 50);
    const speed = http.expectOne('/api/replays/replay-001');
    expect(speed.request.body).toEqual({ speed: 50 });
    speed.flush(status({ speed: 50 }));

    discardPeriodicTasks();
  }));

  it('starts a replay and cancels one', fakeAsync(() => {
    const service = TestBed.inject(ReplayService);
    tick();
    http.expectOne((r) => r.method === 'GET' && r.url === '/api/replays').flush([]);

    service.start({ from: 'a', to: 'b', speed: 5 });
    const started = http.expectOne((r) => r.method === 'POST' && r.url === '/api/replays');
    expect(started.request.body).toEqual({ from: 'a', to: 'b', speed: 5 });
    started.flush(status());

    service.cancel('replay-001');
    const cancelled = http.expectOne(
      (r) => r.method === 'DELETE' && r.url === '/api/replays/replay-001',
    );
    cancelled.flush(null);

    discardPeriodicTasks();
  }));
});
