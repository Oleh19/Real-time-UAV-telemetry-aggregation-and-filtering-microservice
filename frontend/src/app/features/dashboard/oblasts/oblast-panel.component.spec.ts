import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { OblastAlert } from '../../../core/models/telemetry';
import { TelemetryService } from '../../../core/telemetry.service';
import { OblastPanelComponent } from './oblast-panel.component';

function setup(alerts: OblastAlert[]) {
  const stub = { alerts: signal(alerts) };
  TestBed.configureTestingModule({
    imports: [OblastPanelComponent],
    providers: [{ provide: TelemetryService, useValue: stub }],
  });
  const fixture = TestBed.createComponent(OblastPanelComponent);
  fixture.detectChanges();
  return fixture.nativeElement as HTMLElement;
}

describe('OblastPanelComponent', () => {
  it('marks alarmed oblasts red and shows the alarm count', () => {
    const element = setup([
      { id: 1, name: 'Kyiv Oblast', alarmed: true, drones: 2 },
      { id: 2, name: 'Lviv Oblast', alarmed: false, drones: 0 },
    ]);
    const alarmed = element.querySelectorAll('.oblast-alarmed');
    expect(alarmed.length).toBe(1);
    expect(alarmed[0].textContent).toContain('Kyiv Oblast');
    expect(element.querySelector('.alarm-count')?.textContent).toContain('1');
  });

  it('shows a placeholder without oblast data', () => {
    const element = setup([]);
    expect(element.textContent).toContain('Waiting for oblast data');
  });
});
