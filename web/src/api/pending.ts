/**
 * PendingLines buffers incoming log lines until the next render, keeping at
 * most `maxLines` lines and `maxChars` characters of text. When either limit
 * is exceeded the oldest lines are dropped and counted, so a stream that is
 * not being rendered (such as in a background tab) cannot grow without bound.
 * The newest line is always kept.
 */
export class PendingLines<T extends { text: string }> {
  private items: T[] = [];
  private head = 0;
  private chars = 0;
  private droppedCount = 0;
  private readonly maxLines: number;
  private readonly maxChars: number;

  constructor(maxLines: number, maxChars: number) {
    this.maxLines = maxLines;
    this.maxChars = maxChars;
  }

  get size(): number {
    return this.items.length - this.head;
  }

  push(item: T): void {
    this.items.push(item);
    this.chars += item.text.length;
    while (this.size > 1 && (this.size > this.maxLines || this.chars > this.maxChars)) {
      this.chars -= this.items[this.head]?.text.length ?? 0;
      this.head++;
      this.droppedCount++;
    }
    // Compact once the dropped prefix dominates, keeping pushes amortized O(1).
    if (this.head > 1024 && this.head * 2 > this.items.length) {
      this.items = this.items.slice(this.head);
      this.head = 0;
    }
  }

  /** take returns the buffered lines and how many were dropped, and empties the buffer. */
  take(): { lines: T[]; dropped: number } {
    const out = { lines: this.items.slice(this.head), dropped: this.droppedCount };
    this.clear();
    return out;
  }

  clear(): void {
    this.items = [];
    this.head = 0;
    this.chars = 0;
    this.droppedCount = 0;
  }
}
