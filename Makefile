# Ease Programming Language - Bootstrap Build System
#
# Build the Ease compiler from its seed (no external dependencies):
#   make
#
# Verify self-hosting (compiler compiles itself identically):
#   make verify
#
# Update the seed after modifying the compiler:
#   make update-seed
#
# Pick a GC implementation at build time:
#   make GC=none verify         # passthrough baseline
#   make GC=conservative verify # mark-sweep (default)

CC      ?= clang
CFLAGS  := -O1
GC      ?= none

SEED    := bootstrap/seed.ll
COMPILER_SRC := bootstrap/compiler.ease

BUILD_DIR := tmp
EASE      := $(BUILD_DIR)/ease

# Runtime objects — picked GC impl + always-linked stats
RUNTIME_DIR := runtime
GC_OBJ      := $(BUILD_DIR)/gc_$(GC).o
STATS_OBJ   := $(BUILD_DIR)/gc_stats.o
RUNTIME_OBJS := $(GC_OBJ) $(STATS_OBJ)

.PHONY: all clean verify update-seed test bench test-runtime

all: $(EASE)

# Build the compiler from the seed LLVM IR.
# The seed predates the GC; it doesn't reference any gc_* symbol so it
# does not need the runtime objects linked in.
$(EASE): $(SEED) | $(BUILD_DIR)
	$(CC) $(CFLAGS) $(SEED) -o $@

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Runtime object compile rules
$(BUILD_DIR)/gc_%.o: $(RUNTIME_DIR)/gc_%.c $(RUNTIME_DIR)/ease_gc.h $(RUNTIME_DIR)/gc_stats.h | $(BUILD_DIR)
	$(CC) $(CFLAGS) -I $(RUNTIME_DIR) -c $< -o $@

# Runtime smoke test (independent of the compiler)
test-runtime: $(BUILD_DIR)/test_gc
	@EASE_GC_STATS=1 $<

$(BUILD_DIR)/test_gc: $(RUNTIME_DIR)/test_gc.c $(STATS_OBJ) $(BUILD_DIR)/gc_none.o | $(BUILD_DIR)
	$(CC) $(CFLAGS) -I $(RUNTIME_DIR) $< $(STATS_OBJ) $(BUILD_DIR)/gc_none.o -o $@

# Self-host: use the seed-built compiler to recompile itself
$(BUILD_DIR)/output.ll: $(EASE) $(COMPILER_SRC)
	$(EASE) $(COMPILER_SRC)

# Verify self-hosting convergence (seed-built compiler produces identical IR)
verify: $(EASE) $(RUNTIME_OBJS) | $(BUILD_DIR)
	@echo "=== Verifying self-hosting convergence (GC=$(GC)) ==="
	$(EASE) $(COMPILER_SRC)
	@cp $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen1.ll
	$(CC) $(CFLAGS) $(BUILD_DIR)/verify_gen1.ll $(RUNTIME_OBJS) -o $(BUILD_DIR)/ease_gen1
	$(BUILD_DIR)/ease_gen1 $(COMPILER_SRC)
	@cp $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen2.ll
	@diff $(BUILD_DIR)/verify_gen1.ll $(BUILD_DIR)/verify_gen2.ll \
		&& echo "=== Convergence: PASS (gen1 == gen2) ===" \
		|| (echo "=== Convergence: FAIL ===" && exit 1)

# Update the seed after making changes to the compiler source
update-seed: verify
	cp $(BUILD_DIR)/verify_gen1.ll $(SEED)
	@echo "=== Seed updated ==="

# Run tests (Go-style: fn TestXxx in *_test.ease files)
# Usage: make test [DIR=path/to/dir] (default: tests/)
DIR ?= tests
test: $(EASE)
	@$(EASE) test $(DIR)

# Run tests + benchmarks
# Usage: make bench [DIR=path/to/dir] (default: tests/)
bench: $(EASE)
	@$(EASE) test $(DIR) --bench

clean:
	rm -f $(BUILD_DIR)/ease $(BUILD_DIR)/ease_gen1
	rm -f $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen1.ll $(BUILD_DIR)/verify_gen2.ll
	rm -f $(BUILD_DIR)/test_bin
	rm -f $(BUILD_DIR)/gc_*.o $(BUILD_DIR)/test_gc
