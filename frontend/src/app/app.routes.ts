import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    loadComponent: () =>
      import('./features/dashboard/dashboard.component').then((m) => m.DashboardComponent),
  },
  {
    path: 'fleet',
    loadComponent: () => import('./features/fleet/fleet.component').then((m) => m.FleetComponent),
  },
  { path: '**', redirectTo: '' },
];
