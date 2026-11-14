#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

export EDUFORGE_PORT="${EDUFORGE_ACCEPTANCE_PORT:-18080}"
project=eduforge-verify
run_dir="/workspace/acceptance-$(date -u +%Y%m%dT%H%M%S)-$$"
staging=$(mktemp -d /tmp/eduforge-acceptance.XXXXXX)
ssh_container="eduforge-verify-ssh-$$"
ssh_started=

cleanup() {
  if [ -n "$ssh_started" ]; then
    compose exec -T eduforge rm -f /secrets/acceptance-ssh-key || true
    docker stop "$ssh_container" >/dev/null || true
  fi
  rm -rf "$staging"
}

compose() {
  docker compose -p "$project" "$@"
}
trap cleanup EXIT

compose up --no-build -d --wait --wait-timeout 45
compose exec -T eduforge sh -c 'cat > /workspace/centroid-fixture.mjs' < testdata/centroid/experiment.mjs
compose exec -T eduforge node --input-type=module - seed "$run_dir" /workspace/centroid-fixture.mjs < scripts/acceptance.mjs

# Interrupt only the named acceptance service while its second command is running.
compose kill -s SIGKILL eduforge
compose up --no-build --force-recreate -d --wait --wait-timeout 45
compose exec -T eduforge node --input-type=module - after-recreate "$run_dir" < scripts/acceptance.mjs

files='seed.json recreate.json recovery.log experiment.log result.csv experiment-status.json experiment.bundle reader.html reader.pdf talk.pptx talk.pdf'
if [ "${EDUFORGE_ACCEPTANCE_SSH:-0}" = 1 ]; then
  docker build --progress quiet -t eduforge-verify-ssh:local testdata/ssh
  ssh-keygen -q -t ed25519 -N '' -f "$staging/fixture-key"
  docker run -d --rm --name "$ssh_container" --network "${project}_default" --network-alias ssh-fixture eduforge-verify-ssh:local >/dev/null
  ssh_started=1
  docker exec -i -u experiment "$ssh_container" sh -c 'umask 077; cat > /home/experiment/.ssh/authorized_keys' < "$staging/fixture-key.pub"
  commit=$(docker exec -i -u experiment "$ssh_container" sh -eu -c '
    mkdir /home/experiment/workspace/fixture-repo
    cd /home/experiment/workspace/fixture-repo
    cat > experiment.mjs
    git init -q -b main
    git add experiment.mjs
    git -c user.name=EduForge -c user.email=eduforge@localhost commit -qm "Synthetic SSH experiment"
    git rev-parse HEAD' < testdata/centroid/experiment.mjs)
  attempts=0
  until known_host=$(docker exec "$ssh_container" cat /etc/ssh/ssh_host_ed25519_key.pub 2>/dev/null); do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 20 ]; then
      docker logs "$ssh_container"
      exit 1
    fi
    sleep 1
  done
  printf '{"commit":"%s","known_hosts":"ssh-fixture %s\\n"}\n' "$commit" "$known_host" |
    compose exec -T eduforge sh -c 'cat > "$1/ssh-fixture.json"' sh "$run_dir"
  compose exec -T eduforge sh -c 'umask 077; cat > /secrets/acceptance-ssh-key' < "$staging/fixture-key"
  compose exec -T eduforge node --input-type=module - ssh-start "$run_dir" < scripts/acceptance.mjs
  compose up --no-build --force-recreate -d --wait --wait-timeout 45
  compose exec -T eduforge node --input-type=module - ssh-check "$run_dir" < scripts/acceptance.mjs
  files="$files ssh.log ssh.json"
fi

for name in $files; do
  compose exec -T eduforge cat "$run_dir/$name" > "$staging/$name"
done
mkdir -p docs/evidence/compose
for name in $files; do
  cp "$staging/$name" docs/evidence/compose/
done
printf 'Saved evidence in %s\n' docs/evidence/compose
