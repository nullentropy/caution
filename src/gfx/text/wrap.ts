/**
 * Greedy line breaking over source-offset slices. Lines are [start, end)
 * ranges into the original string. The separators consumed at a break (the
 * spaces and the newline) fall in the gap between consecutive ranges, so
 * selections and copies always map back to exact source offsets.
 *
 * Both terminals implement this identically (go/terminal/text/wrap.go).
 */

export interface WrapLine {
  /** UTF-16 offsets into the source string; display text = s.slice(start, end) */
  start: number;
  end: number;
}

export function wrapText(measure: (s: string) => number, text: string, maxW: number): WrapLine[] {
  const lines: WrapLine[] = [];
  let p = 0;
  for (;;) {
    const nl = text.indexOf('\n', p);
    const pEnd = nl < 0 ? text.length : nl;
    wrapParagraph(measure, text, p, pEnd, maxW, lines);
    if (nl < 0) break;
    p = nl + 1;
  }
  return lines;
}

const isHigh = (cu: number): boolean => cu >= 0xd800 && cu <= 0xdbff;

function wrapParagraph(
  measure: (s: string) => number,
  text: string,
  start: number,
  end: number,
  maxW: number,
  out: WrapLine[],
): void {
  if (start >= end) {
    out.push({ start, end: start }); // empty line (blank paragraph, trailing \n)
    return;
  }
  let i = start;
  while (i < end) {
    // longest prefix that fits - measurement is monotonic, so binary search
    let lo = i + 1;
    let hi = end;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (measure(text.slice(i, mid)) <= maxW) lo = mid;
      else hi = mid - 1;
    }
    let brk = lo;
    // never split a surrogate pair
    if (brk < end && isHigh(text.charCodeAt(brk - 1))) brk = brk - 1 > i ? brk - 1 : brk + 1;
    if (brk < end) {
      // back off to the last break opportunity (either side of a space)
      // so words stay whole
      // if none then hard break mid-word
      let sp = -1;
      for (let j = brk; j > i; j--) {
        if (text.charCodeAt(j - 1) === 32 || text.charCodeAt(j) === 32) {
          sp = j;
          break;
        }
      }
      if (sp > i) brk = sp;
    }
    // the line ends before its trailing spaces
    // the next starts after them
    let lineEnd = brk;
    while (lineEnd > i && text.charCodeAt(lineEnd - 1) === 32) lineEnd--;
    out.push({ start: i, end: lineEnd > i ? lineEnd : brk });
    i = brk;
    while (i < end && text.charCodeAt(i) === 32) i++;
  }
}
