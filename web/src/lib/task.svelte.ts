import { ApiError } from './api';

/**
 * One in-flight call and what became of it.
 *
 * Screens here re-run the same query as filters change, so a reply that arrives
 * after a newer one was issued has to be discarded rather than painted: without
 * that, typing into a filter can leave the table showing the results of a
 * keystroke two back.
 */
export class Task<T> {
	data = $state<T | undefined>(undefined);
	error = $state<ApiError | undefined>(undefined);
	pending = $state(false);

	#issued = 0;
	#controller: AbortController | undefined;

	/** True once a call has completed, so an empty table can be told from an unrun one. */
	settled = $state(false);

	/**
	 * Forget the answer, and abandon any call still in flight.
	 *
	 * For when the question changes rather than the filters do — a screen that
	 * opens a different batch, a different sample, a different cycle. Without it
	 * the previous subject's answer stays on screen under the new subject's
	 * heading, which for something like a recall trace is a list of the wrong
	 * lots presented as the right ones.
	 *
	 * The issue counter moves so a reply already on its way is discarded rather
	 * than painted over the cleared state.
	 */
	reset(): void {
		this.#controller?.abort();
		this.#controller = undefined;
		this.#issued++;
		this.data = undefined;
		this.error = undefined;
		this.pending = false;
		this.settled = false;
	}

	async run(fn: (signal: AbortSignal) => Promise<T>): Promise<T | undefined> {
		this.#controller?.abort();
		const controller = new AbortController();
		this.#controller = controller;

		const mine = ++this.#issued;
		this.pending = true;
		this.error = undefined;

		try {
			const out = await fn(controller.signal);
			if (mine !== this.#issued) return undefined;
			this.data = out;
			this.settled = true;
			return out;
		} catch (cause) {
			if (mine !== this.#issued) return undefined;
			// The previous answer is discarded rather than left on screen beside an
			// error. It was the answer to a different question — an older filter, an
			// earlier identifier — and showing it under a failed query would state
			// something the platform no longer knows to be true.
			this.data = undefined;
			this.error =
				cause instanceof ApiError
					? cause
					: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
			this.settled = true;
			return undefined;
		} finally {
			if (mine === this.#issued) this.pending = false;
		}
	}
}
