import { HttpClient } from '@angular/common/http';
import { Injectable, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { catchError, of, switchMap, timer } from 'rxjs';

import { environment } from '../../environments/environment';
import { FriendlySquawk } from './models/telemetry';

const pollIntervalMs = 30000;

@Injectable({ providedIn: 'root' })
export class FriendlyRegistryService {
  private readonly http = inject(HttpClient);

  readonly squawks = toSignal(
    timer(0, pollIntervalMs).pipe(
      switchMap(() =>
        this.http
          .get<FriendlySquawk[]>(`${environment.apiBaseUrl}/friendly`)
          .pipe(catchError(() => of([] as FriendlySquawk[]))),
      ),
    ),
    { initialValue: [] as FriendlySquawk[] },
  );

  readonly codes = computed(() => new Set(this.squawks().map((squawk) => squawk.code)));
}
