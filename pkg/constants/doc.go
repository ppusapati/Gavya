// Package constants centralises environment-variable key names and other
// string constants referenced across the platform.
//
// Keeping these in one place prevents the drift pattern where the same
// env var is spelled `GRPC_PORT` in one service and `GRPC-PORT` in another.
// Consumers import the named constants (constants.GrpcPort, constants.HttpPort,
// constants.KafkaBrokers, …) rather than hard-coding string literals.
//
// New constants go here only when a string is referenced from more than one
// package. Package-local constants stay with their owning package.
package constants
