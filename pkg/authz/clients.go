package authz

// import (
// 	"context"
// 	"net/http"

// 	pbu "p9e.in/samavaya/identity/api/v2/permission"
// 	pb "p9e.in/samavaya/identity/api/v2/user/userconnect"
// )

// // Client holds the gRPC user service client for permission checks
// type Client struct {
// 	userSvc pb.UserServiceClient
// }

// // NewClient creates a new authz client with a ConnectRPC HTTP client
// func NewClient(baseURL string, httpClient *http.Client) *Client {
// 	if httpClient == nil {
// 		httpClient = http.DefaultClient
// 	}

// 	return &Client{
// 		userSvc: pb.NewUserServiceClient(httpClient, baseURL),
// 	}
// }

// // CheckPermission calls the remote CheckUserPermission method
// func (c *Client) CheckPermission(ctx context.Context, req PermissionRequirement) (*pbu.CheckPermissionResponse, error) {
// 	return c.userSvc.CheckUserPermission(ctx, &pbu.CheckPermissionRequest{
// 		Namespace: req.Namespace,
// 		Resource:  req.Resource,
// 		Action:    req.Action,
// 	})
// }
