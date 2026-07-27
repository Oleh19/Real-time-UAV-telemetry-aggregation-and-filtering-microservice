import { DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { FleetMapComponent } from './fleet-map.component';
import { FleetMission, FleetWaypoint, MissionAction } from './fleet.models';
import { FleetService } from './fleet.service';

@Component({
  selector: 'app-fleet',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FleetMapComponent, DecimalPipe],
  templateUrl: './fleet.component.html',
  styleUrl: './fleet.component.scss',
})
export class FleetComponent {
  protected readonly fleet = inject(FleetService);

  protected readonly draft = signal<FleetWaypoint[]>([]);
  protected readonly selectedDroneId = signal('');
  protected readonly missionName = signal('');

  protected readonly newDroneId = signal('');
  protected readonly newDroneModel = signal('');

  protected readonly canCreate = computed(
    () =>
      this.selectedDroneId() !== '' && this.missionName().trim() !== '' && this.draft().length > 0,
  );

  protected onMapClick(waypoint: FleetWaypoint): void {
    if (this.selectedDroneId() === '') {
      return;
    }
    this.draft.update((points) => [...points, waypoint]);
  }

  protected undoWaypoint(): void {
    this.draft.update((points) => points.slice(0, -1));
  }

  protected clearDraft(): void {
    this.draft.set([]);
  }

  protected createMission(): void {
    if (!this.canCreate()) {
      return;
    }
    this.fleet.createMission({
      name: this.missionName().trim(),
      droneId: this.selectedDroneId(),
      waypoints: this.draft(),
    });
    this.missionName.set('');
    this.draft.set([]);
  }

  protected addDrone(): void {
    const id = this.newDroneId().trim();
    const model = this.newDroneModel().trim();
    if (id === '' || model === '') {
      return;
    }
    const drones = this.fleet.drones();
    const base = drones.length > 0 ? drones[0].base : { latitude: 50.45, longitude: 30.52 };
    this.fleet.addDrone({ id, model, base, firmware: '1.0.0' });
    this.newDroneId.set('');
    this.newDroneModel.set('');
  }

  protected action(mission: FleetMission, action: MissionAction): void {
    this.fleet.missionAction(mission.id, action);
  }

  protected batteryClass(level: number): string {
    if (level > 50) {
      return 'battery battery-high';
    }
    if (level > 20) {
      return 'battery battery-medium';
    }
    return 'battery battery-low';
  }
}
