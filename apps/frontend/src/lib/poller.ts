/** How many polls in a row may fail before the data is called stale. */
export const staleAfter = 3;

export type PollerOptions = {
  everyMs: number;
  /** Whether to poll once at once on start, and not only after the first interval. */
  immediate: boolean;
  /**
   * Makes one poll. The function it is given tells whether the poller is still in the run that made the poll, so that
   * an answer that comes late can be dropped. It returns null if there is nothing to poll now, which counts as neither
   * a success nor a failure.
   */
  run: (live: () => boolean) => Promise<void> | null;
  /** Called with true after [staleAfter] failures in a row, and with false at the next success. */
  onStale: (stale: boolean) => void;
};

/** Poller calls a function at a fixed interval and tells when its failures have gone on for too long. */
export class Poller {
  private readonly options: PollerOptions;
  private timer: ReturnType<typeof setInterval> | null = null;
  private runs = 0;
  private failures = 0;
  private stale = false;

  constructor(options: PollerOptions) {
    this.options = options;
  }

  /** Starts polling. It does nothing if the poller is running. */
  start(): void {
    if (this.timer !== null) {
      return;
    }
    if (this.options.immediate) {
      this.poll();
    }
    this.timer = setInterval(() => this.poll(), this.options.everyMs);
  }

  /** Stops polling and forgets the failures. Answers to earlier polls are dropped. */
  stop(): void {
    if (this.timer !== null) {
      clearInterval(this.timer);
      this.timer = null;
    }
    this.runs++;
    this.failures = 0;
    this.stale = false;
  }

  private poll(): void {
    const run = this.runs;
    const live = () => run === this.runs;
    const task = this.options.run(live);
    task?.then(
      () => this.settle(run, true),
      () => this.settle(run, false),
    );
  }

  private settle(run: number, ok: boolean): void {
    if (run !== this.runs) {
      return;
    }
    if (ok) {
      this.failures = 0;
    } else {
      this.failures++;
    }
    const stale = this.failures >= staleAfter;
    if (stale !== this.stale) {
      this.stale = stale;
      this.options.onStale(stale);
    }
  }
}
