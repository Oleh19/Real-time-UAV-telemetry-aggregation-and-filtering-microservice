import { ChangeDetectionStrategy, Component } from '@angular/core';

import { BreachFeedComponent } from './breaches/breach-feed.component';
import { DroneListComponent } from './drones/drone-list.component';
import { PlaybackPanelComponent } from './history/playback-panel.component';
import { DroneMapComponent } from './map/drone-map.component';
import { MetricsPanelComponent } from './metrics/metrics-panel.component';
import { OblastPanelComponent } from './oblasts/oblast-panel.component';
import { SwarmPanelComponent } from './swarms/swarm-panel.component';
import { ZonePanelComponent } from './zones/zone-panel.component';

@Component({
  selector: 'app-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    DroneMapComponent,
    MetricsPanelComponent,
    OblastPanelComponent,
    DroneListComponent,
    PlaybackPanelComponent,
    BreachFeedComponent,
    ZonePanelComponent,
    SwarmPanelComponent,
  ],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {}
