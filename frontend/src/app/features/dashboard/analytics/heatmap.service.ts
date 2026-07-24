import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { catchError, forkJoin, of } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { HeatCell } from '../../../core/models/telemetry';

export const HEATMAP_WINDOWS = [
  { label: 'Last hour', hours: 1, cell: 0.1 },
  { label: 'Last 6 hours', hours: 6, cell: 0.15 },
  { label: 'Last 24 hours', hours: 24, cell: 0.25 },
] as const;

const TIMELAPSE_FRAMES = 12;
const FRAME_INTERVAL_MS = 800;

interface HeatFrame {
  label: string;
  cells: HeatCell[];
}

@Injectable({ providedIn: 'root' })
export class HeatmapService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);

  private readonly framesState = signal<HeatFrame[]>([]);
  private readonly frameIndexState = signal(0);
  private readonly visibleState = signal(false);
  private readonly loadingState = signal(false);
  private readonly playingState = signal(false);
  private readonly peakState = signal(0);
  private readonly cellDegreesState = signal<number>(HEATMAP_WINDOWS[0].cell);

  private timer: ReturnType<typeof setInterval> | null = null;

  readonly visible = this.visibleState.asReadonly();
  readonly loading = this.loadingState.asReadonly();
  readonly playing = this.playingState.asReadonly();
  readonly peakSamples = this.peakState.asReadonly();
  readonly cellDegrees = this.cellDegreesState.asReadonly();
  readonly frameCount = computed(() => this.framesState().length);
  readonly frameIndex = this.frameIndexState.asReadonly();
  readonly frameLabel = computed(() => this.framesState()[this.frameIndexState()]?.label ?? '');
  readonly cells = computed(() => this.framesState()[this.frameIndexState()]?.cells ?? []);

  constructor() {
    this.destroyRef.onDestroy(() => this.clearTimer());
  }

  toggle(windowHours: number, cellDegrees: number): void {
    if (this.visibleState()) {
      this.hide();
      return;
    }
    this.visibleState.set(true);
    this.refresh(windowHours, cellDegrees);
  }

  refresh(windowHours: number, cellDegrees: number): void {
    if (!this.visibleState()) {
      return;
    }
    this.pause();
    this.cellDegreesState.set(cellDegrees);
    this.loadingState.set(true);
    const to = new Date();
    const from = new Date(to.getTime() - windowHours * 3600 * 1000);
    this.fetch(from, to, cellDegrees).subscribe((cells) => {
      this.setFrames([{ label: 'aggregate', cells }]);
      this.loadingState.set(false);
    });
  }

  playTimelapse(windowHours: number, cellDegrees: number): void {
    this.visibleState.set(true);
    this.pause();
    this.cellDegreesState.set(cellDegrees);
    this.loadingState.set(true);
    const now = Date.now();
    const stepMs = (windowHours * 3600 * 1000) / TIMELAPSE_FRAMES;
    const requests = Array.from({ length: TIMELAPSE_FRAMES }, (_, i) => {
      const from = new Date(now - windowHours * 3600 * 1000 + i * stepMs);
      const to = new Date(from.getTime() + stepMs);
      return this.fetch(from, to, cellDegrees);
    });
    const starts = Array.from(
      { length: TIMELAPSE_FRAMES },
      (_, i) => new Date(now - windowHours * 3600 * 1000 + i * stepMs),
    );
    forkJoin(requests)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe((buckets) => {
        this.setFrames(buckets.map((cells, i) => ({ label: frameLabel(starts[i]), cells })));
        this.loadingState.set(false);
        if (this.frameCount() > 1) {
          this.start();
        }
      });
  }

  setFrame(index: number): void {
    this.pause();
    this.frameIndexState.set(index);
  }

  pause(): void {
    this.clearTimer();
    this.playingState.set(false);
  }

  hide(): void {
    this.pause();
    this.visibleState.set(false);
    this.framesState.set([]);
    this.frameIndexState.set(0);
    this.peakState.set(0);
  }

  private start(): void {
    this.playingState.set(true);
    this.timer = setInterval(() => {
      this.frameIndexState.update((i) => (i + 1) % Math.max(1, this.frameCount()));
    }, FRAME_INTERVAL_MS);
  }

  private setFrames(frames: HeatFrame[]): void {
    this.framesState.set(frames);
    this.frameIndexState.set(0);
    this.peakState.set(
      frames.reduce(
        (max, frame) => frame.cells.reduce((m, cell) => Math.max(m, cell.samples), max),
        0,
      ),
    );
  }

  private fetch(from: Date, to: Date, cellDegrees: number) {
    return this.http
      .get<HeatCell[]>(`${environment.apiBaseUrl}/analytics/heatmap`, {
        params: { from: from.toISOString(), to: to.toISOString(), cell: cellDegrees },
      })
      .pipe(catchError(() => of([] as HeatCell[])));
  }

  private clearTimer(): void {
    if (this.timer !== null) {
      clearInterval(this.timer);
      this.timer = null;
    }
  }
}

function frameLabel(start: Date): string {
  return start.toISOString().slice(11, 16);
}
