#!/bin/bash
# Test runner for Ease compiler integration tests

EASE="./ease"
TESTS_DIR="tests"
PASSED=0
FAILED=0
FAILED_TESTS=()

echo "================================"
echo "  Ease Compiler Test Suite"
echo "================================"
echo

# Run from project root
cd "$(dirname "$0")/.."

for test_file in $(ls ${TESTS_DIR}/*.ease | sort); do
    test_name=$(basename "$test_file" .ease)
    echo -n "Running $test_name... "

    if $EASE run "$test_file" > /dev/null 2>&1; then
        echo "✓ PASS"
        ((PASSED++))
    else
        exit_code=$?
        echo "✗ FAIL (exit code: $exit_code)"
        FAILED_TESTS+=("$test_name (exit: $exit_code)")
        ((FAILED++))
    fi
done

echo
echo "================================"
echo "Results: $PASSED passed, $FAILED failed"

if [ $FAILED -gt 0 ]; then
    echo
    echo "Failed tests:"
    for test in "${FAILED_TESTS[@]}"; do
        echo "  - $test"
    done
fi

echo "================================"

if [ $FAILED -eq 0 ]; then
    exit 0
else
    exit 1
fi
