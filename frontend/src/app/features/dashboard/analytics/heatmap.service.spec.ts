import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { HeatmapService } from './heatmap.service';

describe('HeatmapService', () => {
  let http: HttpTestingController;
  let service: HeatmapService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(HeatmapService);
  });

  afterEach(() => {
    http.match(() => true);
  });

  it('stays hidden and issues no request until toggled on', () => {
    expect(service.visible()).toBeFalse();
    http.expectNone(() => true);
  });

  it('fetches cells and tracks the peak when toggled on', () => {
    service.toggle(24, 0.25);

    const req = http.expectOne((r) => r.url === '/api/analytics/heatmap');
    expect(req.request.params.get('cell')).toBe('0.25');
    req.flush([
      { latitude: 50.1, longitude: 30.1, samples: 4, drones: 2 },
      { latitude: 51.1, longitude: 31.1, samples: 10, drones: 5 },
    ]);

    expect(service.visible()).toBeTrue();
    expect(service.cells().length).toBe(2);
    expect(service.peakSamples()).toBe(10);
    expect(service.cellDegrees()).toBe(0.25);
  });

  it('builds animation frames and a global peak from a time-lapse fetch', () => {
    service.playTimelapse(12, 0.25);

    const requests = http.match((r) => r.url === '/api/analytics/heatmap');
    expect(requests.length).toBe(12);
    requests.forEach((req, i) => {
      req.flush([{ latitude: 50, longitude: 30, samples: i, drones: 1 }]);
    });

    expect(service.frameCount()).toBe(12);
    expect(service.peakSamples()).toBe(11);
    expect(service.visible()).toBeTrue();

    service.setFrame(3);
    expect(service.playing()).toBeFalse();
    expect(service.cells()[0].samples).toBe(3);

    service.pause();
  });

  it('clears the layer when toggled off', () => {
    service.toggle(24, 0.25);
    http
      .expectOne((r) => r.url === '/api/analytics/heatmap')
      .flush([{ latitude: 50.1, longitude: 30.1, samples: 4, drones: 2 }]);

    service.toggle(24, 0.25);

    expect(service.visible()).toBeFalse();
    expect(service.cells()).toEqual([]);
  });
});
