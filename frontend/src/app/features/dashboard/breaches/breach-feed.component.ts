import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

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
}
