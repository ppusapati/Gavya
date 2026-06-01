package databaseErrors

// import (
// 	"errors"

// 	"google.golang.org/grpc/codes"
// 	"google.golang.org/grpc/status"
// )

// var (
// 	ErrNoCtxMetaData = errors.New("No ctx metadata")
// )

// func DbErrResponse(err error) error {
// 	if dbErr, isDBErr := GetDatabaseError(err); isDBErr {
// 		// Handle the custom SQLSTATE error here
// 		switch dbErr.Sqlstate {
// 		case "42P01":
// 			return status.Error(codes.NotFound, dbErr.Message)
// 			// Add more cases for other SQLSTATE codes if needed
// 		}

// 		// Handle other database errors
// 		return status.Error(codes.Internal, dbErr.Message)
// 	}

// 	return status.Error(GetErrStatusCode(err), err.Error())
// }

// func GetDatabaseError(err error) (*DatabaseError, bool) {
// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return nil, false
// 	}

// 	for _, detail := range st.Details() {
// 		if dbError, isDBError := detail.(*DatabaseError); isDBError {
// 			return dbError, true
// 		}
// 	}

// 	return nil, false
// }
