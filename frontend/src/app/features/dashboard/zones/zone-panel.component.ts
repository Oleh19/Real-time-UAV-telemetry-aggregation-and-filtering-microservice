import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { CustomZonesService } from './custom-zones.service';

@Component({
  selector: 'app-zone-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './zone-panel.component.html',
  styleUrl: './zone-panel.component.scss',
})
export class ZonePanelComponent {
  protected readonly zones = inject(CustomZonesService);
  protected readonly zoneName = signal('');

  protected onNameInput(event: Event): void {
    this.zoneName.set((event.target as HTMLInputElement).value);
  }

  protected save(): void {
    const name = this.zoneName().trim();
    if (!name) {
      return;
    }
    this.zones.save(name);
    this.zoneName.set('');
  }

  protected cancel(): void {
    this.zones.cancelDrawing();
    this.zoneName.set('');
  }
}
