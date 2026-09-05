export class SoundStore {
  private ctx: AudioContext | null = null;
  private buffers = new Map<string, Promise<AudioBuffer | null>>();
  private gesture = false;

  constructor() {
    // the page has to see a click or a key before we let it make a sound
    // so we're not annoying anybody
    const unlock = () => {
      this.gesture = true;
      if (this.ctx?.state === 'suspended') void this.ctx.resume();
    };
    window.addEventListener('pointerdown', unlock, true);
    window.addEventListener('keydown', unlock, true);
  }

  preload(src: string): void {
    void this.buffer(src);
  }

  play(src: string): void {
    if (!this.gesture) return;
    const ctx = this.context();
    const ready = ctx.state === 'running' ? Promise.resolve() : ctx.resume();
    void Promise.all([ready, this.buffer(src)])
      .then(([, buf]) => {
        if (!buf || ctx.state !== 'running') return;
        const node = ctx.createBufferSource();
        node.buffer = buf;
        node.connect(ctx.destination);
        node.start();
      })
      .catch(() => {});
  }

  private context(): AudioContext {
    return (this.ctx ??= new AudioContext());
  }

  private buffer(src: string): Promise<AudioBuffer | null> {
    let p = this.buffers.get(src);
    if (!p) {
      p = fetch(src)
        .then((r) => {
          if (!r.ok) throw new Error(r.statusText);
          return r.arrayBuffer();
        })
        .then((bytes) => this.context().decodeAudioData(bytes))
        .catch((e: unknown) => {
          console.warn(`caution: sound ${src}:`, e);
          return null;
        });
      this.buffers.set(src, p);
    }
    return p;
  }
}
