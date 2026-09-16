export const SANS =
  `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`;
export const MONO = `ui-monospace, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace`;

export interface Font {
  family: string;
  /** logical px */
  size: number;
  weight: number;
  italic: boolean;
  /** stable cache key */
  key: string;
}

export function font(
  size: number,
  opts: { family?: string; weight?: number; italic?: boolean } = {},
): Font {
  const family = opts.family ?? SANS;
  const weight = opts.weight ?? 400;
  const italic = opts.italic ?? false;
  return { family, size, weight, italic, key: `${weight}${italic ? 'i' : ''}:${size}:${family}` };
}
