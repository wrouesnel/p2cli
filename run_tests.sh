#!/bin/bash
# Integration tests of p2 command line

. ./wvtest.sh

# Test basic input/output and extension inference works
WVPASSEQ "$(./p2 -i tests/data.env -t tests/data.p2)" "$(cat tests/data.out)"
WVPASSEQ "$(./p2 -i tests/data.json -t tests/data.p2)" "$(cat tests/data.out)"
WVPASSEQ "$(./p2 -i tests/data.yml -t tests/data.p2)" "$(cat tests/data.out)"

# Test writing to a file works
WVPASS ./p2 -i tests/data.env -t tests/data.p2 -o tests/data.test
WVPASSEQ "$(cat tests/data.out)" "$(cat tests/data.test)"
