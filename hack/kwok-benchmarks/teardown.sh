#!/usr/bin/env bash
#
# Removes anything a run may have left behind. run.sh deletes the VM itself on
# every exit path, so this is for the cases where it could not: an interrupted
# run with KEEP_VM=1, or a run.sh that was killed outright.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib.sh"
require_vm_config
require gcloud

if gc compute instances describe "$VM_NAME" --zone "$ZONE" >/dev/null 2>&1; then
  log "deleting $VM_NAME in $ZONE"
  gc compute instances delete "$VM_NAME" --zone "$ZONE" --quiet
else
  log "$VM_NAME does not exist in $ZONE"
fi

# Left over from the ssh-based version of this harness; harmless if absent.
if gc compute firewall-rules describe "${VM_NAME}-iap-ssh" >/dev/null 2>&1; then
  log "deleting stale firewall rule ${VM_NAME}-iap-ssh"
  gc compute firewall-rules delete "${VM_NAME}-iap-ssh" --quiet
fi

log "done (results in $BUCKET are left alone - they are cheap and worth keeping)"
