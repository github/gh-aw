/** Public API of the classic-script cracktro runtime. */
declare namespace CracktroDeck {
  type Child = Node | string | number | null | undefined | Child[];

  interface Slide {
    group: string;
    el: HTMLElement & { _lt?: ReturnType<typeof setTimeout> };
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
  }

  interface Services {
    h: API["h"];
    letters: API["letters"];
    resetIndex: API["resetIndex"];
    sound: Sound;
    current(): Slide | undefined;
  }

  interface Config {
    slides?: Slide[] | ((services: Services) => Slide[]);
    themes?: Record<string, Partial<Theme>>;
    bands?: Record<string, [title: string, subtitle: string]>;
    solo?: Record<string, string>;
    prototypes?: Record<string, string>;
    last?: string | null;
    sound?: Sound;
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
