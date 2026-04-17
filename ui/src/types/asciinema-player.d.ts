declare module 'asciinema-player' {
  export function create(
    src: string,
    elem: HTMLElement,
    opts?: Record<string, unknown>
  ): {
    dispose: () => void;
    el?: unknown;
    play: () => void;
    pause: () => void;
    seek: (pos: number) => void;
  };
}
