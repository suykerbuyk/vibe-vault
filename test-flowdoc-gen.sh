#!/bin/bash

set -e

START_DIR="$(pwd)"
CODE_DIR='/home/johns/code'
RPT_FILE="${START_DIR}/flowgen-test-results.md"

truncate -s 0 "${RPT_FILE}"

run_test() {
    PRJ_NAME="$1"
    PRJ_DIR="${CODE_DIR}/${PRJ_NAME}"
    cd "${PRJ_DIR}"
    echo "## Testing flowdoc gen for project ${PRJ_NAME}" | tee -a "${RPT_FILE}"
    echo " - Project directory: ${PRJ_DIR}" | tee -a "${RPT_FILE}"
    echo "### Begin testing of ${PRJ_NAME}" | tee -a "${RPT_FILE}"
    echo "\`vv flowdoc gen\`" | tee -a "${RPT_FILE}"
    vv flowdoc gen | while read line; do
        echo "> $line" | tee -a "${RPT_FILE}"
    done
    echo "" | tee -a "${RPT_FILE}"
    echo "\`vv flowdoc verify\`" | tee -a "${RPT_FILE}"
    vv flowdoc verify | while read line; do
        echo "> $line" | tee -a "${RPT_FILE}"
    done
    echo "### End testing of ${PRJ_NAME}" | tee -a "${RPT_FILE}"
    echo "---" | tee -a "${RPT_FILE}"
}

for PRJ in "cando-rs" "recmeet" "rezbldr" "vibe-vault" "vibe-palace"; do
    run_test "${PRJ}"
done
