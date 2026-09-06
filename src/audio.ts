interface Voice {
  node: AudioBufferSourceNode;
  gain: GainNode;
}

export class SoundStore {
  private ctx: AudioContext | null = null;
  private buffers = new Map<string, Promise<AudioBuffer | null>>();
  private voices = new Map<string, Set<Voice>>();
  private wanted = new Set<string>();
  private gen = new Map<string, number>();
  private gesture = false;

  constructor() {
    const unlock = () => {
      this.gesture = true;
      if (this.ctx?.state === 'suspended') void this.ctx.resume();
      // a loop asked for before the first gesture should be sounding by now,
      // where a one-shot that old would just be a stale noise
      for (const src of this.wanted) this.start(src, true);
      this.wanted.clear();
    };
    window.addEventListener('pointerdown', unlock, true);
    window.addEventListener('keydown', unlock, true);
  }

  preload(src: string): void {
    void this.buffer(src);
  }

  play(src: string, loop = false): void {
    if (!this.gesture) {
      if (loop) {
        this.wanted.add(src);
        void this.buffer(src);
      }
      return;
    }
    this.start(src, loop);
  }

  stop(src?: string): void {
    if (src == null) {
      this.wanted.clear();
      for (const s of [...this.voices.keys()]) this.stopSrc(s);
      return;
    }
    this.wanted.delete(src);
    this.stopSrc(src);
  }

  private stopSrc(src: string): void {
    this.gen.set(src, (this.gen.get(src) ?? 0) + 1);
    const set = this.voices.get(src);
    if (!set || !this.ctx) return;
    const at = this.ctx.currentTime + 0.01;
    for (const v of set) {
      v.gain.gain.setValueAtTime(v.gain.gain.value, this.ctx.currentTime);
      v.gain.gain.linearRampToValueAtTime(0, at);
      v.node.stop(at);
    }
    this.voices.delete(src);
  }

  private start(src: string, loop: boolean): void {
    const ctx = this.context();
    const gen = this.gen.get(src) ?? 0;
    const ready = ctx.state === 'running' ? Promise.resolve() : ctx.resume();
    void Promise.all([ready, this.buffer(src)])
      .then(([, buf]) => {
        if (!buf || ctx.state !== 'running' || this.gen.get(src) !== gen && this.gen.has(src)) return;
        let set = this.voices.get(src);
        if (loop && set) for (const v of set) if (v.node.loop) return;
        const gain = ctx.createGain();
        const node = ctx.createBufferSource();
        node.buffer = buf;
        node.loop = loop;
        node.connect(gain).connect(ctx.destination);
        const voice: Voice = { node, gain };
        if (!set) this.voices.set(src, (set = new Set()));
        set.add(voice);
        const owner = set;
        node.onended = () => {
          owner.delete(voice);
          if (!owner.size && this.voices.get(src) === owner) this.voices.delete(src);
        };
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
