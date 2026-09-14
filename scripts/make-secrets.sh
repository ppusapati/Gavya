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
  # The only credential any service reads. Everything else it needs is in its
  # ConfigMap, which is why that one is in git and this one is not.
  DATABASE_URL: "postgres://gavya_app:${encoded}@${host}:${port}/${dbname}?sslmode=${sslmode}"
EOF
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
