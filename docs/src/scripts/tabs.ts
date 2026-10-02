/**
 * ARIA tabs for the `[role="tab"]` buttons inside `root`: click, or arrow keys,
 * Home and End (with wrap-around), select a tab and update `aria-selected` and
 * the roving tabindex. `onSelect` shows or hides each tab's panel (the element
 * named by its `aria-controls`).
 */
export function setupTabs(
  root: HTMLElement,
  onSelect: (panel: HTMLElement, selected: boolean) => void,
  signal: AbortSignal,
): void {
  const tabs = Array.from(root.querySelectorAll<HTMLElement>('[role="tab"]'));
  const panels = tabs.map((tab) => root.querySelector<HTMLElement>(`#${tab.getAttribute('aria-controls')}`));
  const last = tabs.length - 1;

  const select = (index: number, focus: boolean) => {
    tabs.forEach((tab, i) => {
      const on = i === index;
      tab.setAttribute('aria-selected', String(on));
      tab.tabIndex = on ? 0 : -1;
      const panel = panels[i];
      if (panel) onSelect(panel, on);
    });
    if (focus) tabs[index].focus();
  };

  tabs.forEach((tab, i) => {
    const keys: Record<string, number> = {
      ArrowRight: i === last ? 0 : i + 1,
      ArrowDown: i === last ? 0 : i + 1,
      ArrowLeft: i === 0 ? last : i - 1,
      ArrowUp: i === 0 ? last : i - 1,
      Home: 0,
      End: last,
    };
    tab.addEventListener('click', () => select(i, false), { signal });
    tab.addEventListener(
      'keydown',
      (e) => {
        if (!(e.key in keys)) return;
        e.preventDefault();
        select(keys[e.key], true);
      },
      { signal },
    );
  });
}
