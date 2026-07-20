import { Injectable, computed, effect, inject, signal } from '@angular/core';

import { DroneSample } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';
import { IndexedZone, indexZones, zoneContains } from './geometry';

export interface ZoneEta {
  zoneName: string;
  seconds: number;
}

export interface DronePrediction {
  droneId: string;
  path: [number, number][];
  eta: ZoneEta | null;
}

export const predictionHorizonSeconds = 120;
export const etaSearchSeconds = 600;
export const etaStepSeconds = 10;

interface Velocity {
  latitudePerSecond: number;
  longitudePerSecond: number;
}

export function velocityBetween(previous: DroneSample, current: DroneSample): Velocity | null {
  const elapsedSeconds =
    (new Date(current.Timestamp).getTime() - new Date(previous.Timestamp).getTime()) / 1000;
  if (elapsedSeconds <= 0) {
    return null;
  }
  const velocity: Velocity = {
    latitudePerSecond: (current.Latitude - previous.Latitude) / elapsedSeconds,
    longitudePerSecond: (current.Longitude - previous.Longitude) / elapsedSeconds,
  };
  if (velocity.latitudePerSecond === 0 && velocity.longitudePerSecond === 0) {
    return null;
  }
  return velocity;
}

export function firstZoneOnCourse(
  sample: DroneSample,
  velocity: Velocity,
  zones: IndexedZone[],
): ZoneEta | null {
  const outsideZones = zones.filter(
    (zone) => !zoneContains(zone, sample.Longitude, sample.Latitude),
  );
  for (let seconds = etaStepSeconds; seconds <= etaSearchSeconds; seconds += etaStepSeconds) {
    const longitude = sample.Longitude + velocity.longitudePerSecond * seconds;
    const latitude = sample.Latitude + velocity.latitudePerSecond * seconds;
    for (const zone of outsideZones) {
      if (zoneContains(zone, longitude, latitude)) {
        return { zoneName: zone.name, seconds };
      }
    }
  }
  return null;
}

@Injectable({ providedIn: 'root' })
export class PredictionService {
  private readonly telemetry = inject(TelemetryService);

  private readonly lastDistinct = new Map<string, { previous?: DroneSample; last: DroneSample }>();
  private readonly predictionsState = signal<DronePrediction[]>([]);

  readonly predictions = this.predictionsState.asReadonly();
  readonly byDroneId = computed(() => {
    const index = new Map<string, DronePrediction>();
    for (const prediction of this.predictionsState()) {
      index.set(prediction.droneId, prediction);
    }
    return index;
  });

  private readonly indexedZones = computed(() => indexZones(this.telemetry.zones()));

  constructor() {
    effect(() => {
      const drones = this.telemetry.drones();
      const zones = this.indexedZones();
      this.rememberPositions(drones);
      this.predictionsState.set(
        drones
          .map((drone) => this.predict(drone, zones))
          .filter((prediction): prediction is DronePrediction => prediction !== null),
      );
    });
  }

  private rememberPositions(drones: DroneSample[]): void {
    const seen = new Set<string>();
    for (const drone of drones) {
      seen.add(drone.DroneID);
      const stored = this.lastDistinct.get(drone.DroneID);
      if (!stored) {
        this.lastDistinct.set(drone.DroneID, { last: drone });
        continue;
      }
      if (stored.last.Timestamp !== drone.Timestamp) {
        this.lastDistinct.set(drone.DroneID, { previous: stored.last, last: drone });
      }
    }
    for (const droneId of this.lastDistinct.keys()) {
      if (!seen.has(droneId)) {
        this.lastDistinct.delete(droneId);
      }
    }
  }

  private predict(drone: DroneSample, zones: IndexedZone[]): DronePrediction | null {
    const stored = this.lastDistinct.get(drone.DroneID);
    if (!stored?.previous) {
      return null;
    }
    const velocity = velocityBetween(stored.previous, stored.last);
    if (!velocity) {
      return null;
    }
    const path: [number, number][] = [
      [stored.last.Latitude, stored.last.Longitude],
      [
        stored.last.Latitude + velocity.latitudePerSecond * predictionHorizonSeconds,
        stored.last.Longitude + velocity.longitudePerSecond * predictionHorizonSeconds,
      ],
    ];
    return {
      droneId: drone.DroneID,
      path,
      eta: firstZoneOnCourse(stored.last, velocity, zones),
    };
  }
}
