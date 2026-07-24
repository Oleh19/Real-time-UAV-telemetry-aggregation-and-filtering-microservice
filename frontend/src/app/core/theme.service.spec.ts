import { TestBed } from '@angular/core/testing';

import { ThemeService } from './theme.service';

describe('ThemeService', () => {
  beforeEach(() => {
    localStorage.removeItem('uav-theme');
    delete document.documentElement.dataset['theme'];
    TestBed.configureTestingModule({});
  });

  it('reflects the initial theme on the document root', () => {
    const service = TestBed.inject(ThemeService);
    TestBed.tick();
    expect(document.documentElement.dataset['theme']).toBe(service.theme());
  });

  it('toggles between dark and light and persists the choice', () => {
    const service = TestBed.inject(ThemeService);
    TestBed.tick();
    const initial = service.theme();

    service.toggle();
    TestBed.tick();

    const toggled = service.theme();
    expect(toggled).not.toBe(initial);
    expect(document.documentElement.dataset['theme']).toBe(toggled);
    expect(localStorage.getItem('uav-theme')).toBe(toggled);
    expect(service.isDark()).toBe(toggled === 'dark');
  });
});
