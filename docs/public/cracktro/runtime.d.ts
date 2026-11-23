/** Public API of the classic-script cracktro runtime. */
declare namespace CracktroDeck {
  type Child = Node | string | number | null | undefined | Child[];

  interface Slide {
    group: string;
    el: HTMLElement & { _lt?: ReturnType<typeof setTimeout> };
    /** Stable `#id` link; defaults to a slug of the heading (set it to keep a link after renaming). */
    id?: string;
    notes?: string;
    experimental?: string;
    key?: string;
    under?: string;
    techno?: number;
    enter?(direction: number): void;
    tick?(time: number): void;
    onNext?(): boolean | void;
    onPrev?(): boolean | void;
    onKey?(event: KeyboardEvent): boolean | void;
    header?: boolean;
    parent?: Slide;
    kids?: Slide[];
    band?: number;
    step?: number;
    red?: boolean;
  }

  interface Theme {
    base: number[];
    pal: string[];
    grid: number[];
    ang: number;
    cell: number;
    accent: string[];
    prog: number[];
    music?: string;
    red?: boolean;
    light?: boolean;
  }

  interface Sound {
    readonly muted: boolean;
    toggle(): boolean;
    cut(): void;
    techno(level: number): void;
    slide(direction: number, music: string | undefined, shift: number): void;
    touch(): void;
    step(direction: number): void;
    /** Optional: create or resume the audio context; kiosk mode calls it from the starting gesture while sound is on. */
    unlock?(): unknown;
  }

  interface Services {
    h: API["h"];
    letters: API["letters"];
    resetIndex: API["resetIndex"];
    sound: Sound;
    current(): Slide | undefined;
  }

  /** A predefined filtered view of the deck (`#<slide-id>&tour=<name>`): whole bands by group plus single slides by id. */
  interface Tour {
    title?: string;
    /** One-line description shown under the title in the slide map. */
    summary?: string;
    bands?: string[];
    slides?: string[];
  }

  interface Config {
    slides?: Slide[] | ((services: Services) => Slide[]);
    themes?: Record<string, Partial<Theme>>;
    /** Band title, subtitle and optional header slide id (defaults to the group). */
    bands?: Record<string, [title: string, subtitle: string, id?: string]>;
    solo?: Record<string, string>;
    prototypes?: Record<string, string>;
    tours?: Record<string, Tour>;
    last?: string | null;
    sound?: Sound;
    /** Milliseconds each slide stays up in kiosk mode (K); default 15000, minimum 1000. */
    kioskInterval?: number;
    /** Closed captions (C, CC) in kiosk mode: `scripts` maps a slide id to two speakers' alternating lines, captioned as roll-up speech-to-text; slides without one read their heading and slides.md summary. */
    captions?: { scripts?: Record<string, string[]> };
    css?: string;
    width?: number;
    height?: number;
  }

  interface Row {
    group: string;
    items: Slide[];
    first: number;
    start: number;
    header?: Slide;
  }

  interface Controller {
    slides: Slide[];
    rows: Row[];
    go(index: number): void;
    next(): void;
    prev(): void;
    band(direction: number): void;
    current(): number;
    /** Start a configured tour at its first slide, or show every slide again with `null`. */
    tour(name: string | null): void;
    tours(): string[];
    /** Start (`true`), stop (`false`) or toggle kiosk mode; returns whether it is on. Fullscreen needs a user gesture, so a scripted start rotates in the window. */
    kiosk(on?: boolean): boolean;
    /** Start (`true`), stop (`false`) or toggle closed captioning (CC); starting it starts kiosk mode, stopping kiosk mode stops it. Returns whether it is on. */
    captions(on?: boolean): boolean;
  }

  interface API {
    h(tag: string, attrs?: Record<string, any>, ...children: Child[]): HTMLElement;
    letters(text: string, className?: string): HTMLElement;
    resetIndex(): number;
    create(config?: Config): Controller;
  }
}

declare const CracktroDeck: CracktroDeck.API;

interface Window {
  CracktroDeck: CracktroDeck.API;
}
