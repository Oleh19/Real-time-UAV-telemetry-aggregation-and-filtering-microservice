import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { catchError, of, retry } from 'rxjs';

import { environment } from '../../environments/environment';
import { EVENT_SOURCE_FACTORY } from './event-source';
import {
  DroneSample,
  EMPTY_ZONES,
  IngestStats,
  OblastAlert,
  ZoneFeatureCollection,
} from './models/telemetry';

interface TelemetryStreamEvent {
  drones: DroneSample[];
  stats: IngestStats;
}

@Injectable({ providedIn: 'root' })
export class TelemetryService {
  private readonly http = inject(HttpClient);
  private readonly createEventSource = inject(EVENT_SOURCE_FACTORY);

  private readonly dronesState = signal<DroneSample[]>([]);
  private readonly statsState = signal<IngestStats | null>(null);
  private readonly alertsState = signal<OblastAlert[]>([]);
  private readonly connectedState = signal(false);

  readonly drones = this.dronesState.asReadonly();
  readonly stats = this.statsState.asReadonly();
  readonly alerts = this.alertsState.asReadonly();
  readonly connected = this.connectedState.asReadonly();
  readonly alarmedOblastIds = computed(
    () =>
      new Set(
        this.alerts()
          .filter((alert) => alert.alarmed)
          .map((alert) => alert.id),
      ),
  );

  readonly zones = toSignal(
    this.http.get<ZoneFeatureCollection>(`${environment.apiBaseUrl}/zones`).pipe(
      retry({ count: 30, delay: 2000 }),
      catchError(() => of(EMPTY_ZONES)),
    ),
    { initialValue: EMPTY_ZONES },
  );

  constructor() {
    const telemetry = this.createEventSource(`${environment.apiBaseUrl}/stream`);
    telemetry.onmessage = (event) => {
      const payload = JSON.parse(event.data) as TelemetryStreamEvent;
      this.dronesState.set(payload.drones ?? []);
      this.statsState.set(payload.stats ?? null);
      this.connectedState.set(true);
    };
    telemetry.onopen = () => this.connectedState.set(true);
    telemetry.onerror = () => this.connectedState.set(false);

    const alerts = this.createEventSource(`${environment.apiBaseUrl}/alert-stream`);
    alerts.onmessage = (event) => {
      this.alertsState.set(JSON.parse(event.data) as OblastAlert[]);
    };

    inject(DestroyRef).onDestroy(() => {
      telemetry.close();
      alerts.close();
    });
  }
}
