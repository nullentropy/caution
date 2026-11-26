#ifndef SX_H
#define SX_H
#ifdef __cplusplus
extern "C" {
#endif

enum { SX_MSL = 1, SX_HLSL = 2 };

typedef struct sx_out {
	unsigned int *spirv;
	int spirv_words;
	char *msl, *msl_entry, *hlsl;
	char *err;
	int ubo_size, n_members;
	char **names;
	int *offsets, *sizes;
	int has_tex;
} sx_out;

int sx_compile(int frag, const char *src, unsigned ubo_binding, int want, sx_out *o);
void sx_free(sx_out *o);

#ifdef __cplusplus
}
#endif
#endif
