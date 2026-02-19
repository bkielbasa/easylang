// Ease language runtime library
// Provides string, array, IO, conversion, and map operations for LLVM IR backend.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/stat.h>
#include <dirent.h>
#include <setjmp.h>
#include <time.h>

// ============================================================================
// String operations (null-terminated C strings, represented as char*)
// ============================================================================

long ease_str_eq(const char *a, const char *b) {
    if (a == b) return 1;
    if (!a || !b) return 0;
    return strcmp(a, b) == 0 ? 1 : 0;
}

long ease_str_ne(const char *a, const char *b) {
    return !ease_str_eq(a, b);
}

long ease_str_len(const char *s) {
    if (!s) return 0;
    return (long)strlen(s);
}

char *ease_str_concat(const char *a, const char *b) {
    if (!a) a = "";
    if (!b) b = "";
    size_t la = strlen(a);
    size_t lb = strlen(b);
    char *result = (char *)malloc(la + lb + 1);
    memcpy(result, a, la);
    memcpy(result + la, b, lb);
    result[la + lb] = '\0';
    return result;
}

char *ease_str_slice(const char *s, long start, long end) {
    if (!s) return strdup("");
    long slen = (long)strlen(s);
    if (start < 0) start = 0;
    if (end > slen) end = slen;
    if (start >= end) return strdup("");
    long len = end - start;
    char *result = (char *)malloc(len + 1);
    memcpy(result, s + start, len);
    result[len] = '\0';
    return result;
}

long ease_load_byte(const char *s, long index) {
    if (!s) return 0;
    return (long)(unsigned char)s[index];
}

long ease_str_contains(const char *haystack, const char *needle) {
    if (!haystack || !needle) return 0;
    return strstr(haystack, needle) != NULL ? 1 : 0;
}

long ease_str_starts_with(const char *s, const char *prefix) {
    if (!s || !prefix) return 0;
    size_t plen = strlen(prefix);
    return strncmp(s, prefix, plen) == 0 ? 1 : 0;
}

long ease_str_ends_with(const char *s, const char *suffix) {
    if (!s || !suffix) return 0;
    size_t slen = strlen(s);
    size_t suflen = strlen(suffix);
    if (suflen > slen) return 0;
    return strcmp(s + slen - suflen, suffix) == 0 ? 1 : 0;
}

long ease_str_index_of(const char *s, const char *substr) {
    if (!s || !substr) return -1;
    const char *p = strstr(s, substr);
    if (!p) return -1;
    return (long)(p - s);
}

char *ease_str_substring(const char *s, long start, long end) {
    return ease_str_slice(s, start, end);
}

long ease_str_char_at(const char *s, long index) {
    if (!s) return 0;
    long slen = (long)strlen(s);
    if (index < 0 || index >= slen) return 0;
    return (long)(unsigned char)s[index];
}

char *ease_str_trim(const char *s, const char *chars) {
    if (!s) return strdup("");
    if (!chars) return strdup(s);
    size_t slen = strlen(s);
    size_t start = 0;
    size_t end = slen;
    while (start < end && strchr(chars, s[start])) start++;
    while (end > start && strchr(chars, s[end - 1])) end--;
    size_t len = end - start;
    char *result = (char *)malloc(len + 1);
    memcpy(result, s + start, len);
    result[len] = '\0';
    return result;
}

char *ease_str_replace(const char *s, const char *old, const char *new_str) {
    if (!s || !old || !new_str) return strdup(s ? s : "");
    size_t old_len = strlen(old);
    size_t new_len = strlen(new_str);
    if (old_len == 0) return strdup(s);

    // Count occurrences
    int count = 0;
    const char *p = s;
    while ((p = strstr(p, old)) != NULL) {
        count++;
        p += old_len;
    }
    if (count == 0) return strdup(s);

    size_t result_len = strlen(s) + count * ((long)new_len - (long)old_len);
    char *result = (char *)malloc(result_len + 1);
    char *dst = result;
    p = s;
    while (1) {
        const char *found = strstr(p, old);
        if (!found) {
            strcpy(dst, p);
            break;
        }
        size_t chunk = found - p;
        memcpy(dst, p, chunk);
        dst += chunk;
        memcpy(dst, new_str, new_len);
        dst += new_len;
        p = found + old_len;
    }
    return result;
}

// Array fat pointer: { i8* ptr, i64 len, i64 cap }
// This struct is 24 bytes, passed by pointer from LLVM IR.
typedef struct {
    char *ptr;
    long len;
    long cap;
} EaseArray;

