// runtime/gc_stats.h — Internal counter storage shared by all GC impls.
#ifndef EASE_GC_STATS_H
#define EASE_GC_STATS_H

#include "ease_gc.h"

extern gc_stats_t g_gc_stats;
extern uint64_t   g_gc_init_ns;        // for elapsed_ns reporting

void gc_stats_record_alloc(size_t bytes);
void gc_stats_record_free(size_t bytes);
void gc_stats_record_collection(uint64_t pause_ns);
void gc_stats_install_atexit(void);    // called by gc_init

uint64_t gc_now_ns(void);              // CLOCK_MONOTONIC, ns

#endif // EASE_GC_STATS_H
