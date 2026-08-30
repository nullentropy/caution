/**
 * Micro test harness for the TS client's pure logic. Tests are bundled by
 * `go run -C go ./cmd/tstest` (esbuild, same toolchain as the terminal
 * bundle) and executed with node.
 */

declare const process: { exit(code: number): void };

let failures = 0;
let count = 0;

export function test(name: string, fn: () => void): void {
  count++;
  try {
    fn();
    console.log(`ok   ${name}`);
  } catch (e) {
    failures++;
    console.error(`FAIL ${name}: ${(e as Error).message}`);
  }
}

export function eq<T>(got: T, want: T, msg = ''): void {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) throw new Error(`${msg ? msg + ': ' : ''}got ${g}, want ${w}`);
}

export function done(): void {
  if (failures > 0) {
    console.error(`${failures}/${count} tests failed`);
    process.exit(1);
  }
  console.log(`all ${count} tests passed`);
}
