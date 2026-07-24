import { DOCUMENT } from '@angular/common';
import { Injectable, computed, effect, inject, signal } from '@angular/core';

export type Theme = 'light' | 'dark';

const STORAGE_KEY = 'uav-theme';

@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly themeState = signal<Theme>(this.initialTheme());

  readonly theme = this.themeState.asReadonly();
  readonly isDark = computed(() => this.themeState() === 'dark');

  constructor() {
    effect(() => {
      const theme = this.themeState();
      this.document.documentElement.dataset['theme'] = theme;
      this.persist(theme);
    });
  }

  toggle(): void {
    this.themeState.update((current) => (current === 'dark' ? 'light' : 'dark'));
  }

  private initialTheme(): Theme {
    const stored = this.readStored();
    if (stored) {
      return stored;
    }
    const prefersLight = this.document.defaultView?.matchMedia('(prefers-color-scheme: light)');
    return prefersLight?.matches ? 'light' : 'dark';
  }

  private readStored(): Theme | null {
    const value = this.document.defaultView?.localStorage.getItem(STORAGE_KEY);
    return value === 'light' || value === 'dark' ? value : null;
  }

  private persist(theme: Theme): void {
    this.document.defaultView?.localStorage.setItem(STORAGE_KEY, theme);
  }
}
