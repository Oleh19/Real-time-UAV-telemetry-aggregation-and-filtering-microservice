import { DatePipe, DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { DroneSample } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';

type FilterMode = 'all' | 'hostile' | 'anomaly' | 'friendly';
type SortKey = 'DroneID' | 'Altitude' | 'Speed' | 'Confidence' | 'Quality';
type SortDirection = 'asc' | 'desc';

interface DroneRow extends DroneSample {
  confidenceClass: string;
}

@Component({
  selector: 'app-drone-list',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, DecimalPipe],
  templateUrl: './drone-list.component.html',
  styleUrl: './drone-list.component.scss',
})
export class DroneListComponent {
  private readonly telemetry = inject(TelemetryService);

  protected readonly query = signal('');
  protected readonly filter = signal<FilterMode>('all');
  protected readonly sortKey = signal<SortKey>('DroneID');
  protected readonly sortDirection = signal<SortDirection>('asc');

  protected readonly filters: readonly { id: FilterMode; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'hostile', label: 'Hostile' },
    { id: 'anomaly', label: 'Spoof?' },
    { id: 'friendly', label: 'Friendly' },
  ];

  protected readonly rows = computed<DroneRow[]>(() => {
    const query = this.query().trim().toLowerCase();
    const filter = this.filter();
    const key = this.sortKey();
    const direction = this.sortDirection() === 'asc' ? 1 : -1;
    return this.telemetry
      .drones()
      .filter((drone) => matchesFilter(drone, filter))
      .filter((drone) => matchesQuery(drone, query))
      .map((drone) => ({ ...drone, confidenceClass: confidenceClass(drone.Confidence) }))
      .sort((a, b) => compareBy(a, b, key) * direction);
  });

  protected readonly total = computed(() => this.telemetry.drones().length);

  protected setQuery(value: string): void {
    this.query.set(value);
  }

  protected setFilter(mode: FilterMode): void {
    this.filter.set(mode);
  }

  protected sortBy(key: SortKey): void {
    if (this.sortKey() === key) {
      this.sortDirection.update((current) => (current === 'asc' ? 'desc' : 'asc'));
      return;
    }
    this.sortKey.set(key);
    this.sortDirection.set(key === 'DroneID' ? 'asc' : 'desc');
  }

  protected ariaSort(key: SortKey): 'ascending' | 'descending' | 'none' {
    if (this.sortKey() !== key) {
      return 'none';
    }
    return this.sortDirection() === 'asc' ? 'ascending' : 'descending';
  }
}

function matchesFilter(drone: DroneSample, mode: FilterMode): boolean {
  switch (mode) {
    case 'hostile':
      return !drone.Friendly;
    case 'anomaly':
      return drone.Anomaly;
    case 'friendly':
      return drone.Friendly;
    default:
      return true;
  }
}

function matchesQuery(drone: DroneSample, query: string): boolean {
  if (query === '') {
    return true;
  }
  return (
    drone.DroneID.toLowerCase().includes(query) || (drone.Class ?? '').toLowerCase().includes(query)
  );
}

function compareBy(a: DroneSample, b: DroneSample, key: SortKey): number {
  if (key === 'DroneID') {
    return a.DroneID.localeCompare(b.DroneID);
  }
  return a[key] - b[key];
}

function confidenceClass(level: number): string {
  if (level > 70) {
    return 'confidence confidence-high';
  }
  if (level > 40) {
    return 'confidence confidence-medium';
  }
  return 'confidence confidence-low';
}
