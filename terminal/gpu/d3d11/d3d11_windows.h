#pragma once

typedef struct dx_ctx dx_ctx;
typedef struct dx_tex dx_tex;
typedef struct dx_pipe dx_pipe;

dx_ctx *dx_new(void *hwnd, char **err);
dx_tex *dx_texture(dx_ctx *c, int w, int h);
void dx_write(dx_ctx *c, dx_tex *t, const void *pix, int y0, int h);
void dx_tex_free(dx_ctx *c, dx_tex *t);
dx_pipe *dx_pipeline(dx_ctx *c, const char *vs, const char *fs, int vubo, int fubo, int instanced, char **err);
void dx_pipe_free(dx_pipe *p);
int dx_target(dx_ctx *c, dx_tex *t, int w, int h);
void dx_clear(dx_ctx *c, float r, float g, float b, float a);
void dx_scissor(dx_ctx *c, int on, int x, int y, int w, int h);
void dx_draw(dx_ctx *c, dx_pipe *p, dx_tex *t, int nearest, const void *vub, int vlen, const void *fub, int flen,
             const void *inst, int instBytes, int count);
void dx_copy(dx_ctx *c, dx_tex *src);
int dx_read(dx_ctx *c, dx_tex *t, void *out);
int dx_present(dx_ctx *c);
int dx_is_lost(dx_ctx *c);
