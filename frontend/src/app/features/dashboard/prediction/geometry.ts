import type { Geometry, Position } from 'geojson';

import { ZoneFeatureCollection } from '../../../core/models/telemetry';

export interface BoundingBox {
  minLongitude: number;
  minLatitude: number;
  maxLongitude: number;
  maxLatitude: number;
}

export interface IndexedZone {
  id: number;
  name: string;
  polygons: Position[][][];
  bounds: BoundingBox;
}

export function indexZones(zones: ZoneFeatureCollection): IndexedZone[] {
  const indexed: IndexedZone[] = [];
  for (const feature of zones.features) {
    const polygons = polygonsOf(feature.geometry);
    if (polygons.length === 0) {
      continue;
    }
    indexed.push({
      id: feature.properties.id,
      name: feature.properties.name,
      polygons,
      bounds: boundsOf(polygons),
    });
  }
  return indexed;
}

function polygonsOf(geometry: Geometry): Position[][][] {
  if (geometry.type === 'Polygon') {
    return [geometry.coordinates];
  }
  if (geometry.type === 'MultiPolygon') {
    return geometry.coordinates;
  }
  return [];
}

function boundsOf(polygons: Position[][][]): BoundingBox {
  const bounds: BoundingBox = {
    minLongitude: 180,
    minLatitude: 90,
    maxLongitude: -180,
    maxLatitude: -90,
  };
  for (const polygon of polygons) {
    for (const ring of polygon) {
      for (const [longitude, latitude] of ring) {
        bounds.minLongitude = Math.min(bounds.minLongitude, longitude);
        bounds.maxLongitude = Math.max(bounds.maxLongitude, longitude);
        bounds.minLatitude = Math.min(bounds.minLatitude, latitude);
        bounds.maxLatitude = Math.max(bounds.maxLatitude, latitude);
      }
    }
  }
  return bounds;
}

export function zoneContains(zone: IndexedZone, longitude: number, latitude: number): boolean {
  if (
    longitude < zone.bounds.minLongitude ||
    longitude > zone.bounds.maxLongitude ||
    latitude < zone.bounds.minLatitude ||
    latitude > zone.bounds.maxLatitude
  ) {
    return false;
  }
  return zone.polygons.some((polygon) => pointInPolygon(longitude, latitude, polygon));
}

export function pointInPolygon(longitude: number, latitude: number, rings: Position[][]): boolean {
  let inside = false;
  for (const ring of rings) {
    for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) {
      const [longitudeI, latitudeI] = ring[i];
      const [longitudeJ, latitudeJ] = ring[j];
      if (
        latitudeI > latitude !== latitudeJ > latitude &&
        longitude <
          ((longitudeJ - longitudeI) * (latitude - latitudeI)) / (latitudeJ - latitudeI) +
            longitudeI
      ) {
        inside = !inside;
      }
    }
  }
  return inside;
}
