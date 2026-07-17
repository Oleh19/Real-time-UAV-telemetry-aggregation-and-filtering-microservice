import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { IngestStats } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';
import { MetricsPanelComponent } from './metrics-panel.component';

function setup(stats: IngestStats | null, connected: boolean) {
  const stub = { stats: signal(stats), connected: signal(connected) };
  TestBed.configureTestingModule({
    imports: [MetricsPanelComponent],
    providers: [{ provide: TelemetryService, useValue: stub }],
  });
  const fixture = TestBed.createComponent(MetricsPanelComponent);
  fixture.detectChanges();
  return fixture.nativeElement as HTMLElement;
}

describe('MetricsPanelComponent', () => {
  it('renders stat tiles and online status when data is available', () => {
    const element = setup(
      { Received: 42, Dropped: 1, Published: 40, Failed: 0, Rejected: 1 },
      true,
    );
    expect(element.textContent).toContain('42');
    expect(element.textContent).toContain('rejected');
    expect(element.textContent).toContain('online');
  });

  it('shows a placeholder and offline status without data', () => {
    const element = setup(null, false);
    expect(element.textContent).toContain('Waiting for backend data');
    expect(element.textContent).toContain('offline');
  });
});
