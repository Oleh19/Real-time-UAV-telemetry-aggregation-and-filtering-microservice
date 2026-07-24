import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { ThreatService } from './threat.service';

const topN = 8;

@Component({
  selector: 'app-threat-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './threat-panel.component.html',
  styleUrl: './threat-panel.component.scss',
})
export class ThreatPanelComponent {
  private readonly threatService = inject(ThreatService);

  protected readonly threats = computed(() => this.threatService.threats().slice(0, topN));

  protected level(score: number): string {
    if (score >= 70) {
      return 'critical';
    }
    if (score >= 40) {
      return 'elevated';
    }
    return 'low';
  }

  protected eta(seconds: number | null): string {
    return seconds === null ? '—' : `~${seconds}s`;
  }
}
