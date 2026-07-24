import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, inject } from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import { catchError, filter, of, switchMap, timer } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { BreachRecord } from '../../../core/models/telemetry';

const pollIntervalMs = 5000;
const feedLimit = 50;

@Injectable({ providedIn: 'root' })
export class BreachFeedService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);

  readonly breaches = toSignal(
    timer(0, pollIntervalMs).pipe(
      switchMap(() =>
        this.http
          .get<BreachRecord[]>(`${environment.apiBaseUrl}/breaches`, {
            params: { limit: feedLimit },
          })
          .pipe(catchError(() => of(null))),
      ),
      filter((records): records is BreachRecord[] => records !== null),
    ),
    { initialValue: [] as BreachRecord[] },
  );

  acknowledge(id: number): void {
    this.updateStatus(id, 'ack');
  }

  resolve(id: number): void {
    this.updateStatus(id, 'resolve');
  }

  private updateStatus(id: number, action: 'ack' | 'resolve'): void {
    this.http
      .post(`${environment.apiBaseUrl}/breaches/${id}/${action}`, null)
      .pipe(
        catchError(() => of(null)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe();
  }
}
