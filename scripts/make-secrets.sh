#!/usr/bin/env bash
# Produce the Kubernetes Secrets the deployments reference.
#
# Every deployment under services/*/deployments/k8s names a secret:
#
#     envFrom:
#     - secretRef:
#         name: settlement-service-secret
#
# and nothing in this repository created one. `kubectl apply -f` on the whole
# directory therefore produced twenty-eight pods stuck in CreateContainerConfigError,
# which is the good outcome; the bad one is a deployment where somebody made the
# secrets by hand once, and the twenty-ninth service added later has none and
# nobody remembers the shape of the others.
#
# The secrets are generated rather than committed, because the alternative is a
# credential in git. This writes them to a directory you then apply and delete,
# or pipes them straight to kubectl:
#
#   scripts/make-secrets.sh --password "$(openssl rand -base64 32)" --into ./secrets
#   scripts/make-secrets.sh --password "$PW" --stdout | kubectl apply -n gavya -f -
#
# The password is the one gavya_app connects with: the same value
# deploy/postgres-init was given as GAVYA_APP_PASSWORD, or whatever it was
# changed to since. It is not stored here and this script does not invent one —
# a generated default would be a credential that looks deliberate and is not.
#
# sslmode is verify-full. A cluster deployment reaches its database over a
# network somebody else can be on, and the one thing worse than no TLS is TLS
# that does not check who answered. Override --sslmode only for a database on a
# private network you control, and know that you have.
#
# DOWNLOAD_SIGNING_KEY is generated here rather than asked for, unlike the
# password. The difference is that the password has to match what PostgreSQL was
# already given, and a signing key has no counterpart: nothing else in the
# platform needs to know it, so there is nothing for a generated value to
# disagree with. A distinct key per service, so a compromise of one cannot forge
# the other's links.
#
# Rotating one: put the current value in DOWNLOAD_SIGNING_KEY_PREVIOUS, generate
# a new DOWNLOAD_SIGNING_KEY, and drop the previous one after a day. Changing it
# with no overlap invalidates every link already sent, which somebody discovers
# as a morning of reports that will not open.
set -euo pipefail

password=""
into=""
to_stdout=0
host="postgres"
port="5432"
dbname="dairy"
sslmode="verify-full"
namespace="gavya"

while [ $# -gt 0 ]; do
  case "$1" in
    --password)  password="$2"; shift 2 ;;
    --into)      into="$2"; shift 2 ;;
    --stdout)    to_stdout=1; shift ;;
    --host)      host="$2"; shift 2 ;;
    --port)      port="$2"; shift 2 ;;
    --dbname)    dbname="$2"; shift 2 ;;
    --sslmode)   sslmode="$2"; shift 2 ;;
    --namespace) namespace="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "make-secrets: unknown argument $1" >&2; exit 2 ;;
  esac
done

if [ -z "$password" ]; then
  echo "make-secrets: --password is required. It is the password gavya_app connects" >&2
  echo "make-secrets: with; this script will not invent one, because a generated" >&2
  echo "make-secrets: default is a credential that looks deliberate and is not." >&2
  exit 2
fi
if [ "$to_stdout" != "1" ] && [ -z "$into" ]; then
  echo "make-secrets: pass --into <dir> or --stdout" >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"

# Which services sign download links, read from their own config rather than
# listed here. Same principle as the loop below: a list is a thing somebody has
# to remember to add a service to.
needs_key() {
  local svc="$1"
  grep -rqs 'signedurl.FromEnv()' "$root/services/$svc/" && return 0
  return 1
}

# A key per service. Refuses rather than falling back to something weaker: a
# signing key from a source that is not the system's random device is not one.
signing_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32
    return
  fi
  if [ -r /dev/urandom ]; then
    head -c 32 /dev/urandom | base64 | tr -d '\n'
    echo
    return
  fi
  echo "make-secrets: no openssl and no /dev/urandom, so a signing key cannot be" >&2
  echo "make-secrets: generated. A key from anything weaker is not a key." >&2
  exit 1
}

# The services that reference a secret, read from the deployments rather than
# listed here. A list is a thing somebody has to remember to add a new service
# to, and forgetting is the failure this script exists to fix.
services=()
for d in "$root"/services/*/deployments/k8s/deployment.yaml; do
  grep -q 'secretRef' "$d" || continue
  services+=("$(basename "$(dirname "$(dirname "$(dirname "$d")")")")")
done
if [ "${#services[@]}" -eq 0 ]; then
  echo "make-secrets: no deployment references a secret, which cannot be right." >&2
  exit 1
fi

# URL-encode the password for the DSN. A password with an @ or a / in it and no
# encoding produces a DSN that parses into a different host, and the service
# fails to connect with an error about a name that does not exist.
encoded="$(printf '%s' "$password" | od -An -tx1 -v | tr -d ' \n' | sed 's/../%&/g')"

emit() {
  local svc="$1"
  cat <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: ${svc}-secret
  namespace: ${namespace}
type: Opaque
stringData:
  # The credentials this service reads. Everything else it needs is in its
  # ConfigMap, which is why that one is in git and this one is not.
  DATABASE_URL: "postgres://gavya_app:${encoded}@${host}:${port}/${dbname}?sslmode=${sslmode}"
EOF
  if needs_key "$svc"; then
    cat <<EOF
  # What signs this service's download links. A browser following an <a href>
  # sends no Authorization header, so a link carries its own authority: a token
  # naming the purpose, the tenant, the one resource it may fetch and when it
  # stops working. Whoever holds this key can mint one for anything.
  #
  # Distinct per service, so a compromise of one cannot forge the other's links.
  DOWNLOAD_SIGNING_KEY: "$(signing_key)"
EOF
  fi
}

if [ "$to_stdout" = "1" ]; then
  for svc in "${services[@]}"; do emit "$svc"; done
  exit 0
fi

mkdir -p "$into"
chmod 700 "$into"
for svc in "${services[@]}"; do
  emit "$svc" > "$into/${svc}-secret.yaml"
  chmod 600 "$into/${svc}-secret.yaml"
done

echo "make-secrets: wrote ${#services[@]} secrets to $into"
echo "make-secrets: these contain a live credential. Apply them and delete the"
echo "make-secrets: directory; do not commit it, and do not leave it on a laptop."
