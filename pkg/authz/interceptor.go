package authz

import (
	"context"
	"strings"

	"p9e.in/samavaya/packages/p9context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryInterceptor returns a gRPC interceptor that enforces authz
func UnaryInterceptor(checkPermission func(ctx context.Context, req PermissionRequirement) (*CheckPermissionResponse, error)) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {

		user, err := extractUserFromMetadata(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or missing token")
		}

		perm := matchMethodToPermission(info.FullMethod)
		if perm == nil {
			return handler(ctx, req) // No authz required
		}

		// If permissions bundled in JWT, check statically
		if hasPermissionInJWT(user, perm.Namespace, perm.Resource, perm.Action) {
			ctx = enrichContext(ctx, user)
			return handler(ctx, req)
		}

		// Otherwise, dynamically check via UserService
		reply, err := checkPermission(ctx, *perm)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "authz error: %v", err)
		}
		if reply.Effect != Effect_GRANT || !reply.Allowed {
			return nil, status.Error(codes.PermissionDenied, "access denied")
		}

		ctx = enrichContext(ctx, user)
		return handler(ctx, req)
	}
}

// extractUserFromMetadata pulls token and parses it
func extractUserFromMetadata(ctx context.Context) (*InjectedUserInfo, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md["authorization"]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}
	token := strings.TrimPrefix(md["authorization"][0], "Bearer ")
	claims, err := ParseJWT(token)
	if err != nil {
		return nil, err
	}
	return &InjectedUserInfo{
		UserID:      claims.UserID,
		TenantID:    claims.TenantID,
		CompanyID:   claims.CompanyID,
		BranchID:    claims.BranchID,
		Role:        claims.Role,
		Permissions: claims.Permissions,
		SessionID:   claims.SessionID,
	}, nil
}

// hasPermissionInJWT checks static permission in token
func hasPermissionInJWT(user *InjectedUserInfo, ns, res, act string) bool {
	for _, p := range user.Permissions {
		if p.Namespace == ns && p.Resource == res && p.Action == act && p.Effect == Effect_GRANT {
			return true
		}
	}
	return false
}

// enrichContext injects user info for downstream use
func enrichContext(ctx context.Context, user *InjectedUserInfo) context.Context {
	// Set security context for query validation
	ctx = p9context.SetSecurityContext(ctx, user.UserID, user.Role)

	// Set unified user context (p9context architecture). Permission
	// projection delegates to PermissionsToStrings so the wire format is
	// defined exactly once in this package.
	permissions := PermissionsToStrings(user.Permissions)
	ctx = p9context.NewUserContext(ctx, p9context.UserContext{
		UserID:      user.UserID,
		TenantID:    user.TenantID,
		CompanyID:   user.CompanyID,
		BranchID:    user.BranchID,
		Role:        user.Role,
		Permissions: permissions,
	})

	// Set RLS scope from user context
	ctx = p9context.NewRLSScopeFromUserContext(ctx)

	// Legacy context values for backward compatibility
	ctx = context.WithValue(ctx, "user_id", user.UserID)
	ctx = context.WithValue(ctx, "tenant_id", user.TenantID)
	ctx = context.WithValue(ctx, "role", user.Role)
	ctx = context.WithValue(ctx, "permissions", user.Permissions)
	return ctx
}
