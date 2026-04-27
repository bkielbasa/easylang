// runtime/gc_conservative.c — Stop-the-world conservative mark-sweep GC.
//
// Allocator threads a header inline before each block. gc_collect runs
// mark (root scanning of stack + globals + ease_test_jmp_buf, tri-color
// worklist trace) then sweep (free unmarked, reset marks). gc_alloc
// auto-triggers collection when bytes_live + bytes > threshold; threshold
// resizes after each cycle to max(1 MB, 2 * live_after).
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

// Inline helpers — must appear before any function that calls them.
static inline void *header_to_payload(gc_header_t *h) {
    return (void *)(h + 1);
}

static inline gc_header_t *payload_to_header(void *p) {
    return ((gc_header_t *)p) - 1;
}

// Worklist: pre-allocated outside the GC heap (uses raw malloc/realloc/free).
typedef struct {
    gc_header_t **items;
    size_t        len;
    size_t        cap;
} worklist_t;

static worklist_t g_worklist = {0};

// Reference to the compiler-emitted globals table (defined in user IR).
extern void   *gc_globals[];
extern int64_t gc_globals_count;

// External symbol for the test_jmp_buf used by t.Fatal — also a root.
extern int64_t ease_test_jmp_buf[];

static void worklist_push(gc_header_t *h) {
    if (g_worklist.len == g_worklist.cap) {
        size_t new_cap = g_worklist.cap == 0 ? 64 : g_worklist.cap * 2;
        gc_header_t **new_items = (gc_header_t **)realloc(
            g_worklist.items, new_cap * sizeof(gc_header_t *));
        if (!new_items) abort();
        g_worklist.items = new_items;
        g_worklist.cap   = new_cap;
    }
    g_worklist.items[g_worklist.len++] = h;
}

static gc_header_t *worklist_pop(void) {
    if (g_worklist.len == 0) return NULL;
    return g_worklist.items[--g_worklist.len];
}

// Sorted index of allocations for O(log N) find_owner. Rebuilt once at the
// start of every gc_collect, then queried millions of times during mark.
// Lives outside the GC heap (uses raw realloc/free), so collection cannot
// recurse on itself.
typedef struct {
    uintptr_t    payload_start;
    uintptr_t    payload_end;
    gc_header_t *header;
} alloc_index_t;

static alloc_index_t *g_index_items = NULL;
static size_t         g_index_cap   = 0;
static size_t         g_index_len   = 0;

static int alloc_index_cmp(const void *a, const void *b) {
    uintptr_t pa = ((const alloc_index_t *)a)->payload_start;
    uintptr_t pb = ((const alloc_index_t *)b)->payload_start;
    if (pa < pb) return -1;
    if (pa > pb) return 1;
    return 0;
}

static void alloc_index_rebuild(void) {
    size_t count = 0;
    gc_header_t *h = g_state.head;
    while (h) { count++; h = h->next; }
    if (count > g_index_cap) {
        size_t new_cap = g_index_cap == 0 ? 64 : g_index_cap;
        while (new_cap < count) new_cap *= 2;
        alloc_index_t *new_items = (alloc_index_t *)realloc(
            g_index_items, new_cap * sizeof(alloc_index_t));
        if (!new_items) abort();
        g_index_items = new_items;
        g_index_cap   = new_cap;
    }
    h = g_state.head;
    size_t i = 0;
    while (h) {
        uintptr_t ps = (uintptr_t)header_to_payload(h);
        g_index_items[i].payload_start = ps;
        g_index_items[i].payload_end   = ps + h->size;
        g_index_items[i].header        = h;
        i++;
        h = h->next;
    }
    g_index_len = count;
    qsort(g_index_items, count, sizeof(alloc_index_t), alloc_index_cmp);
}

// Binary search the sorted index. O(log N) per query.
static gc_header_t *find_owner(uint64_t candidate) {
    if (g_index_len == 0) return NULL;
    // Find largest entry with payload_start <= candidate.
    size_t lo = 0, hi = g_index_len;
    while (lo < hi) {
        size_t mid = (lo + hi) / 2;
        if (g_index_items[mid].payload_start <= candidate) {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    if (lo == 0) return NULL;
    alloc_index_t *e = &g_index_items[lo - 1];
    if (candidate >= e->payload_start && candidate < e->payload_end) {
        return e->header;
    }
    return NULL;
}

static void try_mark(uint64_t candidate) {
    gc_header_t *h = find_owner(candidate);
    if (h && !h->mark) {
        h->mark = 1;
        worklist_push(h);
    }
}

static void scan_range(void *lo, void *hi) {
    if (lo == NULL || hi == NULL) return;
    if ((uintptr_t)lo > (uintptr_t)hi) {
        void *t = lo; lo = hi; hi = t;
    }
    uint64_t *p   = (uint64_t *)((uintptr_t)lo & ~(uintptr_t)7);   // 8-byte align down
    uint64_t *end = (uint64_t *)((uintptr_t)hi & ~(uintptr_t)7);
    while (p < end) {
        try_mark(*p);
        p++;
    }
}

static void mark_all_roots(void) {
    // 1. Stack — from current local up to recorded stack_bottom.
    //    setjmp first to flush callee-saved registers into reg_buf.
    //    Cast to void: we use setjmp purely for the register-flush side
    //    effect; longjmp is never aimed back at this site.
    jmp_buf reg_buf;
    (void)setjmp(reg_buf); // NOLINT(bugprone-unused-return-value)
    void *stack_top;
    void *stack_top_addr = &stack_top;
    scan_range(stack_top_addr, g_state.stack_bottom);
    scan_range((void *)reg_buf, (void *)((char *)reg_buf + sizeof(reg_buf)));

    // 2. Globals — gc_globals[i] is the address of one i64 word.
    for (int64_t i = 0; i < gc_globals_count; i++) {
        try_mark(*(uint64_t *)gc_globals[i]);
    }

    // 3. test_jmp_buf — 37 i64s on macOS.
    scan_range((void *)ease_test_jmp_buf,
               (void *)((char *)ease_test_jmp_buf + 37 * 8));
}

static void trace(void) {
    while (g_worklist.len > 0) {
        gc_header_t *h = worklist_pop();
        scan_range(header_to_payload(h),
                   (void *)((char *)header_to_payload(h) + h->size));
    }
}

static void sweep(void) {
    gc_header_t **link = &g_state.head;
    gc_header_t *h = g_state.head;
    while (h) {
        gc_header_t *next = h->next;
        if (h->mark) {
            h->mark = 0;
            link = &h->next;
        } else {
            *link = next;
            gc_stats_record_free(h->size);
            free(h);
        }
        h = next;
    }
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
    if (g_gc_stats.bytes_allocated_live + bytes > g_state.threshold &&
        !g_state.collecting) {
        gc_collect();
    }
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
    if (g_state.collecting) return;
    g_state.collecting = 1;
    uint64_t t0 = gc_now_ns();

    // Build the sorted owner-index once per cycle so find_owner is O(log N).
    alloc_index_rebuild();

    // Mark
    g_worklist.len = 0;
    mark_all_roots();
    trace();

    // Sweep
    sweep();

    // Adjust threshold: max(1 MB, 2 * live_after).
    size_t live_after = g_gc_stats.bytes_allocated_live;
    size_t doubled    = live_after * 2;
    g_state.threshold = doubled < (1u << 20) ? (1u << 20) : doubled;

    uint64_t pause = gc_now_ns() - t0;
    gc_stats_record_collection(pause);
    g_state.collecting = 0;
}
