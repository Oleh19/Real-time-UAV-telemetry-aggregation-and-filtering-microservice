import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';

import { DroneSample } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';
import { PredictionService } from '../prediction/prediction.service';
import { DronePrediction } from '../prediction/prediction.service';
import { ThreatService } from './threat.service';

function drone(overrides: Partial<DroneSample> = {}): DroneSample {
  return {
    DroneID: 'target-001',
    Class: 'recon-uav',
    Timestamp: '2026-07-21T10:00:00Z',
    Latitude: 50,
    Longitude: 30,
    Altitude: 200,
    Speed: 60,
    Confidence: 90,
    Quality: 100,
    Anomaly: false,
    ...overrides,
  };
}

describe('ThreatService', () => {
  const drones = signal<DroneSample[]>([]);
  const byDroneId = signal(new Map<string, DronePrediction>());

  function configure(): ThreatService {
    TestBed.configureTestingModule({
      providers: [
        ThreatService,
        { provide: TelemetryService, useValue: { drones } },
        { provide: PredictionService, useValue: { byDroneId } },
      ],
    });
    return TestBed.inject(ThreatService);
  }

  afterEach(() => {
    drones.set([]);
    byDroneId.set(new Map());
    TestBed.resetTestingModule();
  });

  it('ranks a loitering munition above a multirotor', () => {
    const service = configure();
    drones.set([
      drone({ DroneID: 'm', Class: 'multirotor', Speed: 40 }),
      drone({ DroneID: 'lm', Class: 'loitering-munition', Speed: 280 }),
    ]);
    const ranked = service.threats();
    expect(ranked[0].droneId).toBe('lm');
    expect(ranked[0].score).toBeGreaterThan(ranked[1].score);
    expect(service.topThreat()?.droneId).toBe('lm');
  });

  it('raises the score for a target heading into a zone soon', () => {
    const service = configure();
    drones.set([drone({ DroneID: 'far' }), drone({ DroneID: 'near' })]);
    byDroneId.set(
      new Map<string, DronePrediction>([
        ['near', { droneId: 'near', path: [], eta: { zoneName: 'Kyiv Oblast', seconds: 20 } }],
      ]),
    );
    const near = service.threats().find((t) => t.droneId === 'near')!;
    const far = service.threats().find((t) => t.droneId === 'far')!;
    expect(near.score).toBeGreaterThan(far.score);
    expect(near.zoneName).toBe('Kyiv Oblast');
  });

  it('discounts low-quality tracks', () => {
    const service = configure();
    drones.set([drone({ DroneID: 'sure', Quality: 100 }), drone({ DroneID: 'shaky', Quality: 0 })]);
    const sure = service.threats().find((t) => t.droneId === 'sure')!;
    const shaky = service.threats().find((t) => t.droneId === 'shaky')!;
    expect(sure.score).toBeGreaterThan(shaky.score);
  });
});
