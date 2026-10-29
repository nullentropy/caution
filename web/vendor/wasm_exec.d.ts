// Types for the Go toolchain's wasm_exec.js (vendored by cmd/bundle from
// $GOROOT/lib/wasm; the .js is refreshed every bundle so its runtime ABI
// always matches the shaper.wasm built alongside it).
declare global {
  class Go {
    importObject: WebAssembly.Imports;
    run(instance: WebAssembly.Instance): Promise<void>;
  }
}
export {};
