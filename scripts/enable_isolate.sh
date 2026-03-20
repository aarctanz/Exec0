#!/usr/bin/env bash
set -e

echo "Enabling isolate deterministic environment..."

# Disable SMT
echo off | sudo tee /sys/devices/system/cpu/smt/control

# Set CPU governor
sudo cpupower frequency-set -g performance

# Disable ASLR
echo 0 | sudo tee /proc/sys/kernel/randomize_va_space

# Disable Transparent Huge Pages
echo never | sudo tee /sys/kernel/mm/transparent_hugepage/enabled
echo never | sudo tee /sys/kernel/mm/transparent_hugepage/defrag
echo 0 | sudo tee /sys/kernel/mm/transparent_hugepage/khugepaged/defrag

# Core dumps should not be piped
echo core | sudo tee /proc/sys/kernel/core_pattern

echo "Isolate environment enabled."
