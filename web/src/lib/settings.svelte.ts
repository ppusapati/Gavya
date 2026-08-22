import { ApiClient, Gavya } from './api';

/**
 * Where the gateway is, which tenant is being reviewed, and who is reviewing.
 *
 * Phase-1 has no authentication, so none of this is a credential — the tenant
 * id is a routing fact and the actor is the name written into the audit trail
 * of any resolution. They are kept together because a workspace is useless
 * without all three, and a reviewer should have to state them once.
 */

const KEY = 'gavya.settings.v1';

const DEFAULT_GATEWAY = 'http://localhost:8000';

interface Stored {
	gatewayUrl: string;
	tenantId: string;
	actor: string;
}

function read(): Stored {
	const empty = { gatewayUrl: DEFAULT_GATEWAY, tenantId: '', actor: '' };
	if (typeof localStorage === 'undefined') return empty;
	try {
		const raw = localStorage.getItem(KEY);
		if (!raw) return empty;
		const parsed = JSON.parse(raw) as Partial<Stored>;
		return {
			gatewayUrl: parsed.gatewayUrl || DEFAULT_GATEWAY,
			tenantId: parsed.tenantId || '',
			actor: parsed.actor || ''
		};
	} catch {
		// A browser that refuses site data, or a value left behind by an older
		// shape, must not stop the workspace from opening.
		return empty;
	}
}

class Settings {
	#stored = read();

	gatewayUrl = $state(this.#stored.gatewayUrl);
	tenantId = $state(this.#stored.tenantId);
	actor = $state(this.#stored.actor);

	/** A workspace can only call anything once it knows which tenant to ask about. */
	get ready(): boolean {
		return this.gatewayUrl.trim() !== '' && this.tenantId.trim() !== '';
	}

	save() {
		if (typeof localStorage === 'undefined') return;
		try {
			localStorage.setItem(
				KEY,
				JSON.stringify({
					gatewayUrl: this.gatewayUrl.trim(),
					tenantId: this.tenantId.trim(),
					actor: this.actor.trim()
				})
			);
		} catch {
			// Storage being unavailable costs the reviewer a retype next visit;
			// it must not cost them the change they just made.
		}
	}

	/** A client bound to the current gateway and tenant. */
	api(): Gavya {
		return new Gavya(new ApiClient({ baseUrl: this.gatewayUrl.trim() }), this.tenantId.trim());
	}

	/** The name recorded against a resolution, falling back to something honest. */
	get actorOrUnknown(): string {
		return this.actor.trim() || 'unattributed';
	}
}

export const settings = new Settings();
