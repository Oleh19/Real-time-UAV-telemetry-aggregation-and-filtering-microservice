import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import * as L from 'leaflet';

import { DroneSample, HeatCell, ZoneFeatureCollection } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';
import { HeatmapService } from '../analytics/heatmap.service';
import { TrackHistoryService } from '../history/track-history.service';
import { DronePrediction, PredictionService } from '../prediction/prediction.service';
import { CustomZonesService, ZoneVertex } from '../zones/custom-zones.service';

const MAP_CENTER: L.LatLngExpression = [48.7, 31.2];
const MAP_ZOOM = 6;
const CULL_MARGIN = 0.5;
const customZonePane = 'customZones';

const calmZoneStyle: L.PathOptions = {
  color: '#8b98a9',
  weight: 1,
  fillColor: '#8b98a9',
  fillOpacity: 0.03,
};

const alarmedZoneStyle: L.PathOptions = {
  color: '#d64545',
  weight: 2,
  fillColor: '#d64545',
  fillOpacity: 0.25,
};

const trackStyle: L.PolylineOptions = {
  color: '#4dabf7',
  weight: 2,
  opacity: 0.85,
};

const playbackMarkerStyle: L.CircleMarkerOptions = {
  radius: 7,
  color: '#4dabf7',
  weight: 2,
  fillColor: '#4dabf7',
  fillOpacity: 0.5,
};

const customZoneStyle: L.PathOptions = {
  color: '#e8890c',
  weight: 2,
  fillColor: '#e8890c',
  fillOpacity: 0.12,
};

const breachedCustomZoneStyle: L.PathOptions = {
  color: '#d64545',
  weight: 2,
  fillColor: '#d64545',
  fillOpacity: 0.3,
};

const zonePreviewStyle: L.PolylineOptions = {
  color: '#e8890c',
  weight: 2,
  dashArray: '6 4',
  fillColor: '#e8890c',
  fillOpacity: 0.08,
};

const predictionStyle: L.PolylineOptions = {
  color: '#8b98a9',
  weight: 1.5,
  dashArray: '4 6',
  opacity: 0.8,
};

@Component({
  selector: 'app-drone-map',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<div class="map" #mapHost></div>',
  styles: ':host { display: block; height: 100%; } .map { height: 100%; }',
})
export class DroneMapComponent {
  private readonly telemetry = inject(TelemetryService);
  private readonly history = inject(TrackHistoryService);
  private readonly customZones = inject(CustomZonesService);
  private readonly prediction = inject(PredictionService);
  private readonly heatmap = inject(HeatmapService);
  private readonly mapHost = viewChild.required<ElementRef<HTMLDivElement>>('mapHost');

  private readonly mapReady = signal(false);
  private map?: L.Map;
  private zonesLayer?: L.GeoJSON;
  private customZonesLayer?: L.GeoJSON;
  private zonePreviewLayer?: L.Polygon;
  private trackLayer?: L.Polyline;
  private playbackMarker?: L.CircleMarker;
  private heatLayer?: L.LayerGroup;
  private readonly zoneLayersById = new Map<number, L.Path>();
  private readonly markers = new Map<string, L.Marker>();
  private readonly predictionLines = new Map<string, L.Polyline>();
  private readonly bearings = new Map<string, number>();
  private readonly lastPositions = new Map<string, L.LatLng>();
  private readonly iconKeys = new Map<string, string>();
  private latestDrones: DroneSample[] = [];
  private droneFrame = 0;

  constructor() {
    afterNextRender(() => this.initMap());
    inject(DestroyRef).onDestroy(() => {
      if (this.droneFrame !== 0) {
        cancelAnimationFrame(this.droneFrame);
      }
      this.map?.remove();
      this.map = undefined;
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderZones(this.telemetry.zones());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.styleZones(this.telemetry.alarmedOblastIds());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.latestDrones = this.telemetry.drones();
      this.scheduleDroneRender();
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderTrack(this.history.track());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderPlaybackMarker(this.history.currentPoint());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderCustomZones(this.customZones.zones());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderZonePreview(this.customZones.drawing(), this.customZones.vertices());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderPredictions(this.prediction.predictions());
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.renderHeatmap(
        this.heatmap.cells(),
        this.heatmap.peakSamples(),
        this.heatmap.cellDegrees(),
      );
    });
    effect(() => {
      if (!this.mapReady()) {
        return;
      }
      this.styleCustomZones(this.telemetry.alarmedZoneIds());
    });
  }

