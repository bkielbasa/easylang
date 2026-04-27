// runtime/ease_gc.h — Pluggable GC ABI for the Ease runtime.
//
// All implementations under runtime/gc_*.c must provide every symbol
// declared here. Build picks one impl via the `GC` make variable.
#ifndef EASE_GC_H
#define EASE_GC_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// Lifecycle
void  gc_init(void *stack_bottom);
void  gc_shutdown(void);

// Allocation
void *gc_alloc(size_t bytes);
void  gc_collect(void);

// Stats — always present, even when no real collection happens.
typedef struct {
    uint64_t bytes_allocated_total;   // monotonic
    uint64_t bytes_allocated_live;    // current
    uint64_t allocations_total;       // monotonic count
    uint64_t collections;             // # of GC cycles run
    uint64_t collection_ns_total;     // sum of pause times
    uint64_t collection_ns_max;
    uint64_t bytes_freed_total;       // monotonic
    uint64_t peak_live_bytes;         // high-water mark
} gc_stats_t;

void gc_get_stats(gc_stats_t *out);
void gc_print_stats(int fd);

// Each impl defines this; gc_print_stats reads it.
extern const char *gc_impl_name;

#ifdef __cplusplus
}
#endif

#endif // EASE_GC_H
