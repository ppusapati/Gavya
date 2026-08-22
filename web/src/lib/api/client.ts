/**
 * The browser side of the Connect unary JSON protocol the services speak.
 *
 * A procedure is a POST to /<fully.qualified.Service>/<Method> with a JSON body
 * and a JSON reply. Failures arrive as {code, message} with an HTTP status the
 * code maps onto, which is what lets this client hand callers a typed reason
 * rather than "something went wrong".
 */

export type ConnectCode =
	| 'canceled'
	| 'unknown'
	| 'invalid_argument'
	| 'deadline_exceeded'
	| 'not_found'
	| 'already_exists'
	| 'permission_denied'
	| 'resource_exhausted'
	| 'failed_precondition'
	| 'aborted'
	| 'out_of_range'
	| 'unimplemented'
	| 'internal'
	| 'unavailable'
	| 'data_loss'
	| 'unauthenticated';

export class ApiError extends Error {
	readonly code: ConnectCode;
	readonly status: number;
	readonly procedure: string;

	constructor(code: ConnectCode, message: string, status: number, procedure: string) {
		super(message);
		this.name = 'ApiError';
		this.code = code;
		this.status = status;
		this.procedure = procedure;
	}

	/**
	 * Whether repeating the call could plausibly succeed. Only transient
	 * conditions qualify: an invalid argument never becomes valid by asking
	 * again, and retrying a call that already had an effect risks doing it twice.
	 */
	get retryable(): boolean {
		return this.code === 'unavailable' || this.code === 'resource_exhausted' || this.code === 'aborted';
	}

	/** A sentence to put in front of a person, not a stack trace. */
	get humane(): string {
		switch (this.code) {
			case 'not_found':
				return 'That record no longer exists. It may have been superseded.';
			case 'permission_denied':
				return 'This tenant is not permitted to see that record.';
			case 'unavailable':
				return 'The service is not responding. Nothing was changed.';
			case 'already_exists':
				return this.message;
			case 'invalid_argument':
				return this.message;
			default:
				return this.message || 'The service reported an unexpected failure.';
		}
	}
}

const STATUS_TO_CODE: Record<number, ConnectCode> = {
	400: 'invalid_argument',
	401: 'unauthenticated',
	403: 'permission_denied',
	404: 'not_found',
	409: 'already_exists',
	412: 'failed_precondition',
	429: 'resource_exhausted',
	500: 'internal',
	501: 'unimplemented',
	503: 'unavailable',
	504: 'deadline_exceeded'
};

export interface CallOptions {
	tenantId: string;
	/** Correlates this call with the server's logs. */
	requestId?: string;
	signal?: AbortSignal;
	/** Overridden in tests; defaults to the global fetch. */
	fetch?: typeof globalThis.fetch;
}

export interface ClientConfig {
	/** Gateway origin. Every service is reached through it. */
	baseUrl: string;
	timeoutMs?: number;
}

function newRequestId(): string {
	return `web-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export class ApiClient {
	readonly #baseUrl: string;
	readonly #timeoutMs: number;

	constructor(config: ClientConfig) {
		this.#baseUrl = config.baseUrl.replace(/\/+$/, '');
		this.#timeoutMs = config.timeoutMs ?? 15_000;
	}

	async call<Req extends object, Res>(procedure: string, body: Req, opts: CallOptions): Promise<Res> {
		const doFetch = opts.fetch ?? globalThis.fetch;
		const url = `${this.#baseUrl}/${procedure.replace(/^\/+/, '')}`;

		// The caller's own signal and the timeout both have to be able to abort
		// the request, so they are combined rather than one replacing the other.
		const timeout = new AbortController();
		const timer = setTimeout(() => timeout.abort(), this.#timeoutMs);
		const signal = opts.signal
			? AbortSignal.any([opts.signal, timeout.signal])
			: timeout.signal;

		let response: Response;
		try {
			response = await doFetch(url, {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json',
					Accept: 'application/json',
					'X-Tenant-ID': opts.tenantId,
					'X-Request-ID': opts.requestId ?? newRequestId()
				},
				body: JSON.stringify(body),
				signal
			});
		} catch (cause) {
			if (opts.signal?.aborted) throw cause;
			// A network failure is indistinguishable from an overloaded service,
			// so it is reported as unavailable and is therefore retryable.
			throw new ApiError('unavailable', 'the service could not be reached', 0, procedure);
		} finally {
			clearTimeout(timer);
		}

		if (!response.ok) {
			throw await toApiError(response, procedure);
		}
		return (await response.json()) as Res;
	}
}

async function toApiError(response: Response, procedure: string): Promise<ApiError> {
	const fallback = STATUS_TO_CODE[response.status] ?? 'internal';
	try {
		const body = (await response.json()) as { code?: string; message?: string };
		return new ApiError(
			// The service's own code is more precise than anything inferred from
			// the status, so it wins when present.
			(body.code as ConnectCode) || fallback,
			body.message || response.statusText,
			response.status,
			procedure
		);
	} catch {
		return new ApiError(fallback, response.statusText || 'request failed', response.status, procedure);
	}
}
