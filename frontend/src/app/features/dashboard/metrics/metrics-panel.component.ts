import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { TelemetryService } from '../../../core/telemetry.service';

@Component({
  selector: 'app-metrics-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './metrics-panel.component.html',
  styleUrl: './metrics-panel.component.scss',
})
export class MetricsPanelComponent {
  protected readonly telemetry = inject(TelemetryService);
}
