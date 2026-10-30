import type { WasmTerm } from './wasm';

const carriesFiles = (e: DragEvent) => e.dataTransfer?.types.includes('Files') ?? false;

// OS file drags only. the in-window pointer drag never fires drag events
export function attachFileDrop(canvas: HTMLCanvasElement, term: WasmTerm): void {
  // enter and leave fire for every element the drag crosses, so the hover
  // state follows the balance instead of the last event
  let depth = 0;
  const hover = (over: boolean) => term.fileDrag(over);

  canvas.addEventListener('dragenter', (e) => {
    if (!carriesFiles(e)) return;
    e.preventDefault();
    if (depth++ === 0) hover(true);
  });
  canvas.addEventListener('dragover', (e) => {
    if (!carriesFiles(e)) return;
    e.preventDefault(); // without this the drop never fires
  });
  canvas.addEventListener('dragleave', (e) => {
    if (!carriesFiles(e) || depth === 0) return;
    if (--depth === 0) hover(false);
  });
  canvas.addEventListener('drop', (e) => {
    if (!carriesFiles(e)) return;
    e.preventDefault(); // else the browser navigates to the file
    depth = 0;
    hover(false);
    const r = canvas.getBoundingClientRect();
    // the list is only valid during the event
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
