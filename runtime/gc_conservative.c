// runtime/gc_conservative.c — Stop-the-world conservative mark-sweep GC.
//
// v1: allocator + tracking only. mark/sweep added in subsequent tasks.
#include "ease_gc.h"
#include "gc_stats.h"

#include <setjmp.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

const char *gc_impl_name = "conservative";

// Header threaded inline before each allocation.
typedef struct gc_header {
    struct gc_header *next;
    size_t            size;     // payload bytes (excludes header)
    uint8_t           mark;
    uint8_t           _pad[7];  // align payload to 16 bytes
} gc_header_t;

typedef struct {
    gc_header_t *head;          // singly linked list of all live allocs
    void        *stack_bottom;  // captured at gc_init
    int          collecting;    // recursion guard
    size_t       threshold;     // bytes_live trigger threshold
} gc_state_t;

static gc_state_t g_state = {0};

static inline void *header_to_payload(gc_header_t *h) {
    return (void *)(h + 1);
}

static inline gc_header_t *payload_to_header(void *p) {
    return ((gc_header_t *)p) - 1;
}

void gc_init(void *stack_bottom) {
    g_state.head         = NULL;
    g_state.stack_bottom = stack_bottom;
    g_state.collecting   = 0;
    g_state.threshold    = 1 << 20;  // 1 MB initial
    g_gc_init_ns         = gc_now_ns();
    gc_stats_install_atexit();
}

void gc_shutdown(void) {
    // Free everything still tracked (best-effort cleanup).
    gc_header_t *h = g_state.head;
    while (h) {
        gc_header_t *next = h->next;
        free(h);
        h = next;
    }
    g_state.head = NULL;
}

void *gc_alloc(size_t bytes) {
    if (bytes == 0) bytes = 1;
    gc_header_t *h = (gc_header_t *)malloc(sizeof(gc_header_t) + bytes);
    if (!h) abort();
    h->next = g_state.head;
    h->size = bytes;
    h->mark = 0;
    g_state.head = h;
    void *payload = header_to_payload(h);
    memset(payload, 0, bytes);
    gc_stats_record_alloc(bytes);
    return payload;
}

void gc_collect(void) {
    // Stub: no marking, no sweeping yet. Just record that we ran.
    if (g_state.collecting) return;
    g_state.collecting = 1;
    uint64_t t0 = gc_now_ns();
    // (mark + sweep go here in the next task)
    uint64_t pause = gc_now_ns() - t0;
    gc_stats_record_collection(pause);
    g_state.collecting = 0;
}
