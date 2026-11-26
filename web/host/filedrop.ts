import type { WasmTerm } from './wasm';

const carriesFiles = (e: DragEvent) => e.dataTransfer?.types.includes('Files') ?? false;

// the in-window pointer drag never fires drag events
export function attachFileDrop(canvas: HTMLCanvasElement, term: WasmTerm): void {
  // enter and leave fire for every element the drag crosses
  let depth = 0;
  const hover = (over: boolean) => term.fileDrag(over);

  canvas.addEventListener('dragenter', (e) => {
    if (!carriesFiles(e)) return;
    e.preventDefault();
    if (depth++ === 0) hover(true);
  });
  canvas.addEventListener('dragover', (e) => {
    if (!carriesFiles(e)) return;
    e.preventDefault(); // the browser only fires drop on a target that cancelled dragover
  });
  canvas.addEventListener('dragleave', (e) => {
    if (!carriesFiles(e) || depth === 0) return;
    if (--depth === 0) hover(false);
  });
  canvas.addEventListener('drop', (e) => {
    if (!carriesFiles(e)) return;
    e.preventDefault(); // otherwise the browser opens the dropped file
    depth = 0;
    hover(false);
    const r = canvas.getBoundingClientRect();
    // the browser empties dataTransfer.files once the event handler returns
    const files = Array.from(e.dataTransfer?.files ?? []);
    void send(term, files, e.clientX - r.left, e.clientY - r.top);
  });
}

async function send(term: WasmTerm, files: File[], x: number, y: number): Promise<void> {
  const cap = term.fileDropCap();
  const total = files.reduce((n, f) => n + f.size, 0);
  if (total > cap) {
    console.warn(`caution: dropped files total ${total} bytes, over the server's ${cap} byte cap, skipped`);
    return;
  }
  const read: { name: string; type: string; data: Uint8Array }[] = [];
  for (const f of files) {
    try {
      read.push({ name: f.name, type: f.type, data: new Uint8Array(await f.arrayBuffer()) });
    } catch {
      console.warn(`caution: dropped ${f.name} skipped, not a readable file`);
    }
  }
  if (read.length > 0) term.fileDrop(x, y, read);
}
