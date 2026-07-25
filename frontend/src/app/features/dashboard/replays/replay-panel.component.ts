import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { ReplayService } from './replay.service';

@Component({
  selector: 'app-replay-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe],
  templateUrl: './replay-panel.component.html',
  styleUrl: './replay-panel.component.scss',
})
export class ReplayPanelComponent {
  protected readonly replays = inject(ReplayService);
  protected readonly from = signal('');
  protected readonly to = signal('');
  protected readonly speed = signal(10);
  protected readonly droneId = signal('');

  protected start(): void {
    const now = Date.now();
    const from = this.from()
      ? new Date(this.from()).toISOString()
      : new Date(now - 15 * 60 * 1000).toISOString();
    const to = this.to() ? new Date(this.to()).toISOString() : new Date(now).toISOString();
    const speed = Number(this.speed()) || 10;
    const droneId = this.droneId().trim();
    this.replays.start({ from, to, speed, droneId: droneId === '' ? undefined : droneId });
  }

  protected changeSpeed(id: string, value: string): void {
    const speed = Number(value);
    if (speed > 0) {
      this.replays.setSpeed(id, speed);
    }
  }
}
