import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, discardPeriodicTasks, fakeAsync, tick } from '@angular/core/testing';

import { FleetSnapshot } from './fleet.models';
import { FleetService } from './fleet.service';

const snapshot: FleetSnapshot = {
  drones: [
    {
      id: 'uav-1',
      model: 'quad',
      status: 'idle',
      battery: 100,
      base: { latitude: 50, longitude: 30 },
      latitude: 50,
      longitude: 30,
      firmware: '1.0.0',
    },
  ],
  missions: [
    {
      id: 'mission-001',
      name: 'recon',
      droneId: 'uav-1',
      waypoints: [{ latitude: 50.1, longitude: 30.1 }],
      state: 'planned',
      progress: 0,
    },
  ],
};

describe('FleetService', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), FleetService],
    });
    http = TestBed.inject(HttpTestingController);
  });

  it('polls the fleet snapshot and exposes drones and missions', fakeAsync(() => {
    const service = TestBed.inject(FleetService);
    expect(service.drones()).toEqual([]);

    tick();
    http.expectOne('/api/fleet').flush(snapshot);

    expect(service.drones().length).toBe(1);
    expect(service.drones()[0].id).toBe('uav-1');
    expect(service.missions()[0].name).toBe('recon');

    discardPeriodicTasks();
  }));

  it('issues C2 commands over the fleet API', fakeAsync(() => {
    const service = TestBed.inject(FleetService);
    tick();
    http.expectOne('/api/fleet').flush(snapshot);

    service.createMission({
      name: 'x',
      droneId: 'uav-1',
      waypoints: [{ latitude: 1, longitude: 2 }],
    });
    const created = http.expectOne('/api/fleet/missions');
    expect(created.request.method).toBe('POST');
    created.flush({});

    service.missionAction('mission-001', 'launch');
    const launched = http.expectOne('/api/fleet/missions/mission-001/launch');
    expect(launched.request.method).toBe('POST');
    launched.flush({});

    service.recall('uav-1');
    const recalled = http.expectOne('/api/fleet/drones/uav-1/recall');
    expect(recalled.request.method).toBe('POST');
    recalled.flush({});

    service.removeDrone('uav-1');
    const removed = http.expectOne('/api/fleet/drones/uav-1');
    expect(removed.request.method).toBe('DELETE');
    removed.flush({});

    discardPeriodicTasks();
  }));
});
