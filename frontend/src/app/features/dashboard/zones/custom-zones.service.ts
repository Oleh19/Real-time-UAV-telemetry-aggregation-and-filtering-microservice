import { HttpClient } from '@angular/common/http';
import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { catchError, of } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { EMPTY_ZONES, ZoneFeatureCollection } from '../../../core/models/telemetry';

export interface ZoneVertex {
  latitude: number;
  longitude: number;
}

const minPolygonPoints = 3;

@Injectable({ providedIn: 'root' })
export class CustomZonesService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);

  private readonly zonesState = signal<ZoneFeatureCollection>(EMPTY_ZONES);
  private readonly drawingState = signal(false);
  private readonly verticesState = signal<ZoneVertex[]>([]);
  private readonly savingState = signal(false);

  readonly zones = this.zonesState.asReadonly();
  readonly drawing = this.drawingState.asReadonly();
  readonly vertices = this.verticesState.asReadonly();
  readonly saving = this.savingState.asReadonly();
  readonly canSave = computed(
    () => this.verticesState().length >= minPolygonPoints && !this.savingState(),
  );

  constructor() {
    this.reload();
  }

  reload(): void {
    this.http
      .get<ZoneFeatureCollection>(`${environment.apiBaseUrl}/custom-zones`)
      .pipe(
        catchError(() => of(EMPTY_ZONES)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe((zones) => this.zonesState.set(zones));
  }

  startDrawing(): void {
    this.drawingState.set(true);
    this.verticesState.set([]);
  }

  cancelDrawing(): void {
    this.drawingState.set(false);
    this.verticesState.set([]);
  }

  addVertex(vertex: ZoneVertex): void {
    if (!this.drawingState()) {
      return;
    }
    this.verticesState.update((vertices) => [...vertices, vertex]);
  }

  undoVertex(): void {
    this.verticesState.update((vertices) => vertices.slice(0, -1));
  }

  save(name: string): void {
    if (!this.canSave()) {
      return;
    }
    this.savingState.set(true);
    const coordinates = this.verticesState().map(
      (vertex) => [vertex.longitude, vertex.latitude] as [number, number],
    );
    this.http
      .post(`${environment.apiBaseUrl}/custom-zones`, { name, coordinates })
      .pipe(
        catchError(() => of(null)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(() => {
        this.savingState.set(false);
        this.cancelDrawing();
        this.reload();
      });
  }

  remove(id: number): void {
    this.http
      .delete(`${environment.apiBaseUrl}/custom-zones/${id}`)
      .pipe(
        catchError(() => of(null)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(() => this.reload());
  }
}
