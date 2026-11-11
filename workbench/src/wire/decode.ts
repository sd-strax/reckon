// The decoders at the host's wire boundaries (adr/0244): what the sidecar and
// the backend send starts as `unknown` and is read through these. A required
// field that is missing or of the wrong type is a named error, not a blank on
// screen. Keys a decoder does not name are ignored, so a version may add fields.
// What `required` means comes from the Go tags, and the fixtures hold it there.

/** Read `value`, found at `at`, or throw naming what was wrong there. */
export type Decoder<T> = (value: unknown, at: string) => T;

/** The type a decoder produces. */
export type Decoded<D> = D extends Decoder<infer T> ? T : never;

export class DecodeError extends Error {}

function wrong(value: unknown, at: string, expected: string): DecodeError {
  const where = at || "the response";
  return new DecodeError(value === undefined ? `${where} missing` : `${where} is not ${expected}`);
}

export const str: Decoder<string> = (value, at) => {
  if (typeof value !== "string") throw wrong(value, at, "a string");
  return value;
};

export const num: Decoder<number> = (value, at) => {
  if (typeof value !== "number") throw wrong(value, at, "a number");
  return value;
};

export const bool: Decoder<boolean> = (value, at) => {
  if (typeof value !== "boolean") throw wrong(value, at, "a boolean");
  return value;
};

/** Opaque data, passed through unread. It must be there; null is a value. */
export const anything: Decoder<unknown> = (value, at) => {
  if (value === undefined) throw wrong(value, at, "");
  return value;
};

/** A field Go may omit (`omitempty`, or a nil pointer). */
export function optional<T>(decoder: Decoder<T>): Decoder<T | undefined> {
  return (value, at) => (value === undefined || value === null ? undefined : decoder(value, at));
}

/**
 * A field Go always sends that may be null (a pointer without omitempty): the
 * key must be there, so a renamed field is an error, not a silent null.
 */
export function nullable<T>(decoder: Decoder<T>): Decoder<T | null> {
  return (value, at) => {
    if (value === undefined) throw wrong(value, at, "");
    return value === null ? null : decoder(value, at);
  };
}

/**
 * A slice Go always sends, as null when nil. The key must be there: a renamed
 * list is an error, not an empty one. `optional(list(…))` is one it may omit.
 */
export function list<T>(item: Decoder<T>): Decoder<T[]> {
  return (value, at) => {
    if (value === null) return [];
    if (!Array.isArray(value)) throw wrong(value, at, "a list");
    return value.map((v, i) => item(v, `${at}[${i}]`));
  };
}

export function record<T>(item: Decoder<T>): Decoder<Record<string, T>> {
  return (value, at) => {
    if (value === null) return {};
    if (typeof value !== "object" || Array.isArray(value)) throw wrong(value, at, "an object");
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, item(v, `${at}.${k}`)]));
  };
}

type Shape = Record<string, Decoder<unknown>>;

/** The keys `shape` names, each decoded; any other key is ignored. */
export function object<S extends Shape>(shape: S): Decoder<{ [K in keyof S]: Decoded<S[K]> }> {
  return (value, at) => {
    if (typeof value !== "object" || value === null || Array.isArray(value)) throw wrong(value, at, "an object");
    const input = value as Record<string, unknown>;
    const out: Record<string, unknown> = {};
    for (const [key, decoder] of Object.entries(shape)) {
      const path = at ? `${at}.${key}` : key;
      if (!(key in input)) absent?.push(path);
      const decoded = decoder(input[key], path);
      if (decoded !== undefined) out[key] = decoded;
    }
    return out as { [K in keyof S]: Decoded<S[K]> };
  };
}

/** A decoder for a recursive shape, resolved when it runs. */
export function lazy<T>(decoder: () => Decoder<T>): Decoder<T> {
  return (value, at) => decoder()(value, at);
}

/** Decode one message from `from`, naming it in any failure. */
export function decode<T>(from: string, decoder: Decoder<T>, value: unknown): T {
  try {
    return decoder(value, "");
  } catch (err) {
    if (err instanceof DecodeError) {
      throw new DecodeError(`unexpected response from ${from}: ${err.message}`);
    }
    throw err;
  }
}

let absent: string[] | null = null;

/**
 * The keys a decoder names that `value` does not have. For the fixture test:
 * against a sample with every field filled, a name here is a key the decoder
 * reads and the wire no longer sends, the rename an optional field would hide.
 */
export function namedButAbsent<T>(decoder: Decoder<T>, value: unknown): string[] {
  absent = [];
  try {
    decoder(value, "");
    return absent;
  } finally {
    absent = null;
  }
}
