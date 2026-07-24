import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, discardPeriodicTasks, fakeAsync, tick } from '@angular/core/testing';

import { FriendlyRegistryService } from './friendly-registry.service';

describe('FriendlyRegistryService', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
  });

  it('exposes the friendly codes fetched over HTTP', fakeAsync(() => {
    const service = TestBed.inject(FriendlyRegistryService);

    tick();
    http.expectOne('/api/friendly').flush([
      { code: 'UAF-01', label: 'Air Force patrol' },
      { code: 'MED-01', label: 'Medical evacuation' },
    ]);

    expect(service.codes().has('UAF-01')).toBeTrue();
    expect(service.codes().has('MED-01')).toBeTrue();
    expect(service.codes().has('HOSTILE')).toBeFalse();

    discardPeriodicTasks();
  }));

  it('falls back to an empty registry when the request fails', fakeAsync(() => {
    const service = TestBed.inject(FriendlyRegistryService);

    tick();
    http.expectOne('/api/friendly').error(new ProgressEvent('error'));

    expect(service.squawks()).toEqual([]);
    expect(service.codes().size).toBe(0);

    discardPeriodicTasks();
  }));
});