// ease_str_split splits s by sep and returns a fat pointer to an array of char*
// The result is written into the out_arr parameter (pointer to EaseArray).
void ease_str_split(EaseArray *out_arr, const char *s, const char *sep) {
    if (!s || !sep || strlen(sep) == 0) {
        // Return array with single element
        char **ptrs = (char **)malloc(sizeof(char *));
        ptrs[0] = strdup(s ? s : "");
        out_arr->ptr = (char *)ptrs;
        out_arr->len = 1;
        out_arr->cap = 1;
        return;
    }

    size_t sep_len = strlen(sep);
    // Count splits
    int count = 1;
    const char *p = s;
    while ((p = strstr(p, sep)) != NULL) {
        count++;
        p += sep_len;
    }

    char **ptrs = (char **)malloc(count * sizeof(char *));
    p = s;
    int i = 0;
    while (1) {
        const char *found = strstr(p, sep);
        if (!found) {
            ptrs[i++] = strdup(p);
            break;
        }
        size_t chunk = found - p;
        ptrs[i] = (char *)malloc(chunk + 1);
        memcpy(ptrs[i], p, chunk);
        ptrs[i][chunk] = '\0';
        i++;
        p = found + sep_len;
    }

    out_arr->ptr = (char *)ptrs;
    out_arr->len = count;
    out_arr->cap = count;
}

// ============================================================================
// Array operations
// ============================================================================

// ease_array_push pushes an element to a dynamic array (fat pointer).
// arr_ptr points to an EaseArray struct. elem_ptr points to the element to copy.
// elem_size is the size of each element in bytes.
void ease_array_push(EaseArray *arr, const void *elem_ptr, long elem_size) {
    if (arr->len >= arr->cap) {
        long new_cap = arr->cap == 0 ? 4 : arr->cap * 2;
        char *new_ptr = (char *)malloc(new_cap * elem_size);
        if (arr->ptr && arr->len > 0) {
            memcpy(new_ptr, arr->ptr, arr->len * elem_size);
        }
        // Don't free old ptr - no GC yet, matching current behavior
        arr->ptr = new_ptr;
        arr->cap = new_cap;
    }
    memcpy(arr->ptr + arr->len * elem_size, elem_ptr, elem_size);
    arr->len++;
}

// ============================================================================
// IO operations
// ============================================================================

void ease_print(const char *s) {
    if (s) {
        fputs(s, stdout);
    }
}

void ease_println(const char *s) {
    if (s) {
        puts(s);
    } else {
        puts("");
    }
}

char *ease_read_file(const char *path) {
    if (!path) return strdup("");
    FILE *f = fopen(path, "rb");
    if (!f) return strdup("");
    fseek(f, 0, SEEK_END);
    long size = ftell(f);
    fseek(f, 0, SEEK_SET);
    char *buf = (char *)malloc(size + 1);
    fread(buf, 1, size, f);
    buf[size] = '\0';
    fclose(f);
    return buf;
}

long ease_write_file(const char *path, const char *content) {
    if (!path || !content) return -1;
    FILE *f = fopen(path, "wb");
    if (!f) return -1;
    size_t len = strlen(content);
    size_t written = fwrite(content, 1, len, f);
    fclose(f);
    return written == len ? 0 : -1;
}

// ============================================================================
// Conversion operations
// ============================================================================

char *ease_int_to_str(long n) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%ld", n);
    return strdup(buf);
}

long ease_str_to_int(const char *s) {
    if (!s) return 0;
    return atol(s);
}

// ============================================================================
// Memory operations
// ============================================================================

void ease_poke(char *addr, long value) {
    *addr = (char)value;
}

long ease_peek(const char *addr) {
    return (long)(unsigned char)*addr;
}

void ease_memset(char *addr, long value, long count) {
    memset(addr, (int)value, (size_t)count);
}

// ============================================================================
// Syscall wrappers
// ============================================================================

long ease_syscall_open(const char *path, long flags, long mode) {
    return (long)open(path, (int)flags, (int)mode);
}

long ease_syscall_read(long fd, char *buf, long size) {
    return (long)read((int)fd, buf, (size_t)size);
}

long ease_syscall_write(long fd, const char *buf, long size) {
    return (long)write((int)fd, buf, (size_t)size);
}

long ease_syscall_close(long fd) {
    return (long)close((int)fd);
}

// ============================================================================
// Directory operations
// ============================================================================

long ease_is_dir(const char *path) {
    if (!path) return 0;
    struct stat st;
    if (stat(path, &st) != 0) return 0;
    return S_ISDIR(st.st_mode) ? 1 : 0;
}

char* ease_list_dir(const char *path) {
    if (!path) return strdup("");
    DIR *d = opendir(path);
    if (!d) return strdup("");
    char *result = strdup("");
    struct dirent *entry;
    while ((entry = readdir(d)) != NULL) {
        if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0) continue;
        size_t rlen = strlen(result);
        size_t nlen = strlen(entry->d_name);
        char *new_result = (char *)malloc(rlen + nlen + 2);
        memcpy(new_result, result, rlen);
        memcpy(new_result + rlen, entry->d_name, nlen);
        new_result[rlen + nlen] = '\n';
        new_result[rlen + nlen + 1] = '\0';
        free(result);
        result = new_result;
    }
    closedir(d);
    return result;
}

