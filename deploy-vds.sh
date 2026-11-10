#!/usr/bin/env bash
set -euo pipefail

revision=${1:?Commit SHA required}
[[ "$revision" =~ ^[0-9a-f]{40}$ ]]
container=myutils-api-vds
previous="$container-before-deploy"
image="$container:$revision"
runtime_env=/run/myutils-api.env

test -s "$runtime_env"
if docker container inspect "$previous" >/dev/null 2>&1; then
  echo "Resolve the previous API deployment before retrying" >&2
  exit 1
fi
test "$(docker port "$container" 8080/tcp)" = 127.0.0.1:18080
test "$(docker inspect -f '{{(index .NetworkSettings.Networks "my-utils-vds").IPAddress}}' "$container")" = 172.29.50.10

docker build --build-arg "GIT_COMMIT=$revision" --tag "$image" .

wait_healthy() {
  local name="$1" expected_revision="${2:-}" state
  for ((attempt=0; attempt<90; attempt++)); do
    state=$(docker inspect -f '{{.State.Status}} {{.State.Health.Status}} {{.HostConfig.RestartPolicy.Name}}' "$name")
    if [[ "$state" == 'running healthy unless-stopped' ]]; then
      if [[ -z "$expected_revision" || "$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$name")" == "$expected_revision" ]]; then
        return 0
      fi
    fi
    sleep 2
  done
  docker inspect -f 'state={{.State.Status}} health={{.State.Health.Status}} restarts={{.RestartCount}}' "$name" >&2
  return 1
}

rollback() {
  local status=$?
  trap - EXIT
  if [[ "$status" != 0 ]] && docker container inspect "$previous" >/dev/null 2>&1; then
    echo "API deployment failed; attempting to restore the previous container" >&2
    if docker container inspect "$container" >/dev/null 2>&1; then
      docker rm -f "$container" >/dev/null || exit "$status"
    fi
    if docker rename "$previous" "$container" && docker start "$container" >/dev/null && wait_healthy "$container"; then
      echo "Previous API container is healthy again" >&2
    else
      echo "Previous API could not be verified; inspect production and database compatibility" >&2
    fi
  fi
  exit "$status"
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker rename "$container" "$previous"
docker stop --time 30 "$previous" >/dev/null
docker run --detach --pull=never --restart=unless-stopped --network my-utils-vds --ip 172.29.50.10 \
  --name "$container" --publish 127.0.0.1:18080:8080 --add-host host.docker.internal:172.29.50.1 \
  --env-file "$runtime_env" --mount type=bind,src=/srv/rcnet-vds/auth,dst=/run/rcnet,readonly \
  --label logging=promtail --label app=my-utils-api --label "org.opencontainers.image.revision=$revision" \
  --cap-drop=ALL --security-opt=no-new-privileges "$image"
wait_healthy "$container" "$revision"
trap - EXIT
docker rm "$previous" >/dev/null
echo "My Utils API healthy on VDS at $revision"
