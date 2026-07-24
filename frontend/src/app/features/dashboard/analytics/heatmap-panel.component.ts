import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { HEATMAP_WINDOWS, HeatmapService } from './heatmap.service';

@Component({
  selector: 'app-heatmap-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './heatmap-panel.component.html',
  styleUrl: './heatmap-panel.component.scss',
})
export class HeatmapPanelComponent {
  protected readonly heatmap = inject(HeatmapService);
  protected readonly windows = HEATMAP_WINDOWS;
  protected readonly selected = signal(0);
  protected readonly activeWindow = computed(() => this.windows[this.selected()]);

  protected toggle(): void {
    const window = this.activeWindow();
    this.heatmap.toggle(window.hours, window.cell);
  }

  protected select(index: number): void {
    this.selected.set(index);
    const window = this.activeWindow();
    this.heatmap.refresh(window.hours, window.cell);
  }

  protected playPause(): void {
    if (this.heatmap.playing()) {
      this.heatmap.pause();
      return;
    }
    const window = this.activeWindow();
    this.heatmap.playTimelapse(window.hours, window.cell);
  }

  protected scrub(value: string): void {
    this.heatmap.setFrame(Number(value));
  }
}