// ============================================================================
// Map operations (simple hash map with linear probing)
// ============================================================================

#define MAP_INIT_CAP 16
#define MAP_LOAD_FACTOR 0.75

typedef struct {
    char *key;       // NULL = empty slot
    long value;
    int occupied;
} MapEntry;

typedef struct {
    MapEntry *entries;
    long len;
    long cap;
} EaseMap;

static unsigned long hash_str(const char *s) {
    unsigned long hash = 5381;
    if (!s) return hash;
    int c;
    while ((c = *s++))
        hash = ((hash << 5) + hash) + c;
    return hash;
}

static unsigned long hash_int(long key) {
    unsigned long k = (unsigned long)key;
    k = (k ^ (k >> 30)) * 0xbf58476d1ce4e5b9ULL;
    k = (k ^ (k >> 27)) * 0x94d049bb133111ebULL;
    k = k ^ (k >> 31);
    return k;
}

void *ease_map_new(long key_size, long val_size) {
    (void)key_size;
    (void)val_size;
    EaseMap *m = (EaseMap *)calloc(1, sizeof(EaseMap));
    m->cap = MAP_INIT_CAP;
    m->entries = (MapEntry *)calloc(m->cap, sizeof(MapEntry));
    return m;
}

static long map_find_slot(EaseMap *m, long key) {
    unsigned long h = hash_int(key);
    long idx = (long)(h % (unsigned long)m->cap);
    for (long i = 0; i < m->cap; i++) {
        long slot = (idx + i) % m->cap;
        if (!m->entries[slot].occupied) return slot;
        if (m->entries[slot].key == (char *)key) return slot;
    }
    return -1; // Should never happen if load factor is maintained
}

static void map_resize(EaseMap *m) {
    long old_cap = m->cap;
    MapEntry *old = m->entries;
    m->cap *= 2;
    m->entries = (MapEntry *)calloc(m->cap, sizeof(MapEntry));
    m->len = 0;
    for (long i = 0; i < old_cap; i++) {
        if (old[i].occupied) {
            long slot = map_find_slot(m, (long)old[i].key);
            m->entries[slot].key = old[i].key;
            m->entries[slot].value = old[i].value;
            m->entries[slot].occupied = 1;
            m->len++;
        }
    }
    free(old);
}

long ease_map_get(void *map_ptr, long key) {
    EaseMap *m = (EaseMap *)map_ptr;
    if (!m || m->len == 0) return 0;
    long slot = map_find_slot(m, key);
    if (slot >= 0 && m->entries[slot].occupied) {
        return m->entries[slot].value;
    }
    return 0;
}

void ease_map_set(void *map_ptr, long key, long value) {
    EaseMap *m = (EaseMap *)map_ptr;
    if (!m) return;
    if ((double)m->len / (double)m->cap >= MAP_LOAD_FACTOR) {
        map_resize(m);
    }
    long slot = map_find_slot(m, key);
    if (slot < 0) return;
    if (!m->entries[slot].occupied) {
        m->len++;
    }
    m->entries[slot].key = (char *)key;
    m->entries[slot].value = value;
    m->entries[slot].occupied = 1;
}

void ease_map_delete(void *map_ptr, long key) {
    EaseMap *m = (EaseMap *)map_ptr;
    if (!m || m->len == 0) return;
    long slot = map_find_slot(m, key);
    if (slot >= 0 && m->entries[slot].occupied) {
        m->entries[slot].occupied = 0;
        m->entries[slot].key = NULL;
        m->entries[slot].value = 0;
        m->len--;
    }
}

long ease_map_len(void *map_ptr) {
    EaseMap *m = (EaseMap *)map_ptr;
    if (!m) return 0;
    return m->len;
}

// ============================================================================
// Testing support (setjmp/longjmp for test failure recovery)
// ============================================================================

// Exposed as external global so LLVM IR can call setjmp() directly on it.
// setjmp must be called from the function whose stack frame persists
// through the test — wrapping it in a helper would invalidate the jmp_buf.
jmp_buf ease_test_jmp_buf;

void ease_test_fail(void) {
    longjmp(ease_test_jmp_buf, 1);
}

long ease_time_nanos(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long)ts.tv_sec * 1000000000L + (long)ts.tv_nsec;
}

// ============================================================================
// Program entry point wrapper
// Saves argc/argv, then calls ease_main (the user's main function).
// ============================================================================

// Globals for argc/argv access
static int g_argc;
static char **g_argv;

long ease_argc(void) {
    return (long)g_argc;
}

char *ease_argv(long index) {
    if (index < 0 || index >= g_argc) return "";
    return g_argv[index];
}

// The user's main function compiled by Ease, renamed to ease_main
extern long ease_main(void);

int main(int argc, char **argv) {
    g_argc = argc;
    g_argv = argv;
    long result = ease_main();
    return (int)result;
}
