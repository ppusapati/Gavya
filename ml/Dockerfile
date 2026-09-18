# One Dockerfile for every ML service; SERVICE selects which binary to build.
#
# The crates share a workspace and a lockfile, so building from a single context
# keeps their dependency versions identical to what the tests ran against.
ARG SERVICE

FROM rust:1.94-slim AS builder
WORKDIR /workspace
COPY . .

# Every binary, then pick one — rather than `cargo build --bin "$SERVICE"`.
#
# This line mentions no build argument, so it is the same instruction for all
# four images and its layer is shared between them: the workspace and its
# dependencies compile once instead of four times. With lto and one codegen unit
# that is most of the build. The cost is that each image's builder stage holds
# four binaries; the stage is discarded and the image below carries one.
RUN cargo build --release --bins

ARG SERVICE
RUN test -n "$SERVICE" || { echo "build with --build-arg SERVICE=<crate>"; exit 1; }
RUN cp "target/release/$SERVICE" /bin/mlservice

# cc rather than static: the release profile links against glibc. The nonroot
# tag matches the runAsUser 65532 the Kubernetes manifests require.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=builder /bin/mlservice /mlservice
ENTRYPOINT ["/mlservice"]
