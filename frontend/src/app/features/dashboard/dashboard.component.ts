import { ChangeDetectionStrategy, Component } from '@angular/core';

import { DroneListComponent } from './drones/drone-list.component';
import { DroneMapComponent } from './map/drone-map.component';
import { MetricsPanelComponent } from './metrics/metrics-panel.component';
import { OblastPanelComponent } from './oblasts/oblast-panel.component';

@Component({
  selector: 'app-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DroneMapComponent, MetricsPanelComponent, OblastPanelComponent, DroneListComponent],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {}
