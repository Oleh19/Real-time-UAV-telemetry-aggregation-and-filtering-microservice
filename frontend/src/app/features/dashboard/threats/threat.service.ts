import { Injectable, computed, inject } from '@angular/core';

import { DroneSample } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';
import { PredictionService } from '../prediction/prediction.service';

export interface Threat {
  droneId: string;
  targetClass: string;
  score: number;
  anomaly: boolean;
  etaSeconds: number | null;
  zoneName: string | null;
}

const classWeight: Record<string, number> = {
  'loitering-munition': 50,
  'recon-uav': 30,
  multirotor: 15,
  unknown: 10,
};

const maxEtaSeconds = 600;
const cruiseSpeedMps = 300;

@Injectable({ providedIn: 'root' })
export class ThreatService {
  private readonly telemetry = inject(TelemetryService);
  private readonly prediction = inject(PredictionService);

  readonly threats = computed<Threat[]>(() => {
    const predictions = this.prediction.byDroneId();
    return this.telemetry
      .drones()
      .map((drone) => this.score(drone, predictions.get(drone.DroneID)?.eta ?? null))
      .sort((a, b) => b.score - a.score);
  });

  readonly topThreat = computed<Threat | null>(() => this.threats()[0] ?? null);

  private score(drone: DroneSample, eta: { zoneName: string; seconds: number } | null): Threat {
    let score = classWeight[drone.Class] ?? classWeight['unknown'];
    if (eta) {
      score += 40 * Math.max(0, 1 - eta.seconds / maxEtaSeconds);
    }
    score += 10 * Math.min(1, drone.Speed / cruiseSpeedMps);
    if (drone.Anomaly) {
      score += 10;
    }
    const certainty = 0.5 + 0.5 * (drone.Quality / 100);
    return {
      droneId: drone.DroneID,
      targetClass: drone.Class || 'unknown',
      score: Math.round(Math.min(100, score * certainty)),
      anomaly: drone.Anomaly,
      etaSeconds: eta?.seconds ?? null,
      zoneName: eta?.zoneName ?? null,
    };
  }
}
