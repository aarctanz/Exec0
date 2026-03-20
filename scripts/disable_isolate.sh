#!/usr/bin/env bash
set -e

echo "Restoring normal system environment..."

# Enable SMT
echo on | sudo tee /sys/devices/system/cpu/smt/control

# Restore CPU governor
sudo cpupower frequency-set -g schedutil

# Enable ASLR
echo 2 | sudo tee /proc/sys/kernel/randomize_va_space

# Restore Transparent Huge Pages
echo always | sudo tee /sys/kernel/mm/transparent_hugepage/enabled
echo always | sudo tee /sys/kernel/mm/transparent_hugepage/defrag
echo 1 | sudo tee /sys/kernel/mm/transparent_hugepage/khugepaged/defrag

# Restore system core dump handler
echo '|/usr/lib/systemd/systemd-coredump %P %u %g %s %t %e' | sudo tee /proc/sys/kernel/core_pattern


echo "System restored."
