void *mtl_new(void *nswindow, char **err);
void mtl_release(void *obj);
void mtl_release_later(void *ctx, void *obj);
void *mtl_texture(void *ctx, int w, int h);
int mtl_write(void *ctx, void *tex, const void *pix, int w, int y0, int h);
void *mtl_pipeline(void *ctx, const char *vsrc, const char *ventry, const char *fsrc, const char *fentry,
                   int instanced, char **err);
int mtl_begin(void *ctx, void *tex, int w, int h, int clear, float r, float g, float b, float a);
void mtl_end(void *ctx);
void mtl_scissor(void *ctx, int on, int x, int y, int w, int h);
void mtl_draw(void *ctx, void *pipe, void *tex, int nearest, const void *vub, int vlen, const void *fub, int flen,
              const void *inst, int instBytes, int count, int instanced);
void mtl_copy(void *ctx, void *src);
int mtl_read(void *ctx, void *tex, void *out, int w, int h);
int mtl_present(void *ctx);
int mtl_is_lost(void *ctx);
