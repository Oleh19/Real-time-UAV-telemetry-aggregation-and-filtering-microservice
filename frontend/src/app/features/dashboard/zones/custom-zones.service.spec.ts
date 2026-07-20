import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { ZoneFeatureCollection } from '../../../core/models/telemetry';
import { CustomZonesService } from './custom-zones.service';

const oneZone: ZoneFeatureCollection = {
  type: 'FeatureCollection',
  features: [
    {
      type: 'Feature',
      properties: { id: 1000001, name: 'Airfield' },
      geometry: { type: 'Polygon', coordinates: [] },
    },
  ],
};

describe('CustomZonesService', () => {
  let service: CustomZonesService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(CustomZonesService);
    http = TestBed.inject(HttpTestingController);
    http.expectOne('/api/custom-zones').flush(oneZone);
  });

  afterEach(() => {
    http.verify();
  });

  it('loads zones on startup', () => {
    expect(service.zones().features.length).toBe(1);
    expect(service.zones().features[0].properties.name).toBe('Airfield');
  });

  it('collects vertices only while drawing and saves them as lon-lat pairs', () => {
    service.addVertex({ latitude: 50.0, longitude: 30.0 });
    expect(service.vertices().length).toBe(0);

    service.startDrawing();
    service.addVertex({ latitude: 50.0, longitude: 30.0 });
    service.addVertex({ latitude: 50.1, longitude: 30.0 });
    expect(service.canSave()).toBeFalse();
    service.addVertex({ latitude: 50.1, longitude: 30.1 });
    expect(service.canSave()).toBeTrue();

    service.save('Airfield perimeter');
    const request = http.expectOne('/api/custom-zones');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      name: 'Airfield perimeter',
      coordinates: [
        [30.0, 50.0],
        [30.0, 50.1],
        [30.1, 50.1],
      ],
    });
    request.flush(
      { id: 1000002, name: 'Airfield perimeter' },
      { status: 201, statusText: 'Created' },
    );

    expect(service.drawing()).toBeFalse();
    http.expectOne('/api/custom-zones').flush(oneZone);
  });

  it('undoes vertices and cancels drawing', () => {
    service.startDrawing();
    service.addVertex({ latitude: 50.0, longitude: 30.0 });
    service.undoVertex();
    expect(service.vertices().length).toBe(0);

    service.cancelDrawing();
    expect(service.drawing()).toBeFalse();
  });

  it('deletes a zone and reloads the list', () => {
    service.remove(1000001);
    const request = http.expectOne('/api/custom-zones/1000001');
    expect(request.request.method).toBe('DELETE');
    request.flush(null, { status: 204, statusText: 'No Content' });
    http.expectOne('/api/custom-zones').flush({ type: 'FeatureCollection', features: [] });
    expect(service.zones().features.length).toBe(0);
  });
});
