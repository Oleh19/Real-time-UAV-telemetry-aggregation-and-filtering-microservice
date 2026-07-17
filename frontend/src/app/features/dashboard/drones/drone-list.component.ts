import { DatePipe, DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { TelemetryService } from '../../../core/telemetry.service';

@Component({
  selector: 'app-drone-list',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, DecimalPipe],
  templateUrl: './drone-list.component.html',
  styleUrl: './drone-list.component.scss',
})
export class DroneListComponent {
  private readonly telemetry = inject(TelemetryService);

  protected readonly rows = computed(() =>
    this.telemetry
      .drones()
      .map((drone) => ({ ...drone, confidenceClass: confidenceClass(drone.Confidence) }))
      .sort((a, b) => a.DroneID.localeCompare(b.DroneID)),
  );
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
