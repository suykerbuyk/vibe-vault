/* C fixture for verify symbol-grammar tests. */

#include <stdio.h>

struct Frame {
    int id;
    unsigned char data[8];
};

typedef struct Frame frame_t;

#define MAX_FRAMES 128

static int helper_count(int n) {
    return n + 1;
}

int run_pipeline(int argc, char **argv) {
    /* call site that should NOT be matched as a declaration */
    return helper_count(argc);
}

void emit_frame(struct Frame *f) {
    if (f) printf("%d\n", f->id);
}
