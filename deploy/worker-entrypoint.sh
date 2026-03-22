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

    # Enable controllers at every level in the hierarchy
    # Root -> isolate subtree
    for ctl_file in "$CGROUP_BASE/cgroup.subtree_control" "$ISOLATE_CG/cgroup.subtree_control"; do
        if [ -f "$ctl_file" ]; then
            echo "+cpu +memory +pids" > "$ctl_file" 2>/dev/null || true
        fi
    done

    # Write the cgroup path to the file isolate expects
    echo "$ISOLATE_CG" > /run/isolate/cgroup
fi

exec "$@"
