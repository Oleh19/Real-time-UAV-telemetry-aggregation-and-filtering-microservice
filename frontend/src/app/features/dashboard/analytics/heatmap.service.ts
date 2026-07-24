import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { catchError, of } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { HeatCell } from '../../../core/models/telemetry';

export const HEATMAP_WINDOWS = [
  { label: 'Last hour', hours: 1, cell: 0.1 },
  { label: 'Last 6 hours', hours: 6, cell: 0.15 },
  { label: 'Last 24 hours', hours: 24, cell: 0.25 },
] as const;

@Injectable({ providedIn: 'root' })
export class HeatmapService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);

  private readonly cellsState = signal<HeatCell[]>([]);
  private readonly visibleState = signal(false);
  private readonly loadingState = signal(false);
  private readonly cellDegreesState = signal<number>(HEATMAP_WINDOWS[0].cell);

  readonly cells = this.cellsState.asReadonly();
  readonly visible = this.visibleState.asReadonly();
  readonly loading = this.loadingState.asReadonly();
  readonly cellDegrees = this.cellDegreesState.asReadonly();
  readonly peakSamples = computed(() =>
    this.cellsState().reduce((max, cell) => Math.max(max, cell.samples), 0),
  );

  toggle(windowHours: number, cellDegrees: number): void {
    if (this.visibleState()) {
      this.visibleState.set(false);
      this.cellsState.set([]);
      return;
    }
    this.visibleState.set(true);
    this.refresh(windowHours, cellDegrees);
  }

  refresh(windowHours: number, cellDegrees: number): void {
    if (!this.visibleState()) {
      return;
    }
    this.loadingState.set(true);
    this.cellDegreesState.set(cellDegrees);
    const to = new Date();
    const from = new Date(to.getTime() - windowHours * 3600 * 1000);
    this.http
      .get<HeatCell[]>(`${environment.apiBaseUrl}/analytics/heatmap`, {
        params: {
          from: from.toISOString(),
          to: to.toISOString(),
          cell: cellDegrees,
        },
      })
      .pipe(
        catchError(() => of([] as HeatCell[])),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe((cells) => {
        this.cellsState.set(cells);
        this.loadingState.set(false);
      });
  }
}
