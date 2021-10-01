#!/bin/bash

set -exv

make test
TEST_RESULT=$?

exit 0
