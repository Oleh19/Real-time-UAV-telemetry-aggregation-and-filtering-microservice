import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, inject } from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import { catchError, filter, of, switchMap, timer } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { ReplayStatus } from '../../../core/models/telemetry';

const pollIntervalMs = 2000;

export interface StartReplayRequest {
  from: string;
  to: string;
  speed: number;
  droneId?: string;
}

@Injectable({ providedIn: 'root' })
export class ReplayService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);
  private readonly base = `${environment.apiBaseUrl}/replays`;

  readonly replays = toSignal(
    timer(0, pollIntervalMs).pipe(
      switchMap(() => this.http.get<ReplayStatus[]>(this.base).pipe(catchError(() => of(null)))),
      filter((records): records is ReplayStatus[] => records !== null),
    ),
    { initialValue: [] as ReplayStatus[] },
  );

  start(request: StartReplayRequest): void {
    this.fire(this.http.post(this.base, request));
  }

  pause(id: string): void {
    this.fire(this.http.patch(`${this.base}/${id}`, { paused: true }));
  }

  resume(id: string): void {
    this.fire(this.http.patch(`${this.base}/${id}`, { paused: false }));
  }

  setSpeed(id: string, speed: number): void {
    this.fire(this.http.patch(`${this.base}/${id}`, { speed }));
  }

  cancel(id: string): void {
    this.fire(this.http.delete(`${this.base}/${id}`));
  }

  private fire(request: ReturnType<HttpClient['get']>): void {
    request
      .pipe(
        catchError(() => of(null)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe();
  }
}
