import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { SwarmFeedService } from './swarm-feed.service';

@Component({
  selector: 'app-swarm-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './swarm-panel.component.html',
  styleUrl: './swarm-panel.component.scss',
})
export class SwarmPanelComponent {
  protected readonly feed = inject(SwarmFeedService);
}
