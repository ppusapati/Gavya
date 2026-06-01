// Package graphql provides shared GraphQL utilities for Apollo Federation.
//
// # Overview
//
// This package contains:
//   - Custom scalars (DateTime, JSON, etc)
//   - Custom directives (@auth, @rateLimit, etc)
//   - GraphQL context utilities
//   - Common utilities and middleware
//
// # Federation Architecture
//
// Each service exposes a GraphQL schema at services/{service}/graphql/schema.graphql:
//
//	type Land @key(fields: "id") {
//	  id: ID!
//	  parcelNumber: String!
//	  address: String!
//	}
//
// The Apollo Federation Gateway (apps/graphql-gateway) discovers and composes
// all service schemas into a unified graph:
//
//	Gateway:
//	├─ Discovers services/*/graphql/schema.graphql
//	├─ Validates each schema
//	├─ Composes all schemas
//	├─ Stitches type extensions
//	└─ Routes requests to appropriate services
//
// # Custom Scalars
//
// DateTime - ISO 8601 timestamps
// JSON - Arbitrary JSON objects
// BigInt - 64-bit integers
// Decimal - Arbitrary precision decimals
// Date - Date only (YYYY-MM-DD)
// Time - Time only (HH:MM:SS)
// UUID - UUID values
// URL - URL strings
//
// # Custom Directives
//
// @auth(role: String!) - Require authentication with specific role
// @deprecated(reason: String!) - Deprecate fields
// @rateLimit(limit: Int!, window: String!) - Rate limit resolver
// @cache(ttl: Int!) - Cache resolver results
// @sensitive - Mark sensitive data (PII, etc)
//
// # Context
//
// GraphQL context carries:
//   - User ID and claims
//   - Request metadata
//   - Service references
//   - Observability (tracing, metrics)
//   - Database connections
//
// # Example
//
//	// Service schema
//	extend schema
//	  @link(url: "https://specs.apollo.dev/federation/v2.0")
//
//	type Land @key(fields: "id") {
//	  id: ID!
//	  parcelNumber: String!
//	  address: String!
//	  owner: Owner!
//	  createdAt: DateTime!
//	}
//
//	type Owner {
//	  id: ID!
//	  name: String!
//	  email: String!
//	}
//
//	type Query {
//	  land(id: ID!): Land @auth(role: "user")
//	  lands(skip: Int, limit: Int): [Land!]! @rateLimit(limit: 100, window: "1m")
//	}
//
// # Federation Patterns
//
// ## Basic Type Definition
//
//	type Land @key(fields: "id") {
//	  id: ID!
//	  ...
//	}
//
// ## Type Extension (from another service)
//
//	extend type Land {
//	  negotiationCount: Int!
//	  latestNegotiation: Negotiation
//	}
//
// ## Reference Resolution
//
//	type Land @key(fields: "id") {
//	  id: ID!
//	  reference(representation: LandInput!) {
//	    // Resolve reference from other services
//	  }
//	}
//
// # Build Process
//
// The build process (run by CI/CD):
//	1. Schema Discovery - Find all services/*/graphql/schema.graphql
//	2. Schema Validation - Validate each schema independently
//	3. Schema Composition - Compose all schemas using Apollo Federation
//	4. Type Generation - Generate TypeScript types from composed schema
//	5. Deployment - Deploy gateway with composed schema
//
// See tools/ directory for implementation details.
package graphql