  private initMap(): void {
    this.map = L.map(this.mapHost().nativeElement, { center: MAP_CENTER, zoom: MAP_ZOOM });
    this.map.createPane(customZonePane).style.zIndex = '450';
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; OpenStreetMap contributors',
    }).addTo(this.map);
    this.map.on('click', (event: L.LeafletMouseEvent) => {
      this.customZones.addVertex({ latitude: event.latlng.lat, longitude: event.latlng.lng });
    });
    this.map.on('moveend zoomend', () => this.scheduleDroneRender());
    this.mapReady.set(true);
  }

  private scheduleDroneRender(): void {
    if (this.droneFrame !== 0) {
      return;
    }
    this.droneFrame = requestAnimationFrame(() => {
      this.droneFrame = 0;
      this.renderDrones(this.latestDrones);
    });
  }

  private renderZones(zones: ZoneFeatureCollection): void {
    if (!this.map) {
      return;
    }
    this.zonesLayer?.remove();
    this.zoneLayersById.clear();
    this.zonesLayer = L.geoJSON(zones, {
      style: calmZoneStyle,
      onEachFeature: (feature, layer) => {
        layer.bindTooltip(String(feature.properties?.['name'] ?? 'oblast'));
        if (typeof feature.properties?.['id'] === 'number' && layer instanceof L.Path) {
          this.zoneLayersById.set(feature.properties['id'], layer);
        }
      },
    }).addTo(this.map);
    this.styleZones(this.telemetry.alarmedOblastIds());
  }

  private styleZones(alarmedIds: Set<number>): void {
    for (const [id, layer] of this.zoneLayersById) {
      layer.setStyle(alarmedIds.has(id) ? alarmedZoneStyle : calmZoneStyle);
    }
  }

  private renderDrones(drones: DroneSample[]): void {
    if (!this.map) {
      return;
    }
    const predictions = this.prediction.byDroneId();
    const viewport = this.map.getBounds().pad(CULL_MARGIN);
    const live = new Set<string>();
    for (const drone of drones) {
      live.add(drone.DroneID);
      const position = L.latLng(drone.Latitude, drone.Longitude);
      const bearing = this.updateBearing(drone.DroneID, position);
      if (!viewport.contains(position)) {
        this.hideMarker(drone.DroneID);
        continue;
      }
      const color = confidenceColor(drone.Confidence);
      const iconKey = `${color}:${Math.round(bearing)}`;
      const tooltip = tooltipFor(drone, predictions.get(drone.DroneID));
      const existing = this.markers.get(drone.DroneID);
      if (existing) {
        existing.setLatLng(position);
        if (this.iconKeys.get(drone.DroneID) !== iconKey) {
          existing.setIcon(droneIcon(color, bearing));
          this.iconKeys.set(drone.DroneID, iconKey);
        }
        existing.setTooltipContent(tooltip);
        continue;
      }
      const marker = L.marker(position, { icon: droneIcon(color, bearing) }).bindTooltip(tooltip);
      marker.on('click', () => {
        if (!this.customZones.drawing()) {
          this.history.load(drone.DroneID);
        }
      });
      marker.addTo(this.map);
      this.markers.set(drone.DroneID, marker);
      this.iconKeys.set(drone.DroneID, iconKey);
    }
    this.forgetVanished(live);
  }

  private styleCustomZones(alarmed: Set<number>): void {
    if (!this.customZonesLayer) {
      return;
    }
    this.customZonesLayer.setStyle((feature) =>
      alarmed.has(Number(feature?.properties?.['id'])) ? breachedCustomZoneStyle : customZoneStyle,
    );
  }

  private hideMarker(droneId: string): void {
    const marker = this.markers.get(droneId);
    if (marker) {
      marker.remove();
      this.markers.delete(droneId);
      this.iconKeys.delete(droneId);
    }
  }

  private forgetVanished(live: Set<string>): void {
    for (const [id, marker] of this.markers) {
      if (!live.has(id)) {
        marker.remove();
        this.markers.delete(id);
        this.iconKeys.delete(id);
      }
    }
    for (const id of this.bearings.keys()) {
      if (!live.has(id)) {
        this.bearings.delete(id);
        this.lastPositions.delete(id);
      }
    }
  }

  private renderHeatmap(cells: HeatCell[], peak: number, cellDegrees: number): void {
    if (!this.map) {
      return;
    }
    this.heatLayer?.remove();
    this.heatLayer = undefined;
    if (cells.length === 0 || peak <= 0) {
      return;
    }
    const half = cellDegrees / 2;
    const rectangles = cells.map((cell) => {
      const bounds: L.LatLngBoundsExpression = [
        [cell.latitude - half, cell.longitude - half],
        [cell.latitude + half, cell.longitude + half],
      ];
      return L.rectangle(bounds, heatStyle(cell.samples, peak)).bindTooltip(
        `${cell.samples} samples · ${cell.drones} drones`,
      );
    });
    this.heatLayer = L.layerGroup(rectangles).addTo(this.map);
  }

  private renderPredictions(predictions: DronePrediction[]): void {
    if (!this.map) {
      return;
    }
    const seen = new Set<string>();
    for (const prediction of predictions) {
      seen.add(prediction.droneId);
      const points = prediction.path.map(([latitude, longitude]) => L.latLng(latitude, longitude));
      const existing = this.predictionLines.get(prediction.droneId);
      if (existing) {
        existing.setLatLngs(points);
        continue;
      }
      this.predictionLines.set(
        prediction.droneId,
        L.polyline(points, predictionStyle).addTo(this.map),
      );
    }
    for (const [droneId, line] of this.predictionLines) {
      if (!seen.has(droneId)) {
        line.remove();
        this.predictionLines.delete(droneId);
      }
    }
  }

  private renderCustomZones(zones: ZoneFeatureCollection): void {
    if (!this.map) {
      return;
    }
    this.customZonesLayer?.remove();
    this.customZonesLayer = L.geoJSON(zones, {
      pane: customZonePane,
      style: customZoneStyle,
      onEachFeature: (feature, layer) => {
        layer.bindTooltip(String(feature.properties?.['name'] ?? 'custom zone'));
      },
    }).addTo(this.map);
    this.styleCustomZones(this.telemetry.alarmedZoneIds());
  }

  private renderZonePreview(drawing: boolean, vertices: ZoneVertex[]): void {
    if (!this.map) {
      return;
    }
    this.zonePreviewLayer?.remove();
    this.zonePreviewLayer = undefined;
    this.mapHost().nativeElement.style.cursor = drawing ? 'crosshair' : '';
    if (!drawing || vertices.length === 0) {
      return;
    }
    const points = vertices.map((vertex) => L.latLng(vertex.latitude, vertex.longitude));
    this.zonePreviewLayer = L.polygon(points, zonePreviewStyle).addTo(this.map);
  }

  private renderTrack(track: DroneSample[]): void {
    if (!this.map) {
      return;
    }
    this.trackLayer?.remove();
    this.trackLayer = undefined;
    if (track.length < 2) {
      return;
    }
    const points = track.map((sample) => L.latLng(sample.Latitude, sample.Longitude));
    this.trackLayer = L.polyline(points, trackStyle).addTo(this.map);
  }

  private renderPlaybackMarker(point: DroneSample | null): void {
    if (!this.map) {
      return;
    }
    if (!point) {
      this.playbackMarker?.remove();
      this.playbackMarker = undefined;
      return;
    }
    const position = L.latLng(point.Latitude, point.Longitude);
    if (this.playbackMarker) {
      this.playbackMarker.setLatLng(position);
      return;
    }
    this.playbackMarker = L.circleMarker(position, playbackMarkerStyle).addTo(this.map);
  }

  private updateBearing(droneId: string, position: L.LatLng): number {
    const previous = this.lastPositions.get(droneId);
    this.lastPositions.set(droneId, position);
    if (previous) {
      const nextBearing = bearingDegrees(previous, position);
      if (nextBearing !== null) {
        this.bearings.set(droneId, nextBearing);
        return nextBearing;
      }
    }
    return this.bearings.get(droneId) ?? 0;
  }
}

