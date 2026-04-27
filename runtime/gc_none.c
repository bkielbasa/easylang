// runtime/gc_none.c — Passthrough impl: gc_alloc = malloc(zeroed), gc_collect = noop.
//
// Useful as a baseline for benchmarks (measures overhead added by tracking
// alone vs. tracking + actual collection).
#include "ease_gc.h"
#include "gc_stats.h"

#include <stdlib.h>
#include <string.h>

const char *gc_impl_name = "none";

void gc_init(void *stack_bottom) {
    (void)stack_bottom;
    g_gc_init_ns = gc_now_ns();
    gc_stats_install_atexit();
}

void gc_shutdown(void) {
    // nothing to release; libc cleans up on exit
}

void *gc_alloc(size_t bytes) {
    // Promote zero-byte requests to 1 so callers always get a unique
    // non-null pointer; stats record the actual byte count we allocated.
    if (bytes == 0) bytes = 1;
    void *p = malloc(bytes);
    if (!p) {
        // Out of memory — abort. Ease has no exception model.
        abort();
    }
    memset(p, 0, bytes);
    gc_stats_record_alloc(bytes);
    return p;
}

void gc_collect(void) {
    // No-op for the passthrough impl. Stats unchanged.
}
