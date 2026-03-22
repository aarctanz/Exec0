#!/bin/bash
set -e

# Create isolate's runtime directories
mkdir -p /run/isolate/locks

# Set up the cgroup subtree for isolate
# isolate reads /run/isolate/cgroup as a file containing the cgroup path
CGROUP_BASE="/sys/fs/cgroup"

if [ -d "$CGROUP_BASE" ]; then
    # Remount cgroup filesystem read-write if it's mounted read-only
    # (common on systemd-based hosts even with privileged containers)
    mount -o remount,rw "$CGROUP_BASE" 2>/dev/null || true

    ISOLATE_CG="$CGROUP_BASE/isolate"
    mkdir -p "$ISOLATE_CG"

    # On systemd hosts, the worker process may live in a leaf cgroup that
    # prevents enabling subtree_control on the root. Move our process to
    # a dedicated init cgroup so the root becomes an inner node.
    if [ -f "$CGROUP_BASE/cgroup.procs" ]; then
        INIT_CG="$CGROUP_BASE/init"
        mkdir -p "$INIT_CG"
        echo $$ > "$INIT_CG/cgroup.procs" 2>/dev/null || true
    fi

    # Enable controllers at every level in the hierarchy
    # Root -> isolate -> box-N (isolate creates box-N itself)
    for ctl_file in "$CGROUP_BASE/cgroup.subtree_control" "$ISOLATE_CG/cgroup.subtree_control"; do
        if [ -f "$ctl_file" ]; then
            echo "+cpu +memory +pids" > "$ctl_file" || {
                echo "WARNING: failed to enable controllers in $ctl_file" >&2
                # Try enabling one at a time — some controllers may not be available
                for ctl in cpu memory pids; do
                    echo "+$ctl" > "$ctl_file" 2>/dev/null || \
                        echo "WARNING: failed to enable +$ctl in $ctl_file" >&2
                done
            }
        fi
    done

    # Verify controllers are active
    if [ -f "$ISOLATE_CG/cgroup.subtree_control" ]; then
        ACTIVE=$(cat "$ISOLATE_CG/cgroup.subtree_control")
        echo "isolate cgroup controllers: $ACTIVE"
        for required in cpu memory pids; do
            if ! echo "$ACTIVE" | grep -qw "$required"; then
                echo "ERROR: required controller '$required' not active in isolate cgroup" >&2
            fi
        done
    fi

    # Write the cgroup path to the file isolate expects
    echo "$ISOLATE_CG" > /run/isolate/cgroup
fi

exec "$@"
