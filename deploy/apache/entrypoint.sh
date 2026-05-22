#!/bin/sh
set -eu

PORT="${PORT:-8080}"
LB_REGION="${LB_REGION:-unknown}"

# Comma-separated upstream URLs; Railway spreads replicas behind one internal hostname.
# Example: http://discoveryd.railway.internal:8088
# For explicit multi-member balancing, list up to 3 URLs (same host repeated is valid but redundant).
UPSTREAM_LIST="${DISCOVERYD_UPSTREAM_LIST:-${DISCOVERYD_UPSTREAM:-http://discoveryd.railway.internal:8088}}"

BALANCER_MEMBERS=""
OLDIFS=$IFS
IFS=','
for upstream in $UPSTREAM_LIST; do
  upstream=$(echo "$upstream" | tr -d ' ')
  [ -z "$upstream" ] && continue
  BALANCER_MEMBERS="${BALANCER_MEMBERS}    BalancerMember \"${upstream}\" status=+H hcmethod=GET hcuri=/healthz
"
done
IFS=$OLDIFS

if [ -z "$BALANCER_MEMBERS" ]; then
  echo "No DISCOVERYD_UPSTREAM_LIST members configured" >&2
  exit 1
fi

export PORT LB_REGION BALANCER_MEMBERS

envsubst '${PORT} ${LB_REGION} ${BALANCER_MEMBERS}' \
  < /usr/local/apache2/conf/httpd.conf.template \
  > /usr/local/apache2/conf/httpd.conf

echo "Apache LB :${PORT} region=${LB_REGION} members:"
echo "$BALANCER_MEMBERS"

exec httpd -DFOREGROUND
