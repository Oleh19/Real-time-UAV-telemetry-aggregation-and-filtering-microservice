import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, effect, inject } from '@angular/core';

import { TrackHistoryService } from './track-history.service';

const playbackTickMs = 150;

@Component({
  selector: 'app-playback-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe],
  templateUrl: './playback-panel.component.html',
  styleUrl: './playback-panel.component.scss',
})
export class PlaybackPanelComponent {
  protected readonly history = inject(TrackHistoryService);

  constructor() {
    effect((onCleanup) => {
      if (!this.history.playing()) {
        return;
      }
      const timer = setInterval(() => this.history.advance(), playbackTickMs);
      onCleanup(() => clearInterval(timer));
    });
  }

  protected close(): void {
    this.history.clear();
  }

  protected onSeek(event: Event): void {
    this.history.seek(Number((event.target as HTMLInputElement).value));
  }
}
