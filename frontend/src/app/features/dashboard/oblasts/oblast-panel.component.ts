import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { TelemetryService } from '../../../core/telemetry.service';

@Component({
  selector: 'app-oblast-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './oblast-panel.component.html',
  styleUrl: './oblast-panel.component.scss',
})
export class OblastPanelComponent {
  protected readonly telemetry = inject(TelemetryService);

  protected readonly alarmedCount = computed(
    () => this.telemetry.alerts().filter((alert) => alert.alarmed).length,
  );
}
