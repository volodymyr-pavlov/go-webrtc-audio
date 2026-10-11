/** An integer made of bits that can be combined with `|` and tested with `&`. */
export type Bits = number;

/** Tests whether any bit of `mask` is set in `value`. */
export function has(value: Bits, mask: Bits): boolean {
  return (value & mask) !== 0;
}

/**
 * Returns the entry of a table for a phase, a stage or a connection state. The table has an entry for every bit, which
 * its type makes the compiler check, so the lookup cannot come back empty.
 */
export function pick<T>(table: Readonly<Record<number, T>>, bit: Bits): T {
  return table[bit] as T;
}
