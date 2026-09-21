import { ApiClient, Gavya, type SignInResponse } from './api';

/**
 * Where the gateway is, and the session the console is calling under.
 *
 * This used to hold a gateway, a tenant id and a name the reviewer typed, and
 * the comment here said Phase-1 had no authentication so none of it was a
 * credential. That stopped being true when authorisation was added and nobody
 * came back to this file: the gateway now refuses every procedure without a
 * bearer token, and the tenant it acts for is the session's, not one a browser
 * can claim. The console went on sending a tenant header the gateway does not
 * read and no credential at all, so every screen in it got 401.
 *
 * So the tenant and the actor are no longer typed. They are what the session
 * says they are, which is the only version of them the platform will honour.
 */

const KEY = 'gavya.settings.v1';

const DEFAULT_GATEWAY = 'http://localhost:8000';

interface Stored {
	gatewayUrl: string;
	session: string;
	tenantId: string;
	userId: string;
	roleName: string;
	expiresAt: string;
	timezone: string;
}

/**
 * The browser's own zone, as a starting point and not as an answer.
 *
 * Which day a collection falls on is a local fact, and milk-service refuses to
 * guess it — a collection at one in the morning Indian time falls on the 11th in
 * Kolkata and the 10th in UTC, and the fortnight a member is paid for is drawn
 * from that. The tenant's zone is the one that counts and the platform holds it;
 * until the tenant screens read it back, this is the reviewer's own, shown in
 * the panel so a mismatch is visible rather than silent.
 */
function browserZone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}

const empty: Stored = {
	gatewayUrl: DEFAULT_GATEWAY,
	session: '',
	tenantId: '',
	userId: '',
	roleName: '',
	expiresAt: '',
	timezone: browserZone()
};

function read(): Stored {
	if (typeof localStorage === 'undefined') return { ...empty };
	try {
		const raw = localStorage.getItem(KEY);
		if (!raw) return { ...empty };
		const parsed = JSON.parse(raw) as Partial<Stored>;
		return {
			gatewayUrl: parsed.gatewayUrl || DEFAULT_GATEWAY,
			session: parsed.session || '',
			tenantId: parsed.tenantId || '',
			userId: parsed.userId || '',
			roleName: parsed.roleName || '',
			expiresAt: parsed.expiresAt || '',
			timezone: parsed.timezone || browserZone()
		};
	} catch {
		// A browser that refuses site data, or a value left behind by an older
		// shape, must not stop the workspace from opening.
		return { ...empty };
	}
}

class Settings {
	#stored = read();

	gatewayUrl = $state(this.#stored.gatewayUrl);
	session = $state(this.#stored.session);
	tenantId = $state(this.#stored.tenantId);
	userId = $state(this.#stored.userId);
	roleName = $state(this.#stored.roleName);
	expiresAt = $state(this.#stored.expiresAt);
	timezone = $state(this.#stored.timezone);

	/**
	 * A workspace can call something once it is signed in.
	 *
	 * The expiry is checked here rather than waited for, so a console left open
	 * overnight asks for a password instead of showing a screen of 401s.
	 */
	get ready(): boolean {
		if (this.gatewayUrl.trim() === '' || this.session === '') return false;
		return !this.expired;
	}

	get expired(): boolean {
		if (!this.expiresAt) return false;
		const at = Date.parse(this.expiresAt);
		return Number.isFinite(at) && at <= Date.now();
	}

	/** Exchange an email and password for a session, and remember it. */
	async signIn(email: string, password: string, tenantId?: string): Promise<void> {
		const res: SignInResponse = await Gavya.signIn(
			new ApiClient({ baseUrl: this.gatewayUrl.trim() }),
			{ email, password, ...(tenantId ? { tenant_id: tenantId } : {}) }
		);
		this.session = res.session_id;
		this.tenantId = res.tenant_id;
		this.userId = res.user_id;
		this.roleName = res.role_name ?? '';
		this.expiresAt = res.expires_at;
		this.save();
	}

	/**
	 * Forget the session.
	 *
	 * Local only: it does not tell the platform, so the session stays valid until
	 * it expires or somebody revokes it. Saying so rather than calling this a
	 * sign-out, because a person who signs out of a shared machine is entitled to
	 * know which of those two they got.
	 */
	forget() {
		this.session = '';
		this.tenantId = '';
		this.userId = '';
		this.roleName = '';
		this.expiresAt = '';
		this.save();
	}

	save() {
		if (typeof localStorage === 'undefined') return;
		try {
			localStorage.setItem(
				KEY,
				JSON.stringify({
					gatewayUrl: this.gatewayUrl.trim(),
					session: this.session,
					tenantId: this.tenantId,
					userId: this.userId,
					roleName: this.roleName,
					expiresAt: this.expiresAt,
					timezone: this.timezone
				} satisfies Stored)
			);
		} catch {
			// Storage being unavailable costs the reviewer a sign-in next visit;
			// it must not cost them the change they just made.
		}
	}

	/** A client bound to the current gateway and session. */
	api(): Gavya {
		return new Gavya(
			new ApiClient({ baseUrl: this.gatewayUrl.trim() }),
			this.session,
			this.tenantId
		);
	}

	/**
	 * The name recorded against a resolution.
	 *
	 * The signed-in user, not a typed name. A resolution attributed to whatever
	 * somebody put in a box is not an attribution.
	 */
	get actorOrUnknown(): string {
		return this.userId || 'unattributed';
	}
}

export const settings = new Settings();
