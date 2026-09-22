import type { ApiClient, CallOptions } from './client';
import * as T from './types';

/**
 * People, roles and machine credentials, as procedures.
 *
 * The console could sign in and nothing else: thirteen of identity-service's
 * fifteen procedures had no client, which meant a co-operative could be
 * authorised against roles nobody could be given. Adding a person, setting a
 * password, assigning a role and issuing a machine credential were all rows
 * somebody typed into a database console, which is not a thing an operator of a
 * village society is going to do.
 *
 * SignOut is here too, and it matters more than its size suggests: until it had
 * a caller, signing out of the console forgot the session locally and left it
 * valid on the platform until it expired.
 */
export class IdentityApi {
	readonly #client: ApiClient;
	readonly #tenantId: string;
	readonly #session: string;

	constructor(client: ApiClient, session: string, tenantId: string) {
		this.#client = client;
		this.#session = session;
		this.#tenantId = tenantId;
	}

	#opts(extra?: Partial<CallOptions>): CallOptions {
		return { session: this.#session, ...extra };
	}

	get tenantId(): string {
		return this.#tenantId;
	}

	/* ---- the session ---- */

	/**
	 * What this session actually holds.
	 *
	 * The gateway calls it on every request; a console calls it to show somebody
	 * which permissions they have rather than letting them find out by being
	 * refused.
	 */
	verifySession(extra?: Partial<CallOptions>) {
		return this.#client.call<T.VerifySessionRequest, T.VerifySessionResponse>(
			`${T.IDENTITY}/VerifySession`,
			{ session_id: this.#session },
			this.#opts(extra)
		);
	}

	/** End this session on the platform, not only in this browser. */
	signOut(reason?: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SignOutRequest, T.EmptyResponse>(
			`${T.IDENTITY}/SignOut`,
			{ session_id: this.#session, reason },
			this.#opts(extra)
		);
	}

	/** End every session a person holds. What a lost telephone needs. */
	signOutEverywhere(userId: string, reason?: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SignOutEverywhereRequest, T.SignOutEverywhereResponse>(
			`${T.IDENTITY}/SignOutEverywhere`,
			{ tenant_id: this.#tenantId, user_id: userId, reason },
			this.#opts(extra)
		);
	}

	/* ---- people ---- */

	listRoles(extra?: Partial<CallOptions>) {
		return this.#client.call<Record<string, never>, T.ListRolesResponse>(
			`${T.IDENTITY}/ListRoles`,
			{},
			this.#opts(extra)
		);
	}

	addMember(req: Omit<T.AddMemberRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AddMemberRequest, T.AddMemberResponse>(
			`${T.IDENTITY}/AddMember`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listMembers(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListMembersRequest, T.ListMembersResponse>(
			`${T.IDENTITY}/ListMembers`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	setMemberRole(userId: string, role: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SetMemberRoleRequest, T.EmptyResponse>(
			`${T.IDENTITY}/SetMemberRole`,
			{ tenant_id: this.#tenantId, user_id: userId, role, actor },
			this.#opts(extra)
		);
	}

	setMemberStatus(userId: string, status: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SetMemberStatusRequest, T.EmptyResponse>(
			`${T.IDENTITY}/SetMemberStatus`,
			{ tenant_id: this.#tenantId, user_id: userId, status, actor },
			this.#opts(extra)
		);
	}

	/**
	 * Change your own password.
	 *
	 * Whose is read from the session by the service, never from this request —
	 * there is no subject to pass, and that is the point.
	 */
	changePassword(currentPassword: string, newPassword: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ChangePasswordRequest, T.EmptyResponse>(
			`${T.IDENTITY}/ChangePassword`,
			{ current_password: currentPassword, new_password: newPassword },
			this.#opts(extra)
		);
	}

	/** Set somebody else's, which takes a permission the one above does not. */
	setMemberPassword(userId: string, password: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SetMemberPasswordRequest, T.EmptyResponse>(
			`${T.IDENTITY}/SetMemberPassword`,
			{ tenant_id: this.#tenantId, user_id: userId, password, actor },
			this.#opts(extra)
		);
	}

	/* ---- machine credentials ---- */

	/**
	 * Issue one. The reply carries the secret, once — there is nowhere to read
	 * it back from, so a caller that discards it has to issue another.
	 */
	issueServiceIdentity(
		req: Omit<T.IssueServiceIdentityRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.IssueServiceIdentityRequest, T.IssueServiceIdentityResponse>(
			`${T.IDENTITY}/IssueServiceIdentity`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	revokeServiceIdentity(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RevokeServiceIdentityRequest, T.EmptyResponse>(
			`${T.IDENTITY}/RevokeServiceIdentity`,
			{ tenant_id: this.#tenantId, id, actor },
			this.#opts(extra)
		);
	}

	listServiceIdentities(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListServiceIdentitiesRequest, T.ListServiceIdentitiesResponse>(
			`${T.IDENTITY}/ListServiceIdentities`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}
}
