import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { catchError, filter, of, switchMap, timer } from 'rxjs';

import { environment } from '../../../../environments/environment';

export type StationStatus = 'online' | 'stale' | 'offline';

export interface StationRecord {
  id: string;
  status: StationStatus;
  lastSeen: string;
  samples: number;
  ratePerSecond: number;
}

const pollIntervalMs = 5000;

@Injectable({ providedIn: 'root' })
export class StationFeedService {
  private readonly http = inject(HttpClient);

  readonly stations = toSignal(
    timer(0, pollIntervalMs).pipe(
      switchMap(() =>
        this.http
          .get<StationRecord[]>(`${environment.apiBaseUrl}/stations`)
          .pipe(catchError(() => of(null))),
      ),
      filter((records): records is StationRecord[] => records !== null),
    ),
    { initialValue: [] as StationRecord[] },
  );
}
