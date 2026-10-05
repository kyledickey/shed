/** A streaming secret redactor for the redaction demo. */

const MASK = "***";

/** MIN_SECRET is the shortest value that is masked, as in shed. */
export const MIN_SECRET = 8;

const maskable = (s: string) => s.length >= MIN_SECRET;

/** StreamRedactor masks literal secrets in text written to it in arbitrary pieces. */
export class StreamRedactor {
  private secrets: string[];
  private buf = "";
  /** How many characters are held back after each write: the longest secret minus one. */
  readonly tail: number;

  constructor(secrets: string[]) {
    this.secrets = secrets.filter(maskable).sort((a, b) => b.length - a.length);
    this.tail = this.secrets.length > 0 ? this.secrets[0]!.length - 1 : 0;
  }

  /** write adds a piece of output and returns the text that is now safe to emit. */
  write(piece: string): string {
    this.buf += piece;
    return this.drain(false);
  }

  /** flush returns whatever is still held back, as at the end of the stream. */
  flush(): string {
    return this.drain(true);
  }

  /** held is the text withheld so far, waiting for a possible match to complete. */
  get held(): string {
    return this.buf;
  }

  private drain(final: boolean): string {
    let out = "";
    let consumed = 0;
    while (consumed < this.buf.length) {
      const remaining = this.buf.slice(consumed);
      const safe = remaining.length - (final ? 0 : this.tail);
      if (safe <= 0) break;
      let first = safe;
      let length = 0;
      for (const secret of this.secrets) {
        const i = remaining.indexOf(secret);
        if (i >= 0 && i < first) {
          first = i;
          length = secret.length;
        }
      }
      if (first > 0) {
        out += remaining.slice(0, first);
        consumed += first;
      }
      if (length > 0) {
        out += MASK;
        consumed += length;
      }
    }
    this.buf = this.buf.slice(consumed);
    return out;
  }
}

/** redactEach replaces secrets inside each piece on its own, so a secret split across pieces survives. */
export function redactEach(piece: string, secrets: string[]): string {
  let out = piece;
  for (const s of secrets.filter(maskable).sort((a, b) => b.length - a.length)) {
    out = out.split(s).join(MASK);
  }
  return out;
}

/** chunk splits text into pieces of at most size characters. */
export function chunk(text: string, size: number): string[] {
  const out: string[] = [];
  for (let i = 0; i < text.length; i += size) out.push(text.slice(i, i + size));
  return out;
}
