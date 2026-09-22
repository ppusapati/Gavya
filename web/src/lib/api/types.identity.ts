/**
 * People, roles and machine credentials.
 *
 * Field names mirror the Go handlers' json tags exactly, and clients_test.go
 * compares the two on every run of the gate.
 *
 * Nothing here carries a password hash, and nothing asks for one. The one place
 * a secret crosses the wire in the other direction is IssueServiceIdentity's
 * reply, and it crosses once: the platform keeps a hash and cannot show it
 * again, because a credential that can be read back is one that can be read back
 * by whoever reaches the database.
 */

export interface Role {
	name: string;
	description: string;
	/**
	 * What the role actually gets. It arrives with the role rather than behind
	 * a second lookup, because "what does a supervisor get" is the question
	 * somebody assigning one is really asking.
	 */
	permissions: string[];
}

export interface ListRolesResponse {
	roles: Role[];
}

export interface Member {
	user_id: string;
	email: string;
	full_name: string;
	role: string;
	status: string;
	/**
	 * Whether they can sign in — not the hash, nor any part of it. A listing
	 * that carries a hash is a listing somebody pastes into a support ticket.
	 */
	has_password: boolean;
	last_login_at?: string;
}

export interface AddMemberRequest {
	tenant_id: string;
	email: string;
	full_name: string;
	role: string;
	/**
	 * May be omitted. The person then exists and cannot sign in until an
	 * administrator sets one, which is a real state and not an error.
	 */
	password?: string;
	actor: string;
}

export interface AddMemberResponse {
	user_id: string;
	/**
	 * False when the address already had an account and this attached it to the
	 * tenant instead of making a second one.
	 */
	created: boolean;
}

export interface ListMembersRequest {
	tenant_id: string;
}

export interface ListMembersResponse {
	members: Member[];
}

export interface SetMemberRoleRequest {
	tenant_id: string;
	user_id: string;
	role: string;
	actor: string;
}

export interface SetMemberStatusRequest {
	tenant_id: string;
	user_id: string;
	status: string;
	actor: string;
}

/**
 * Somebody changing their own.
 *
 * There is no user_id, deliberately. This is the one route needing no
 * permission — a person holding no role must still be able to change the
 * credential they were handed — and the current password is what authorises it.
 * A subject in the body would mean a route with no permission requirement that
 * names whose password to change.
 */
export interface ChangePasswordRequest {
	current_password: string;
	new_password: string;
}

export interface SetMemberPasswordRequest {
	tenant_id: string;
	user_id: string;
	password: string;
	actor: string;
}

export type EmptyResponse = Record<string, never>;

export interface IssueServiceIdentityRequest {
	tenant_id: string;
	name: string;
	/**
	 * Required. A credential with no end date is one nobody rotates, and a
	 * default chosen by the client would be this console setting a
	 * co-operative's rotation policy without being asked.
	 */
	expires_at: string;
	actor: string;
}

export interface IssueServiceIdentityResponse {
	id: string;
	name: string;
	/** Shown once. It is never retrievable again from anywhere. */
	secret: string;
	expires_at: string;
}

export interface RevokeServiceIdentityRequest {
	tenant_id: string;
	id: string;
	actor: string;
}

export interface ListServiceIdentitiesRequest {
	tenant_id: string;
}

export interface ServiceIdentity {
	id: string;
	name: string;
	status: string;
	expires_at: string;
	last_used_at?: string;
}

export interface ListServiceIdentitiesResponse {
	service_identities: ServiceIdentity[];
}

export interface VerifySessionRequest {
	session_id: string;
}

export interface VerifySessionResponse {
	tenant_id: string;
	user_id?: string;
	service_identity_id?: string;
	role_name?: string;
	/**
	 * Always present, empty included: a reader can tell "holds nothing" from
	 * "this reply is from a version that did not say".
	 */
	permissions: string[];
}

export interface SignOutRequest {
	session_id: string;
	reason?: string;
}

export interface SignOutEverywhereRequest {
	tenant_id: string;
	user_id: string;
	reason?: string;
}

export interface SignOutEverywhereResponse {
	sessions_ended: number;
}
