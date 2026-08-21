# One Dockerfile for every ML service; SERVICE selects which binary to build.
#
# The crates share a workspace and a lockfile, so building from a single context
# keeps their dependency versions identical to what the tests ran against.
ARG SERVICE

FROM rust:1.94-slim AS builder
ARG SERVICE
WORKDIR /workspace
COPY . .
RUN cargo build --release --bin "$SERVICE" \
 && cp "target/release/$SERVICE" /bin/mlservice

# cc rather than static: the release profile links against glibc. The nonroot
# tag matches the runAsUser 65532 the Kubernetes manifests require.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=builder /bin/mlservice /mlservice
ENTRYPOINT ["/mlservice"]
