#!/usr/bin/env bash
#
# Runs one benchmark end to end: creates a VM whose startup-script is the whole
# benchmark, watches its progress through GCS, pulls the results back, and
# deletes the VM.
#
# There is no ssh anywhere in here - see the comment in lib.sh for why. The VM
# only ever makes outbound calls, so nothing needs to be reachable and no
# credential of yours needs to still be valid when it finishes.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib.sh"
require_vm_config
require gcloud

RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$PROFILE}"
DEST="$BUCKET/$RUN_ID"

on_exit() {
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    log "run.sh exited with status $rc"
    log "  the VM may still be running the benchmark - it reports to $DEST"
    log "  watch:   gcloud storage cat $DEST/remote.log"
    log "  collect: gcloud storage cp -r $DEST/results '$RESULTS_DIR' && $HERE/collect.sh"
  fi
  if [[ "$KEEP_VM" != "1" ]]; then
    if gc compute instances describe "$VM_NAME" --zone "$ZONE" >/dev/null 2>&1; then
      log "deleting $VM_NAME"
      gc compute instances delete "$VM_NAME" --zone "$ZONE" --quiet >/dev/null 2>&1 \
        || log "delete FAILED - remove $VM_NAME by hand, it is still billing"
    fi
  else
    log "KEEP_VM=1 - $VM_NAME is STILL RUNNING and billing; ./teardown.sh removes it"
  fi
  return $rc
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

log "results bucket: $DEST"
# gcloud storage, not gsutil: gsutil authenticates through its own legacy
# credential store and fails with "Your credentials are invalid" even when
# gcloud itself is perfectly well authenticated.
gc storage buckets describe "$BUCKET" >/dev/null 2>&1 || {
  log "creating bucket $BUCKET"
  gc storage buckets create "$BUCKET" --location="${ZONE%-*}"
}

# Config is baked into the startup script rather than read from the metadata
# server: fewer moving parts inside the VM, and the exact script that ran is
# recoverable from the instance metadata afterwards.
STARTUP="$(mktemp)"
trap 'rm -f "$STARTUP"' RETURN 2>/dev/null || true
{
  echo '#!/usr/bin/env bash'
  echo "export REPO='$REPO' REF='$REF' PROFILE='$PROFILE'"
  echo "export DURATION='$DURATION' SCRAPE_INTERVAL='$SCRAPE_INTERVAL'"
  echo "export CA_SCAN_INTERVAL='$CA_SCAN_INTERVAL'"
  echo "export KIND_VERSION='$KIND_VERSION' KIND_IMAGE='$KIND_IMAGE'"
  echo "export KWOK_VERSION='$KWOK_VERSION' GO_VERSION='$GO_VERSION'"
  echo "export WORKDIR='$REMOTE_DIR' GCS_DEST='$DEST'"
  echo "export DRA='$DRA' DRA_DEVICES_PER_NODE='$DRA_DEVICES_PER_NODE'"
  echo "export DRA_FLEET_NODES='$DRA_FLEET_NODES' DRA_FLEET_CLAIMS_PER_NODE='$DRA_FLEET_CLAIMS_PER_NODE'"
  # SWEEP is multi-line; single quotes carry the newlines through verbatim.
  [[ -n "${SWEEP:-}" ]] && echo "export SWEEP='$SWEEP'"
  tail -n +2 "$HERE/remote.sh"
} > "$STARTUP"

if gc compute instances describe "$VM_NAME" --zone "$ZONE" >/dev/null 2>&1; then
  fail "$VM_NAME already exists - delete it first (./teardown.sh) or set VM_NAME"
fi

log "creating $VM_NAME ($MACHINE_TYPE) with the benchmark as its startup-script"
gc compute instances create "$VM_NAME" \
  --zone "$ZONE" \
  --machine-type "$MACHINE_TYPE" \
  --boot-disk-size "$DISK_SIZE" \
  --boot-disk-type pd-ssd \
  --image-family "$IMAGE_FAMILY" \
  --image-project "$IMAGE_PROJECT" \
  --scopes cloud-platform \
  --metadata-from-file startup-script="$STARTUP" \
  --quiet >/dev/null

if [[ -n "${SWEEP:-}" ]]; then
  log "waiting for the sweep ($(printf '%s' "$SWEEP" | grep -c . ) configurations, ref=$REF, ${DURATION}s each)"
else
  log "waiting for the benchmark (profile=$PROFILE ref=$REF duration=${DURATION}s)"
fi
log "  progress is mirrored to $DEST/remote.log"

# Poll the mirrored log rather than holding a connection open. Nothing is lost
# if this loop is interrupted; the VM keeps going and the log keeps updating.
DEADLINE=$(( $(date +%s) + ${WAIT_TIMEOUT:-5400} ))
SEEN=0
while (( $(date +%s) < DEADLINE )); do
  if gc storage objects describe "$DEST/DONE" >/dev/null 2>&1; then
    log "benchmark reported DONE"
    break
  fi
  if gc storage objects describe "$DEST/FAILED" >/dev/null 2>&1; then
    gc storage cat "$DEST/remote.log" 2>/dev/null | tail -40 >&2
    fail "benchmark reported FAILED - full log at $DEST/remote.log"
  fi
  # Print only what is new since the last poll.
  if lines=$(gc storage cat "$DEST/remote.log" 2>/dev/null); then
    total=$(printf '%s\n' "$lines" | wc -l)
    if (( total > SEEN )); then
      printf '%s\n' "$lines" | tail -n +$(( SEEN + 1 )) | sed 's/^/  | /' >&2
      SEEN=$total
    fi
  fi
  sleep 20
done

gc storage objects describe "$DEST/DONE" >/dev/null 2>&1 || fail "timed out waiting for DONE - see $DEST/remote.log"

log "downloading results"
mkdir -p "$RESULTS_DIR"
rm -rf "${RESULTS_DIR:?}/"*
gc storage cp -r "$DEST/results/*" "$RESULTS_DIR/" >/dev/null 2>&1
gc storage cp "$DEST/remote.log" "$RESULTS_DIR/remote.log" >/dev/null 2>&1 || true

"$HERE/collect.sh"
