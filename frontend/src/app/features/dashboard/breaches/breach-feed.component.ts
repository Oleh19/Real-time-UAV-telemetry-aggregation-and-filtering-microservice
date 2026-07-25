import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { environment } from '../../../../environments/environment';
import { BreachFeedService } from './breach-feed.service';

@Component({
  selector: 'app-breach-feed',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe],
  templateUrl: './breach-feed.component.html',
  styleUrl: './breach-feed.component.scss',
})
export class BreachFeedComponent {
  protected readonly feed = inject(BreachFeedService);
  protected readonly csvUrl = `${environment.apiBaseUrl}/breaches/export?format=csv`;
  protected readonly geojsonUrl = `${environment.apiBaseUrl}/breaches/export?format=geojson`;
}
