// runtime/gc_stats.c — Shared stats counters and exit-time printer.
#include "gc_stats.h"

#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>

gc_stats_t g_gc_stats = {0};
uint64_t   g_gc_init_ns = 0;

uint64_t gc_now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ull + (uint64_t)ts.tv_nsec;
}

void gc_stats_record_alloc(size_t bytes) {
    g_gc_stats.bytes_allocated_total += bytes;
    g_gc_stats.bytes_allocated_live  += bytes;
    g_gc_stats.allocations_total     += 1;
    if (g_gc_stats.bytes_allocated_live > g_gc_stats.peak_live_bytes) {
        g_gc_stats.peak_live_bytes = g_gc_stats.bytes_allocated_live;
    }
}

void gc_stats_record_free(size_t bytes) {
    // Clamp against the live total — a miscount in a future collector
    // would otherwise underflow the unsigned counter and permanently
    // corrupt peak_live_bytes.
    if (bytes > g_gc_stats.bytes_allocated_live) {
        bytes = g_gc_stats.bytes_allocated_live;
    }
    g_gc_stats.bytes_freed_total    += bytes;
    g_gc_stats.bytes_allocated_live -= bytes;
}

void gc_stats_record_collection(uint64_t pause_ns) {
    g_gc_stats.collections          += 1;
    g_gc_stats.collection_ns_total  += pause_ns;
    if (pause_ns > g_gc_stats.collection_ns_max) {
        g_gc_stats.collection_ns_max = pause_ns;
    }
}

void gc_get_stats(gc_stats_t *out) {
    *out = g_gc_stats;
}

void gc_print_stats(FILE *out) {
    uint64_t elapsed = gc_now_ns() - g_gc_init_ns;
    fprintf(out,
        "gc_impl=%s\n"
        "gc_alloc_total_bytes=%" PRIu64 "\n"
        "gc_alloc_total_count=%" PRIu64 "\n"
        "gc_live_bytes_peak=%" PRIu64 "\n"
        "gc_live_bytes_final=%" PRIu64 "\n"
        "gc_collections=%" PRIu64 "\n"
        "gc_pause_ns_total=%" PRIu64 "\n"
        "gc_pause_ns_max=%" PRIu64 "\n"
        "gc_freed_bytes_total=%" PRIu64 "\n"
        "elapsed_ns=%" PRIu64 "\n",
        gc_impl_name,
        g_gc_stats.bytes_allocated_total,
        g_gc_stats.allocations_total,
        g_gc_stats.peak_live_bytes,
        g_gc_stats.bytes_allocated_live,
        g_gc_stats.collections,
        g_gc_stats.collection_ns_total,
        g_gc_stats.collection_ns_max,
        g_gc_stats.bytes_freed_total,
        elapsed);
}

static void gc_stats_atexit_handler(void) {
    if (getenv("EASE_GC_STATS")) {
        gc_print_stats(stderr);
    }
}

void gc_stats_install_atexit(void) {
    // Idempotent — guards against double-init from a future impl or test.
    static int installed = 0;
    if (installed) return;
    installed = 1;
    atexit(gc_stats_atexit_handler);
}