function droneIcon(color: string, bearing: number): L.DivIcon {
  return L.divIcon({
    className: 'drone-icon-wrap',
    html: `<div class="drone-icon" style="transform: rotate(${Math.round(bearing)}deg); border-bottom-color: ${color}"></div>`,
    iconSize: [18, 18],
    iconAnchor: [9, 9],
  });
}

function bearingDegrees(from: L.LatLng, to: L.LatLng): number | null {
  const deltaLat = to.lat - from.lat;
  const deltaLon = (to.lng - from.lng) * Math.cos((to.lat * Math.PI) / 180);
  if (deltaLat === 0 && deltaLon === 0) {
    return null;
  }
  return (Math.atan2(deltaLon, deltaLat) * 180) / Math.PI;
}

function heatStyle(samples: number, peak: number): L.PathOptions {
  const intensity = Math.sqrt(samples / peak);
  const hue = 60 - 60 * intensity;
  return {
    stroke: false,
    fillColor: `hsl(${hue}, 90%, 55%)`,
    fillOpacity: 0.2 + 0.5 * intensity,
  };
}

function confidenceColor(level: number): string {
  if (level > 70) {
    return '#2f9e44';
  }
  if (level > 40) {
    return '#e8890c';
  }
  return '#d64545';
}

function tooltipFor(drone: DroneSample, prediction?: DronePrediction): string {
  const kind = drone.Class || 'unknown';
  const spoof = drone.Anomaly ? ' · ⚠ spoof?' : '';
  const base = `${drone.DroneID} · ${kind}${spoof} · ${Math.round(drone.Altitude)} m · track ${drone.Confidence}% · q${drone.Quality}`;
  if (!prediction?.eta) {
    return base;
  }
  return `${base}<br>ETA ${prediction.eta.zoneName}: ~${prediction.eta.seconds}s`;
}
