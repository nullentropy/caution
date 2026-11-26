#include <stdint.h>

typedef struct vkctx vkctx;
typedef struct vktex vktex;
typedef struct vkpipe vkpipe;

enum { VKW_NONE = 0, VKW_XLIB = 1, VKW_WAYLAND = 2 };

vkctx *vk_new(int platform, void *a, uint64_t b, const uint32_t *blitV, int blitVWords, const uint32_t *blitF,
              int blitFWords, char **err);
const char *vk_device_name(vkctx *c);

vktex *vk_texture(vkctx *c, int w, int h, int nearest);
int vk_write(vkctx *c, vktex *t, const void *pix, int y0, int h);
void vk_delete_texture(vkctx *c, vktex *t);

vkpipe *vk_pipeline(vkctx *c, const uint32_t *vs, int vwords, const uint32_t *fs, int fwords, int instanced,
                    int hasTex, char **err);
void vk_delete_pipeline(vkctx *c, vkpipe *p);

void vk_set_target(vkctx *c, vktex *t, int w, int h);
void vk_clear(vkctx *c, float r, float g, float b, float a);
void vk_scissor(vkctx *c, int on, int x, int y, int w, int h);
void vk_draw(vkctx *c, vkpipe *p, vktex *t, const void *vub, int vlen, const void *fub, int flen, const void *inst,
             int instBytes, int count);
void vk_copy(vkctx *c, vktex *src);
int vk_read(vkctx *c, vktex *t, void *out);
int vk_present(vkctx *c);
int vk_is_lost(vkctx *c);
int vk_max_texture(vkctx *c);
