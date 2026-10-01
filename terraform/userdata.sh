#!/bin/bash
# Minimal userdata — just signal that instance booted.
# Actual setup is done via SSH in orchestrate.sh Phase 3.
touch /tmp/instance-booted
