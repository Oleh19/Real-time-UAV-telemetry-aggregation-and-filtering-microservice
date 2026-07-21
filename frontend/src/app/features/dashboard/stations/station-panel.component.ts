import { DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { StationFeedService } from './station-feed.service';

@Component({
  selector: 'app-station-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe],
  templateUrl: './station-panel.component.html',
  styleUrl: './station-panel.component.scss',
})
export class StationPanelComponent {
  protected readonly feed = inject(StationFeedService);

  protected readonly silentCount = computed(
    () => this.feed.stations().filter((station) => station.status !== 'online').length,
  );
}
