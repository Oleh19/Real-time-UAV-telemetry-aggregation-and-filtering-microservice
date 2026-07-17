import type { Geometry } from 'geojson';

export interface DroneSample {
  DroneID: string;
  Timestamp: string;
  Latitude: number;
  Longitude: number;
  Altitude: number;
  Speed: number;
  Confidence: number;
}

export interface IngestStats {
  Received: number;
  Dropped: number;
  Published: number;
  Failed: number;
  Rejected: number;
}

export interface OblastAlert {
  id: number;
  name: string;
  alarmed: boolean;
  drones: number;
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
