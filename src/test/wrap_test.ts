import { wrapText } from '../gfx/text/wrap';
import { eq, test } from './harness';

// Fixed-advance measure: 7px per code point. These fixtures mirror
// go/terminal/text/wrap_test.go
const meas = (s: string): number => [...s].length * 7;

test('single line fits', () => {
  eq(wrapText(meas, 'ab cd', 100), [{ start: 0, end: 5 }]);
});

test('breaks at word boundary', () => {
  // 19 units; 14 fit. the fitted prefix ends at a word end
  eq(wrapText(meas, 'aaaa aaaa aaaa aaaa', 100), [
    { start: 0, end: 14 },
    { start: 15, end: 19 },
  ]);
});

test('break spaces fall in the gap', () => {
  eq(wrapText(meas, 'aa   bb', 30), [
    { start: 0, end: 2 },
    { start: 5, end: 7 },
  ]);
});

test('overlong word hard-breaks', () => {
  eq(wrapText(meas, 'aaaaaaaaaaaaaaa', 70), [
    { start: 0, end: 10 },
    { start: 10, end: 15 },
  ]);
});

test('honors newlines', () => {
  eq(wrapText(meas, 'ab\n\ncd', 100), [
    { start: 0, end: 2 },
    { start: 3, end: 3 },
    { start: 4, end: 6 },
  ]);
});

test('trailing newline yields an empty line', () => {
  eq(wrapText(meas, 'ab\n', 100), [
    { start: 0, end: 2 },
    { start: 3, end: 3 },
  ]);
});

test('empty string is one empty line', () => {
  eq(wrapText(meas, '', 100), [{ start: 0, end: 0 }]);
});

test('never splits a surrogate pair', () => {
  // each emoji is 2 UTF-16 units; two fit per line (14px ≤ 20). a break at
  // unit 3 would split the second emoji so it must land at 4.
  eq(wrapText(meas, '😀😀😀', 20), [
    { start: 0, end: 4 },
    { start: 4, end: 6 },
  ]);
});

test('always advances at pathological widths', () => {
  eq(wrapText(meas, 'abc', 1), [
    { start: 0, end: 1 },
    { start: 1, end: 2 },
    { start: 2, end: 3 },
  ]);
});
