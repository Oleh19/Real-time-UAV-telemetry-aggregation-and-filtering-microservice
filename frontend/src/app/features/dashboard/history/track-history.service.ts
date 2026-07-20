import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { catchError, of } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { DroneSample } from '../../../core/models/telemetry';

@Injectable({ providedIn: 'root' })
export class TrackHistoryService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);

  private readonly droneIdState = signal<string | null>(null);
  private readonly trackState = signal<DroneSample[]>([]);
  private readonly loadingState = signal(false);
  private readonly playingState = signal(false);
  private readonly indexState = signal(0);

  readonly droneId = this.droneIdState.asReadonly();
  readonly track = this.trackState.asReadonly();
  readonly loading = this.loadingState.asReadonly();
  readonly playing = this.playingState.asReadonly();
  readonly index = this.indexState.asReadonly();
  readonly lastIndex = computed(() => Math.max(0, this.trackState().length - 1));
  readonly currentPoint = computed<DroneSample | null>(
    () => this.trackState()[this.indexState()] ?? null,
  );

  load(droneId: string): void {
    this.droneIdState.set(droneId);
    this.trackState.set([]);
    this.indexState.set(0);
    this.playingState.set(false);
    this.loadingState.set(true);
    this.http
      .get<DroneSample[]>(`${environment.apiBaseUrl}/history`, { params: { drone_id: droneId } })
      .pipe(
        catchError(() => of([] as DroneSample[])),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe((samples) => {
        if (this.droneIdState() !== droneId) {
          return;
        }
        const track = samples ?? [];
        this.trackState.set(track);
        this.indexState.set(Math.max(0, track.length - 1));
        this.loadingState.set(false);
      });
  }

  clear(): void {
    this.droneIdState.set(null);
    this.trackState.set([]);
    this.indexState.set(0);
    this.playingState.set(false);
    this.loadingState.set(false);
  }

  seek(index: number): void {
    this.playingState.set(false);
    this.indexState.set(Math.min(Math.max(index, 0), this.lastIndex()));
  }

  togglePlayback(): void {
    if (this.trackState().length < 2) {
      return;
    }
    if (!this.playingState() && this.indexState() >= this.lastIndex()) {
      this.indexState.set(0);
    }
    this.playingState.update((playing) => !playing);
  }

  advance(): void {
    if (!this.playingState()) {
      return;
    }
    const next = this.indexState() + 1;
    this.indexState.set(Math.min(next, this.lastIndex()));
    if (next >= this.lastIndex()) {
      this.playingState.set(false);
    }
  }
}
