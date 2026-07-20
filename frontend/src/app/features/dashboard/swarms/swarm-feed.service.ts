import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { catchError, filter, of, switchMap, timer } from 'rxjs';

import { environment } from '../../../../environments/environment';

export interface SwarmRecord {
  id: string;
  droneIds: string[];
  latitude: number;
  longitude: number;
  detectedAt: string;
}

const pollIntervalMs = 5000;

@Injectable({ providedIn: 'root' })
export class SwarmFeedService {
  private readonly http = inject(HttpClient);

  readonly swarms = toSignal(
    timer(0, pollIntervalMs).pipe(
      switchMap(() =>
        this.http
          .get<SwarmRecord[]>(`${environment.apiBaseUrl}/swarms`)
          .pipe(catchError(() => of(null))),
      ),
      filter((records): records is SwarmRecord[] => records !== null),
    ),
    { initialValue: [] as SwarmRecord[] },
  );
}
