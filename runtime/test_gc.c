// runtime/test_gc.c — Smoke test the runtime in isolation, no compiler involved.
//
// Build:  cc -I runtime runtime/test_gc.c runtime/gc_stats.c runtime/gc_none.c -o tmp/test_gc
// Run:    EASE_GC_STATS=1 ./tmp/test_gc
#include "ease_gc.h"
#include "gc_stats.h"

#include <stdio.h>

int main(void) {
    int frame;
    gc_init(&frame);

    void *p1 = gc_alloc(64);
    void *p2 = gc_alloc(128);
    (void)p1; (void)p2;

    gc_stats_t s;
    gc_get_stats(&s);

    if (s.allocations_total != 2) {
        fprintf(stderr, "FAIL: expected 2 allocations, got %llu\n",
                (unsigned long long)s.allocations_total);
        return 1;
    }
    if (s.bytes_allocated_total != 64 + 128) {
        fprintf(stderr, "FAIL: expected 192 bytes, got %llu\n",
                (unsigned long long)s.bytes_allocated_total);
        return 1;
    }

    gc_collect();
    gc_get_stats(&s);
    // gc_none doesn't actually collect, so live bytes unchanged
    if (s.bytes_allocated_live != 64 + 128) {
        fprintf(stderr, "FAIL: gc_none should not free; live=%llu\n",
                (unsigned long long)s.bytes_allocated_live);
        return 1;
    }

    printf("test_gc: PASS\n");
    return 0;
}
