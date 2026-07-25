import type { Geometry } from 'geojson';

export interface DroneSample {
  DroneID: string;
  Class: string;
  Timestamp: string;
  Latitude: number;
  Longitude: number;
  Altitude: number;
  Speed: number;
  Confidence: number;
  Quality: number;
  Anomaly: boolean;
  Squawk: string;
  Friendly: boolean;
}

export interface FriendlySquawk {
  code: string;
  label: string;
}

export type ReplayState = 'running' | 'completed' | 'cancelled' | 'failed';

export interface ReplayStatus {
  id: string;
  state: ReplayState;
  speed: number;
  paused: boolean;
  from: string;
  to: string;
  droneId?: string;
  total: number;
  published: number;
  startedAt: string;
}

export interface HeatCell {
  latitude: number;
  longitude: number;
  samples: number;
  drones: number;
}

export interface IngestStats {
  Received: number;
  Dropped: number;
  Published: number;
  Failed: number;
  Rejected: number;
}

export type ZoneKind = 'oblast' | 'custom';

export interface OblastAlert {
  id: number;
  name: string;
  kind: ZoneKind;
  alarmed: boolean;
  drones: number;
}

export type BreachEvent = 'entered' | 'exited';

export type BreachStatus = 'open' | 'acknowledged' | 'resolved';

export interface BreachRecord {
  ID: number;
  DroneID: string;
  ZoneID: number;
  ZoneName: string;
  Event: BreachEvent;
  Status: BreachStatus;
  OccurredAt: string;
  Latitude: number;
  Longitude: number;
  Altitude: number;
}

export interface ZoneFeature {
  type: 'Feature';
  properties: {
    id: number;
    name: string;
  };
  geometry: Geometry;
}

export interface ZoneFeatureCollection {
  type: 'FeatureCollection';
  features: ZoneFeature[];
}

export const EMPTY_ZONES: ZoneFeatureCollection = {
  type: 'FeatureCollection',
  features: [],
};
