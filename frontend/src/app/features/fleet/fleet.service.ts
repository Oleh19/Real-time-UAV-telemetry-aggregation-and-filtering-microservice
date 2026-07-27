import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject } from '@angular/core';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import { catchError, filter, of, switchMap, timer } from 'rxjs';

import { environment } from '../../../environments/environment';
import {
  AddDroneRequest,
  CreateMissionRequest,
  EMPTY_FLEET,
  FleetSnapshot,
  MissionAction,
} from './fleet.models';

const pollIntervalMs = 1000;

@Injectable()
export class FleetService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);
  private readonly base = `${environment.apiBaseUrl}/fleet`;

  private readonly snapshot = toSignal(
    timer(0, pollIntervalMs).pipe(
      switchMap(() => this.http.get<FleetSnapshot>(this.base).pipe(catchError(() => of(null)))),
      filter((snap): snap is FleetSnapshot => snap !== null),
    ),
    { initialValue: EMPTY_FLEET },
  );

  readonly drones = computed(() => this.snapshot().drones);
  readonly missions = computed(() => this.snapshot().missions);

  addDrone(request: AddDroneRequest): void {
    this.fire(this.http.post(`${this.base}/drones`, request));
  }

  removeDrone(id: string): void {
    this.fire(this.http.delete(`${this.base}/drones/${id}`));
  }

  recall(id: string): void {
    this.fire(this.http.post(`${this.base}/drones/${id}/recall`, null));
  }

  createMission(request: CreateMissionRequest): void {
    this.fire(this.http.post(`${this.base}/missions`, request));
  }

  deleteMission(id: string): void {
    this.fire(this.http.delete(`${this.base}/missions/${id}`));
  }

  missionAction(id: string, action: MissionAction): void {
    this.fire(this.http.post(`${this.base}/missions/${id}/${action}`, null));
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
