import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  afterNextRender,
  effect,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import * as L from 'leaflet';

import { FleetDrone, FleetMission, FleetWaypoint } from './fleet.models';

const MAP_CENTER: L.LatLngExpression = [48.7, 31.2];
const MAP_ZOOM = 6;

const statusColors: Record<string, string> = {
  idle: '#8b98a9',
  'en-route': '#4dabf7',
  holding: '#e8890c',
  returning: '#e8890c',
  charging: '#2f9e44',
  maintenance: '#d64545',
  offline: '#5a6472',
};

@Component({
  selector: 'app-fleet-map',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<div class="map" #mapHost></div>',
  styles: ':host { display: block; height: 100%; } .map { height: 100%; }',
})
export class FleetMapComponent {
  readonly drones = input<FleetDrone[]>([]);
  readonly missions = input<FleetMission[]>([]);
  readonly draft = input<FleetWaypoint[]>([]);
  readonly mapClick = output<FleetWaypoint>();

  private readonly mapHost = viewChild.required<ElementRef<HTMLDivElement>>('mapHost');
  private readonly ready = signal(false);
  private map?: L.Map;
  private readonly droneLayer = L.layerGroup();
  private readonly missionLayer = L.layerGroup();
  private readonly draftLayer = L.layerGroup();

  constructor() {
    afterNextRender(() => this.initMap());
    effect(() => {
      const drones = this.drones();
      if (this.ready()) {
        this.renderDrones(drones);
      }
    });
    effect(() => {
      const missions = this.missions();
      if (this.ready()) {
        this.renderMissions(missions);
      }
    });
    effect(() => {
      const draft = this.draft();
      if (this.ready()) {
        this.renderDraft(draft);
      }
    });
  }

  private initMap(): void {
    this.map = L.map(this.mapHost().nativeElement, { center: MAP_CENTER, zoom: MAP_ZOOM });
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; OpenStreetMap contributors',
    }).addTo(this.map);
    this.missionLayer.addTo(this.map);
    this.draftLayer.addTo(this.map);
    this.droneLayer.addTo(this.map);
    this.map.on('click', (event: L.LeafletMouseEvent) => {
      this.mapClick.emit({ latitude: event.latlng.lat, longitude: event.latlng.lng });
    });
    this.ready.set(true);
  }

  private renderDrones(drones: FleetDrone[]): void {
    this.droneLayer.clearLayers();
    for (const drone of drones) {
      L.circleMarker([drone.base.latitude, drone.base.longitude], {
        radius: 4,
        color: '#8b98a9',
        weight: 1,
        fillOpacity: 0,
      }).addTo(this.droneLayer);
      L.circleMarker([drone.latitude, drone.longitude], {
        radius: 7,
        color: statusColors[drone.status] ?? '#8b98a9',
        weight: 2,
        fillColor: statusColors[drone.status] ?? '#8b98a9',
        fillOpacity: 0.6,
      })
        .bindTooltip(`${drone.id} · ${drone.status} · ${Math.round(drone.battery)}%`)
        .addTo(this.droneLayer);
    }
  }

  private renderMissions(missions: FleetMission[]): void {
    this.missionLayer.clearLayers();
    for (const mission of missions) {
      if (mission.state !== 'active' && mission.state !== 'paused') {
        continue;
      }
      const points = mission.waypoints.map((w) => L.latLng(w.latitude, w.longitude));
      if (points.length > 0) {
        L.polyline(points, { color: '#4dabf7', weight: 2, dashArray: '4 6' }).addTo(
          this.missionLayer,
        );
      }
    }
  }

  private renderDraft(draft: FleetWaypoint[]): void {
    this.draftLayer.clearLayers();
    const points = draft.map((w) => L.latLng(w.latitude, w.longitude));
    if (points.length > 1) {
      L.polyline(points, { color: '#e8890c', weight: 2, dashArray: '6 4' }).addTo(this.draftLayer);
    }
    points.forEach((p, i) =>
      L.circleMarker(p, { radius: 5, color: '#e8890c', weight: 2, fillOpacity: 0.5 })
        .bindTooltip(`waypoint ${i + 1}`)
        .addTo(this.draftLayer),
    );
  }
}
