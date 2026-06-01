package tenant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ContextKey string

const TenantInfoKey ContextKey = "tenantInfo"

func HttpTenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p9log.Debugf("HTTP tenant middleware processing request: %s", r.URL.Path)

		// Extract tenant information from HTTP headers
		tenantID := r.Header.Get("X-Tenant-ID")
		tenantName := r.Header.Get("X-Tenant-Name")

		p9log.Debugf("Extracted tenant info - ID: %s, Name: %s", tenantID, tenantName)

		// Create a TenantInfo object
		tenant := models.TenantInfo{
			ID:   tenantID,
			Name: tenantName,
		}

		// Serialize the TenantInfo to JSON
		tenantJSON, err := json.Marshal(tenant)
		if err != nil {
			p9log.Error(err)
			return
		}

		// Convert the JSON to bytes
		tenantData := []byte(tenantJSON)

		// Create metadata with binary data
		tenantMetadata := metadata.Pairs("tenant_info", string(tenantData))

		// Create a context with the extracted tenant information
		ctx := metadata.NewOutgoingContext(r.Context(), tenantMetadata)
		// ctx := context.WithValue(r.Context(), "Grpc-Metadata-Namaste", "namaste")

		// Pass the updated context to the next HTTP handler
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GrpcTenantMiddleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		p9log.Context(ctx).Info("Metadata not found in gRPC context")
	}

	// Extract client IP for logging/debugging
	if headers, ok := metadata.FromIncomingContext(ctx); ok {
		xForwardFor := headers.Get("x-forwarded-host")
		if len(xForwardFor) > 0 && xForwardFor[0] != "" {
			ips := strings.Split(xForwardFor[0], ".")
			if len(ips) > 0 {
				clientIP := ips[0]
				p9log.Context(ctx).Debugf("Client IP from x-forwarded-host: %s", clientIP)
			}
		}
	}

	p9log.Context(ctx).Debugf("gRPC tenant middleware - metadata present: %v", ok)

	// Extract the tenant ID from metadata
	tenantID, ok := md["x-tenant-id"]
	if !ok || len(tenantID) == 0 {
		p9log.Context(ctx).Error("tenant id not found in metadata")
		return nil, status.Errorf(codes.NotFound, "tenant_info not found in metadata")
	}

	tenantName, ok := md["x-tenant-name"]
	if !ok || len(tenantName) == 0 {
		p9log.Context(ctx).Error("tenant name not found in metadata")
		return nil, status.Errorf(codes.NotFound, "tenant_info not found in metadata")
	}

	ctx = p9context.NewCurrentTenant(ctx, tenantID[0], tenantName[0])

	// Verify tenant context was set correctly
	tenantInfo, found := p9context.FromCurrentTenant(ctx)
	p9log.Context(ctx).Debugf("Tenant context set - ID: %s, found: %v", tenantInfo.GetId(), found)

	return handler(ctx, req)
}
