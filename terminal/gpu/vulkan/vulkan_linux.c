#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <vulkan/vulkan.h>
#include "vulkan_linux.h"

#define IN_FLIGHT 3
#define ARENA_START (1 << 20)
#define BIG_UPLOAD (1 << 20)

typedef struct {
	VkBuffer buf;
	VkDeviceMemory mem;
	void *map;
	VkDeviceSize size;
} vkbuf;

struct vktex {
	VkImage img;
	VkDeviceMemory mem;
	VkImageView view;
	int w, h, nearest;
	VkImageLayout layout;
};

struct vkpipe {
	VkPipeline pipe;
	int instanced, hasTex;
};

enum { J_TEX, J_PIPE, J_BUF };
typedef struct {
	int kind;
	void *p;
	vkbuf b;
} junk;
typedef struct {
	junk *v;
	int n, cap;
} junklist;

typedef struct {
	VkCommandPool pool;
	VkCommandBuffer cb;
	VkFence fence;
	VkSemaphore acquire;
	vkbuf arena;
	VkDeviceSize used;
	junklist junk;
} frame;

typedef struct {
	VkFormat format;
	VkPipeline pipe;
} blitpipe;

struct vkctx {
	VkInstance inst;
	VkPhysicalDevice phys;
	VkDevice dev;
	VkQueue q;
	uint32_t qfam;
	VkPhysicalDeviceMemoryProperties memProps;
	VkDeviceSize uboAlign;
	char name[VK_MAX_PHYSICAL_DEVICE_NAME_SIZE];
	uint32_t maxTex;

	PFN_vkCmdBeginRendering beginRendering;
	PFN_vkCmdEndRendering endRendering;
	PFN_vkCmdPushDescriptorSetKHR pushDescriptors;

	VkDescriptorSetLayout dsl;
	VkPipelineLayout layout;
	VkSampler linear, nearest;
	vkbuf quad;
	vktex *dummy;
	VkShaderModule blitV, blitF;
	blitpipe blits[4];
	int nblits;

	VkSurfaceKHR surface;
	VkSwapchainKHR sc;
	VkFormat scFormat;
	VkExtent2D scExtent;
	int scWantW, scWantH, scStale;
	int lost;
	uint32_t nimg;
	VkImage *imgs;
	VkImageView *views;
	VkImageLayout *imgLayout;
	VkSemaphore *done;
	int img;
	int acqPending;
	VkSemaphore acqSem;

	frame frames[IN_FLIGHT];
	int fi, recording;
	junklist loose;
	vkbuf readback;

	vktex *target;
	int tw, th;
	int open, hasClear;
	float clear[4];
	int scissorOn, scissor[4];
	uint32_t pw, ph;
	VkFormat pfmt;
	VkPipeline bound;
};

static char *errf(const char *what, VkResult r) {
	char buf[256];
	snprintf(buf, sizeof buf, "vulkan: %s (VkResult %d)", what, (int)r);
	return strdup(buf);
}

static void warn(const char *what, VkResult r) { fprintf(stderr, "vulkan: %s (VkResult %d)\n", what, (int)r); }

static void lose(vkctx *c, const char *what, VkResult r) {
	if (!c->lost)
		fprintf(stderr, "vulkan: %s failed (VkResult %d), drawing stopped\n", what, (int)r);
	c->lost = 1;
}

static int memtype(vkctx *c, uint32_t bits, VkMemoryPropertyFlags want, VkMemoryPropertyFlags prefer) {
	for (int pass = 0; pass < 2; pass++) {
		VkMemoryPropertyFlags f = pass == 0 ? want | prefer : want;
		for (uint32_t i = 0; i < c->memProps.memoryTypeCount; i++)
			if ((bits & (1u << i)) && (c->memProps.memoryTypes[i].propertyFlags & f) == f)
				return (int)i;
	}
	return -1;
}

static int mkbuf(vkctx *c, VkDeviceSize size, VkBufferUsageFlags usage, VkMemoryPropertyFlags prefer, vkbuf *out) {
	memset(out, 0, sizeof *out);
	VkBufferCreateInfo bi = {
		.sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
		.size = size,
		.usage = usage,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
	};
	VkResult r = vkCreateBuffer(c->dev, &bi, NULL, &out->buf);
	if (r != VK_SUCCESS) {
		warn("create buffer", r);
		return 0;
	}
	VkMemoryRequirements req;
	vkGetBufferMemoryRequirements(c->dev, out->buf, &req);
	int mt = memtype(c, req.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT,
	                 prefer);
	VkMemoryAllocateInfo ai = {
		.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
		.allocationSize = req.size,
		.memoryTypeIndex = (uint32_t)mt,
	};
	if (mt < 0 || (r = vkAllocateMemory(c->dev, &ai, NULL, &out->mem)) != VK_SUCCESS) {
		warn("allocate buffer memory", mt < 0 ? VK_ERROR_OUT_OF_DEVICE_MEMORY : r);
		vkDestroyBuffer(c->dev, out->buf, NULL);
		out->buf = VK_NULL_HANDLE;
		return 0;
	}
	if ((r = vkBindBufferMemory(c->dev, out->buf, out->mem, 0)) != VK_SUCCESS ||
	    (r = vkMapMemory(c->dev, out->mem, 0, VK_WHOLE_SIZE, 0, &out->map)) != VK_SUCCESS) {
		warn("map buffer memory", r);
		vkDestroyBuffer(c->dev, out->buf, NULL);
		vkFreeMemory(c->dev, out->mem, NULL);
		memset(out, 0, sizeof *out);
		return 0;
	}
	out->size = size;
	return 1;
}

static void freebuf(vkctx *c, vkbuf *b) {
	if (!b->buf)
		return;
	vkDestroyBuffer(c->dev, b->buf, NULL);
	vkFreeMemory(c->dev, b->mem, NULL);
	memset(b, 0, sizeof *b);
}

static void junk_add(junklist *l, junk j) {
	if (l->n == l->cap) {
		l->cap = l->cap ? l->cap * 2 : 16;
		l->v = realloc(l->v, (size_t)l->cap * sizeof(junk));
	}
	l->v[l->n++] = j;
}

static void destroy_tex(vkctx *c, vktex *t) {
	vkDestroyImageView(c->dev, t->view, NULL);
	vkDestroyImage(c->dev, t->img, NULL);
	vkFreeMemory(c->dev, t->mem, NULL);
	free(t);
}

static void junk_free(vkctx *c, junklist *l) {
	for (int i = 0; i < l->n; i++) {
		junk *j = &l->v[i];
		switch (j->kind) {
		case J_TEX:
			destroy_tex(c, j->p);
			break;
		case J_PIPE:
			vkDestroyPipeline(c->dev, ((vkpipe *)j->p)->pipe, NULL);
			free(j->p);
			break;
		case J_BUF:
			freebuf(c, &j->b);
			break;
		}
	}
	l->n = 0;
}

static void retire(vkctx *c, junk j) { junk_add(c->recording ? &c->frames[c->fi].junk : &c->loose, j); }


static int ensure_cb(vkctx *c) {
	if (c->lost)
		return 0;
	if (c->recording)
		return 1;
	c->fi = (c->fi + 1) % IN_FLIGHT;
	frame *f = &c->frames[c->fi];
	VkResult r = vkWaitForFences(c->dev, 1, &f->fence, VK_TRUE, UINT64_MAX);
	if (r != VK_SUCCESS) {
		lose(c, "fence wait", r);
		return 0;
	}
	vkResetFences(c->dev, 1, &f->fence);
	junk_free(c, &f->junk);
	// a fence covers everything submitted before it on the queue
	for (int i = 0; i < c->loose.n; i++)
		junk_add(&f->junk, c->loose.v[i]);
	c->loose.n = 0;
	f->used = 0;
	vkResetCommandPool(c->dev, f->pool, 0);
	VkCommandBufferBeginInfo bi = {
		.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
		.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT,
	};
	vkBeginCommandBuffer(f->cb, &bi);
	c->recording = 1;
	c->bound = VK_NULL_HANDLE;
	return 1;
}

static VkCommandBuffer cb(vkctx *c) { return c->frames[c->fi].cb; }

static int arena_put(vkctx *c, const void *p, VkDeviceSize n, VkDeviceSize align, VkBuffer *out, VkDeviceSize *off) {
	frame *f = &c->frames[c->fi];
	VkDeviceSize o = (f->used + align - 1) & ~(align - 1);
	if (o + n > f->arena.size) {
		VkDeviceSize sz = f->arena.size * 2;
		while (sz < n)
			sz *= 2;
		vkbuf nb;
		if (!mkbuf(c, sz,
		           VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT | VK_BUFFER_USAGE_VERTEX_BUFFER_BIT |
		               VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		           0, &nb))
			return 0;
		junk_add(&f->junk, (junk){.kind = J_BUF, .b = f->arena});
		f->arena = nb;
		o = 0;
	}
	memcpy((char *)f->arena.map + o, p, n);
	f->used = o + n;
	*out = f->arena.buf;
	*off = o;
	return 1;
}

// a shader read after a shader read needs no barrier
static void barrier(vkctx *c, VkImage img, VkImageLayout *cur, VkImageLayout to) {
	if (*cur == to && to == VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL)
		return;
	VkImageMemoryBarrier b = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,
		.srcAccessMask = VK_ACCESS_MEMORY_WRITE_BIT,
		.dstAccessMask = VK_ACCESS_MEMORY_READ_BIT | VK_ACCESS_MEMORY_WRITE_BIT,
		.oldLayout = *cur,
		.newLayout = to,
		.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED,
		.image = img,
		.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
	};
	vkCmdPipelineBarrier(cb(c), VK_PIPELINE_STAGE_ALL_COMMANDS_BIT, VK_PIPELINE_STAGE_ALL_COMMANDS_BIT, 0, 0, NULL, 0,
	                     NULL, 1, &b);
	*cur = to;
}

static void end_pass(vkctx *c) {
	if (!c->open)
		return;
	c->endRendering(cb(c));
	c->open = 0;
}


static void sc_release(vkctx *c) {
	for (uint32_t i = 0; i < c->nimg; i++) {
		vkDestroyImageView(c->dev, c->views[i], NULL);
		vkDestroySemaphore(c->dev, c->done[i], NULL);
	}
	free(c->imgs);
	free(c->views);
	free(c->imgLayout);
	free(c->done);
	c->imgs = NULL, c->views = NULL, c->imgLayout = NULL, c->done = NULL;
	c->nimg = 0;
}

static int sc_create(vkctx *c, int w, int h) {
	vkDeviceWaitIdle(c->dev);
	sc_release(c);
	c->scWantW = w, c->scWantH = h, c->scStale = 1;

	VkSurfaceCapabilitiesKHR caps;
	VkResult r = vkGetPhysicalDeviceSurfaceCapabilitiesKHR(c->phys, c->surface, &caps);
	if (r != VK_SUCCESS) {
		warn("surface capabilities", r);
		return 0;
	}
	VkExtent2D ext = caps.currentExtent;
	if (ext.width == UINT32_MAX) {
		ext.width = (uint32_t)w, ext.height = (uint32_t)h;
		if (ext.width < caps.minImageExtent.width)
			ext.width = caps.minImageExtent.width;
		if (ext.height < caps.minImageExtent.height)
			ext.height = caps.minImageExtent.height;
		if (ext.width > caps.maxImageExtent.width)
			ext.width = caps.maxImageExtent.width;
		if (ext.height > caps.maxImageExtent.height)
			ext.height = caps.maxImageExtent.height;
	}
	if (ext.width == 0 || ext.height == 0)
		return 0;

	uint32_t nf = 0;
	vkGetPhysicalDeviceSurfaceFormatsKHR(c->phys, c->surface, &nf, NULL);
	if (nf == 0)
		return 0;
	VkSurfaceFormatKHR *fs = calloc(nf, sizeof *fs);
	vkGetPhysicalDeviceSurfaceFormatsKHR(c->phys, c->surface, &nf, fs);
	VkSurfaceFormatKHR pick = fs[0];
	if (pick.format == VK_FORMAT_UNDEFINED)
		pick.format = VK_FORMAT_B8G8R8A8_UNORM;
	VkFormat want[] = {VK_FORMAT_B8G8R8A8_UNORM, VK_FORMAT_R8G8B8A8_UNORM};
	for (int k = 1; k >= 0; k--)
		for (uint32_t i = 0; i < nf; i++)
			if (fs[i].format == want[k])
				pick = fs[i];
	free(fs);

	VkCompositeAlphaFlagBitsKHR alpha = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
	if (!(caps.supportedCompositeAlpha & alpha)) {
		VkCompositeAlphaFlagBitsKHR all[] = {VK_COMPOSITE_ALPHA_INHERIT_BIT_KHR, VK_COMPOSITE_ALPHA_PRE_MULTIPLIED_BIT_KHR,
		                                     VK_COMPOSITE_ALPHA_POST_MULTIPLIED_BIT_KHR};
		for (int i = 0; i < 3; i++)
			if (caps.supportedCompositeAlpha & all[i]) {
				alpha = all[i];
				break;
			}
	}
	uint32_t count = caps.minImageCount + 1;
	if (caps.maxImageCount > 0 && count > caps.maxImageCount)
		count = caps.maxImageCount;
	VkSwapchainKHR old = c->sc;
	VkSwapchainCreateInfoKHR ci = {
		.sType = VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR,
		.surface = c->surface,
		.minImageCount = count,
		.imageFormat = pick.format,
		.imageColorSpace = pick.colorSpace,
		.imageExtent = ext,
		.imageArrayLayers = 1,
		.imageUsage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT,
		.imageSharingMode = VK_SHARING_MODE_EXCLUSIVE,
		.preTransform = (caps.supportedTransforms & VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR)
		                    ? VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR
		                    : caps.currentTransform,
		.compositeAlpha = alpha,
		.presentMode = VK_PRESENT_MODE_FIFO_KHR,
		.clipped = VK_TRUE,
		.oldSwapchain = old,
	};
	r = vkCreateSwapchainKHR(c->dev, &ci, NULL, &c->sc);
	if (old)
		vkDestroySwapchainKHR(c->dev, old, NULL);
	if (r != VK_SUCCESS) {
		c->sc = VK_NULL_HANDLE;
		warn("create swapchain", r);
		return 0;
	}
	c->scFormat = pick.format;
	c->scExtent = ext;
	vkGetSwapchainImagesKHR(c->dev, c->sc, &c->nimg, NULL);
	c->imgs = calloc(c->nimg, sizeof(VkImage));
	c->views = calloc(c->nimg, sizeof(VkImageView));
	c->imgLayout = calloc(c->nimg, sizeof(VkImageLayout));
	c->done = calloc(c->nimg, sizeof(VkSemaphore));
	vkGetSwapchainImagesKHR(c->dev, c->sc, &c->nimg, c->imgs);
	for (uint32_t i = 0; i < c->nimg; i++) {
		VkImageViewCreateInfo vi = {
			.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
			.image = c->imgs[i],
			.viewType = VK_IMAGE_VIEW_TYPE_2D,
			.format = c->scFormat,
			.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
		};
		vkCreateImageView(c->dev, &vi, NULL, &c->views[i]);
		VkSemaphoreCreateInfo si = {.sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
		// there is one semaphore per image because a present's wait is only
		// known to be finished once its image comes back from acquire
		vkCreateSemaphore(c->dev, &si, NULL, &c->done[i]);
		c->imgLayout[i] = VK_IMAGE_LAYOUT_UNDEFINED;
	}
	c->scStale = 0;
	return 1;
}

static int acquire(vkctx *c) {
	if (c->img >= 0)
		return 1;
	if (!c->surface)
		return 0;
	for (int tries = 0; tries < 2; tries++) {
		if (!c->sc || c->scStale || c->tw != c->scWantW || c->th != c->scWantH)
			if (!sc_create(c, c->tw, c->th))
				return 0;
		VkSemaphore s = c->frames[c->fi].acquire;
		uint32_t i;
		VkResult r = vkAcquireNextImageKHR(c->dev, c->sc, UINT64_MAX, s, VK_NULL_HANDLE, &i);
		if (r == VK_SUCCESS || r == VK_SUBOPTIMAL_KHR) {
			if (r == VK_SUBOPTIMAL_KHR)
				c->scStale = 1;
			c->img = (int)i;
			c->acqSem = s;
			c->acqPending = 1;
			c->imgLayout[i] = VK_IMAGE_LAYOUT_UNDEFINED;
			return 1;
		}
		if (r == VK_ERROR_DEVICE_LOST) {
			lose(c, "acquire", r);
			return 0;
		}
		if (r != VK_ERROR_OUT_OF_DATE_KHR) {
			warn("acquire", r);
			return 0;
		}
		c->scStale = 1;
	}
	return 0;
}


static void apply_scissor(vkctx *c) {
	int tw = (int)c->pw, th = (int)c->ph;
	int x = 0, y = 0, w = tw, h = th;
	if (c->scissorOn)
		x = c->scissor[0], y = c->scissor[1], w = c->scissor[2], h = c->scissor[3];
	int x1 = x + w, y1 = y + h;
	x = x < 0 ? 0 : (x > tw ? tw : x);
	y = y < 0 ? 0 : (y > th ? th : y);
	x1 = x1 < x ? x : (x1 > tw ? tw : x1);
	y1 = y1 < y ? y : (y1 > th ? th : y1);
	VkRect2D r = {{x, y}, {(uint32_t)(x1 - x), (uint32_t)(y1 - y)}};
	vkCmdSetScissor(cb(c), 0, 1, &r);
}

static int ensure_open(vkctx *c) {
	if (c->open)
		return 1;
	if (!ensure_cb(c))
		return 0;
	VkImage img;
	VkImageView view;
	VkImageLayout *lay;
	VkFormat fmt;
	uint32_t w, h;
	if (c->target) {
		vktex *t = c->target;
		img = t->img, view = t->view, lay = &t->layout, fmt = VK_FORMAT_R8G8B8A8_UNORM;
		w = (uint32_t)t->w, h = (uint32_t)t->h;
	} else {
		if (!acquire(c))
			return 0;
		img = c->imgs[c->img], view = c->views[c->img], lay = &c->imgLayout[c->img], fmt = c->scFormat;
		w = c->scExtent.width, h = c->scExtent.height;
	}
	barrier(c, img, lay, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL);
	VkRenderingAttachmentInfo att = {
		.sType = VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
		.imageView = view,
		.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
		.loadOp = c->hasClear ? VK_ATTACHMENT_LOAD_OP_CLEAR : VK_ATTACHMENT_LOAD_OP_LOAD,
		.storeOp = VK_ATTACHMENT_STORE_OP_STORE,
		.clearValue.color.float32 = {c->clear[0], c->clear[1], c->clear[2], c->clear[3]},
	};
	VkRenderingInfo ri = {
		.sType = VK_STRUCTURE_TYPE_RENDERING_INFO,
		.renderArea = {{0, 0}, {w, h}},
		.layerCount = 1,
		.colorAttachmentCount = 1,
		.pColorAttachments = &att,
	};
	c->beginRendering(cb(c), &ri);
	c->hasClear = 0;
	c->open = 1;
	c->pw = w, c->ph = h, c->pfmt = fmt;
	c->bound = VK_NULL_HANDLE;
	VkViewport vp = {0, 0, (float)w, (float)h, 0, 1};
	vkCmdSetViewport(cb(c), 0, 1, &vp);
	apply_scissor(c);
	VkDeviceSize off = 0;
	vkCmdBindVertexBuffers(cb(c), 0, 1, &c->quad.buf, &off);
	return 1;
}

static void flush_pending(vkctx *c) {
	if (c->hasClear && !ensure_open(c))
		c->hasClear = 0;
	end_pass(c);
}

// transitions happen outside a render pass
static int sampled(vkctx *c, vktex *t) {
	if (t->layout == VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL)
		return 1;
	end_pass(c);
	if (!ensure_cb(c))
		return 0;
	barrier(c, t->img, &t->layout, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL);
	return 1;
}

static int submit(vkctx *c, int present) {
	if (!c->recording)
		return 0;
	if (c->lost) {
		c->recording = 0;
		return 0;
	}
	end_pass(c);
	frame *f = &c->frames[c->fi];
	int doPresent = present && c->img >= 0;
	if (doPresent)
		barrier(c, c->imgs[c->img], &c->imgLayout[c->img], VK_IMAGE_LAYOUT_PRESENT_SRC_KHR);
	vkEndCommandBuffer(f->cb);
	VkPipelineStageFlags ws = VK_PIPELINE_STAGE_ALL_COMMANDS_BIT;
	VkSubmitInfo si = {
		.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
		.commandBufferCount = 1,
		.pCommandBuffers = &f->cb,
	};
	if (c->acqPending) {
		si.waitSemaphoreCount = 1;
		si.pWaitSemaphores = &c->acqSem;
		si.pWaitDstStageMask = &ws;
	}
	if (doPresent) {
		si.signalSemaphoreCount = 1;
		si.pSignalSemaphores = &c->done[c->img];
	}
	VkResult r = vkQueueSubmit(c->q, 1, &si, f->fence);
	if (r != VK_SUCCESS) {
		lose(c, "submit", r);
		c->recording = 0;
		return 0;
	}
	c->recording = 0;
	c->acqPending = 0;
	if (!doPresent)
		return 0;
	uint32_t idx = (uint32_t)c->img;
	VkPresentInfoKHR pi = {
		.sType = VK_STRUCTURE_TYPE_PRESENT_INFO_KHR,
		.waitSemaphoreCount = 1,
		.pWaitSemaphores = &c->done[idx],
		.swapchainCount = 1,
		.pSwapchains = &c->sc,
		.pImageIndices = &idx,
	};
	r = vkQueuePresentKHR(c->q, &pi);
	if (r == VK_ERROR_OUT_OF_DATE_KHR || r == VK_SUBOPTIMAL_KHR)
		c->scStale = 1;
	else if (r == VK_ERROR_DEVICE_LOST)
		lose(c, "present", r);
	else if (r != VK_SUCCESS)
		warn("present", r);
	c->img = -1;
	return r == VK_SUCCESS || r == VK_SUBOPTIMAL_KHR;
}


static VkShaderModule module(vkctx *c, const uint32_t *code, int words) {
	VkShaderModuleCreateInfo ci = {
		.sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
		.codeSize = (size_t)words * 4,
		.pCode = code,
	};
	VkShaderModule m = VK_NULL_HANDLE;
	if (vkCreateShaderModule(c->dev, &ci, NULL, &m) != VK_SUCCESS)
		return VK_NULL_HANDLE;
	return m;
}

static VkResult mkpipeline(vkctx *c, VkShaderModule vs, VkShaderModule fs, int instanced, VkFormat format, int blend,
                           VkPipeline *out) {
	VkPipelineShaderStageCreateInfo stages[2] = {
		{.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
		 .stage = VK_SHADER_STAGE_VERTEX_BIT,
		 .module = vs,
		 .pName = "main"},
		{.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
		 .stage = VK_SHADER_STAGE_FRAGMENT_BIT,
		 .module = fs,
		 .pName = "main"},
	};
	VkVertexInputBindingDescription binds[2] = {
		{0, 8, VK_VERTEX_INPUT_RATE_VERTEX},
		{1, 7 * 16, VK_VERTEX_INPUT_RATE_INSTANCE},
	};
	VkVertexInputAttributeDescription attrs[8] = {{0, 0, VK_FORMAT_R32G32_SFLOAT, 0}};
	for (uint32_t i = 0; i < 7; i++)
		attrs[1 + i] = (VkVertexInputAttributeDescription){1 + i, 1, VK_FORMAT_R32G32B32A32_SFLOAT, i * 16};
	VkPipelineVertexInputStateCreateInfo vi = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
		.vertexBindingDescriptionCount = instanced ? 2 : 1,
		.pVertexBindingDescriptions = binds,
		.vertexAttributeDescriptionCount = instanced ? 8 : 1,
		.pVertexAttributeDescriptions = attrs,
	};
	VkPipelineInputAssemblyStateCreateInfo ia = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,
		.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_STRIP,
	};
	VkPipelineViewportStateCreateInfo vp = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
		.viewportCount = 1,
		.scissorCount = 1,
	};
	VkPipelineRasterizationStateCreateInfo rs = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
		.polygonMode = VK_POLYGON_MODE_FILL,
		.cullMode = VK_CULL_MODE_NONE,
		.frontFace = VK_FRONT_FACE_COUNTER_CLOCKWISE,
		.lineWidth = 1,
	};
	VkPipelineMultisampleStateCreateInfo ms = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
		.rasterizationSamples = VK_SAMPLE_COUNT_1_BIT,
	};
	VkPipelineColorBlendAttachmentState ba = {
		.blendEnable = blend ? VK_TRUE : VK_FALSE,
		.srcColorBlendFactor = VK_BLEND_FACTOR_ONE,
		.dstColorBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA,
		.colorBlendOp = VK_BLEND_OP_ADD,
		.srcAlphaBlendFactor = VK_BLEND_FACTOR_ONE,
		.dstAlphaBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA,
		.alphaBlendOp = VK_BLEND_OP_ADD,
		.colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT | VK_COLOR_COMPONENT_B_BIT |
		                  VK_COLOR_COMPONENT_A_BIT,
	};
	VkPipelineColorBlendStateCreateInfo cbs = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
		.attachmentCount = 1,
		.pAttachments = &ba,
	};
	VkDynamicState dyn[] = {VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR};
	VkPipelineDynamicStateCreateInfo ds = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,
		.dynamicStateCount = 2,
		.pDynamicStates = dyn,
	};
	VkPipelineRenderingCreateInfo rci = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO,
		.colorAttachmentCount = 1,
		.pColorAttachmentFormats = &format,
	};
	VkGraphicsPipelineCreateInfo ci = {
		.sType = VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
		.pNext = &rci,
		.stageCount = 2,
		.pStages = stages,
		.pVertexInputState = &vi,
		.pInputAssemblyState = &ia,
		.pViewportState = &vp,
		.pRasterizationState = &rs,
		.pMultisampleState = &ms,
		.pColorBlendState = &cbs,
		.pDynamicState = &ds,
		.layout = c->layout,
	};
	return vkCreateGraphicsPipelines(c->dev, VK_NULL_HANDLE, 1, &ci, NULL, out);
}

vkpipe *vk_pipeline(vkctx *c, const uint32_t *vs, int vwords, const uint32_t *fs, int fwords, int instanced,
                    int hasTex, char **err) {
	if (c->lost) {
		*err = strdup("vulkan: drawing stopped after a device error");
		return NULL;
	}
	VkShaderModule vm = module(c, vs, vwords), fm = module(c, fs, fwords);
	VkPipeline p = VK_NULL_HANDLE;
	VkResult r = VK_ERROR_INITIALIZATION_FAILED;
	if (vm && fm)
		r = mkpipeline(c, vm, fm, instanced, VK_FORMAT_R8G8B8A8_UNORM, 1, &p);
	if (vm)
		vkDestroyShaderModule(c->dev, vm, NULL);
	if (fm)
		vkDestroyShaderModule(c->dev, fm, NULL);
	if (r != VK_SUCCESS) {
		*err = errf("pipeline", r);
		return NULL;
	}
	vkpipe *pp = calloc(1, sizeof *pp);
	pp->pipe = p;
	pp->instanced = instanced;
	pp->hasTex = hasTex;
	return pp;
}

void vk_delete_pipeline(vkctx *c, vkpipe *p) {
	if (!c->lost)
		retire(c, (junk){.kind = J_PIPE, .p = p});
}

static VkPipeline blit_for(vkctx *c, VkFormat f) {
	for (int i = 0; i < c->nblits; i++)
		if (c->blits[i].format == f)
			return c->blits[i].pipe;
	if (c->nblits == (int)(sizeof c->blits / sizeof c->blits[0]))
		return VK_NULL_HANDLE;
	VkPipeline p;
	VkResult r = mkpipeline(c, c->blitV, c->blitF, 0, f, 0, &p);
	if (r != VK_SUCCESS) {
		warn("blit pipeline", r);
		return VK_NULL_HANDLE;
	}
	c->blits[c->nblits++] = (blitpipe){f, p};
	return p;
}


static vktex *texture_alloc(vkctx *c, int w, int h, int nearest) {
	if (c->lost)
		return NULL;
	if (w < 1)
		w = 1;
	if (h < 1)
		h = 1;
	vktex *t = calloc(1, sizeof *t);
	t->w = w, t->h = h, t->nearest = nearest;
	t->layout = VK_IMAGE_LAYOUT_UNDEFINED;
	VkImageCreateInfo ii = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		.imageType = VK_IMAGE_TYPE_2D,
		.format = VK_FORMAT_R8G8B8A8_UNORM,
		.extent = {(uint32_t)w, (uint32_t)h, 1},
		.mipLevels = 1,
		.arrayLayers = 1,
		.samples = VK_SAMPLE_COUNT_1_BIT,
		.tiling = VK_IMAGE_TILING_OPTIMAL,
		.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT |
		         VK_IMAGE_USAGE_TRANSFER_DST_BIT,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE,
		.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
	};
	VkResult r = vkCreateImage(c->dev, &ii, NULL, &t->img);
	if (r != VK_SUCCESS) {
		warn("create image", r);
		free(t);
		return NULL;
	}
	VkMemoryRequirements req;
	vkGetImageMemoryRequirements(c->dev, t->img, &req);
	int mt = memtype(c, req.memoryTypeBits, 0, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
	VkMemoryAllocateInfo ai = {
		.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
		.allocationSize = req.size,
		.memoryTypeIndex = (uint32_t)mt,
	};
	if (mt < 0 || (r = vkAllocateMemory(c->dev, &ai, NULL, &t->mem)) != VK_SUCCESS) {
		warn("allocate image memory", r);
		vkDestroyImage(c->dev, t->img, NULL);
		free(t);
		return NULL;
	}
	if ((r = vkBindImageMemory(c->dev, t->img, t->mem, 0)) != VK_SUCCESS) {
		warn("bind image memory", r);
		vkDestroyImage(c->dev, t->img, NULL);
		vkFreeMemory(c->dev, t->mem, NULL);
		free(t);
		return NULL;
	}
	VkImageViewCreateInfo vi = {
		.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
		.image = t->img,
		.viewType = VK_IMAGE_VIEW_TYPE_2D,
		.format = VK_FORMAT_R8G8B8A8_UNORM,
		.subresourceRange = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1},
	};
	if ((r = vkCreateImageView(c->dev, &vi, NULL, &t->view)) != VK_SUCCESS) {
		warn("create image view", r);
		vkDestroyImage(c->dev, t->img, NULL);
		vkFreeMemory(c->dev, t->mem, NULL);
		free(t);
		return NULL;
	}
	return t;
}

vktex *vk_texture(vkctx *c, int w, int h, int nearest) { return texture_alloc(c, w, h, nearest); }

void vk_delete_texture(vkctx *c, vktex *t) {
	if (!c->lost)
		retire(c, (junk){.kind = J_TEX, .p = t});
}

int vk_write(vkctx *c, vktex *t, const void *pix, int y0, int h) {
	if (!ensure_cb(c))
		return 0;
	end_pass(c);
	VkDeviceSize n = (VkDeviceSize)t->w * h * 4;
	VkBuffer buf;
	VkDeviceSize off = 0;
	if (n > BIG_UPLOAD) {
		vkbuf b;
		if (!mkbuf(c, n, VK_BUFFER_USAGE_TRANSFER_SRC_BIT, 0, &b))
			return 0;
		memcpy(b.map, pix, n);
		junk_add(&c->frames[c->fi].junk, (junk){.kind = J_BUF, .b = b});
		buf = b.buf;
	} else if (!arena_put(c, pix, n, 16, &buf, &off)) {
		return 0;
	}
	barrier(c, t->img, &t->layout, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL);
	VkBufferImageCopy r = {
		.bufferOffset = off,
		.imageSubresource = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 0, 1},
		.imageOffset = {0, y0, 0},
		.imageExtent = {(uint32_t)t->w, (uint32_t)h, 1},
	};
	vkCmdCopyBufferToImage(cb(c), buf, t->img, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &r);
	return 1;
}


void vk_set_target(vkctx *c, vktex *t, int w, int h) {
	if (c->lost)
		return;
	flush_pending(c);
	c->target = t;
	c->tw = w, c->th = h;
}

void vk_clear(vkctx *c, float r, float g, float b, float a) {
	if (c->lost)
		return;
	end_pass(c);
	c->hasClear = 1;
	c->clear[0] = r, c->clear[1] = g, c->clear[2] = b, c->clear[3] = a;
}

void vk_scissor(vkctx *c, int on, int x, int y, int w, int h) {
	if (c->lost)
		return;
	c->scissorOn = on;
	c->scissor[0] = x, c->scissor[1] = y, c->scissor[2] = w, c->scissor[3] = h;
	if (c->open)
		apply_scissor(c);
}

static void bind(vkctx *c, VkPipeline p) {
	if (c->bound == p)
		return;
	vkCmdBindPipeline(cb(c), VK_PIPELINE_BIND_POINT_GRAPHICS, p);
	c->bound = p;
}

static VkWriteDescriptorSet texWrite(vktex *t, VkDescriptorImageInfo *ii, VkSampler s) {
	*ii = (VkDescriptorImageInfo){s, t->view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
	return (VkWriteDescriptorSet){
		.sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
		.dstBinding = 2,
		.descriptorCount = 1,
		.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
		.pImageInfo = ii,
	};
}

void vk_draw(vkctx *c, vkpipe *p, vktex *t, const void *vub, int vlen, const void *fub, int flen, const void *inst,
             int instBytes, int count) {
	if (c->lost)
		return;
	if (p->instanced && (count <= 0 || instBytes <= 0))
		return;
	if (!t && p->hasTex)
		t = c->dummy;
	if (t && !sampled(c, t))
		return;
	if (!ensure_open(c))
		return;
	bind(c, p->pipe);
	VkWriteDescriptorSet w[3];
	VkDescriptorBufferInfo bi[2];
	VkDescriptorImageInfo ii;
	uint32_t n = 0;
	const void *ub[2] = {vub, fub};
	int ul[2] = {vlen, flen};
	for (int k = 0; k < 2; k++) {
		if (ul[k] <= 0)
			continue;
		VkBuffer b;
		VkDeviceSize off;
		if (!arena_put(c, ub[k], (VkDeviceSize)ul[k], c->uboAlign, &b, &off)) {
			lose(c, "arena allocation", VK_ERROR_OUT_OF_DEVICE_MEMORY);
			return;
		}
		bi[k] = (VkDescriptorBufferInfo){b, off, (VkDeviceSize)ul[k]};
		w[n++] = (VkWriteDescriptorSet){
			.sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
			.dstBinding = (uint32_t)k,
			.descriptorCount = 1,
			.descriptorType = VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER,
			.pBufferInfo = &bi[k],
		};
	}
	if (t)
		w[n++] = texWrite(t, &ii, t->nearest ? c->nearest : c->linear);
	if (n > 0)
		c->pushDescriptors(cb(c), VK_PIPELINE_BIND_POINT_GRAPHICS, c->layout, 0, n, w);
	if (p->instanced) {
		VkBuffer b;
		VkDeviceSize off;
		if (!arena_put(c, inst, (VkDeviceSize)instBytes, 16, &b, &off)) {
			lose(c, "arena allocation", VK_ERROR_OUT_OF_DEVICE_MEMORY);
			return;
		}
		vkCmdBindVertexBuffers(cb(c), 1, 1, &b, &off);
		vkCmdDraw(cb(c), 4, (uint32_t)count, 0, 0);
	} else {
		vkCmdDraw(cb(c), 4, 1, 0, 0);
	}
}

void vk_copy(vkctx *c, vktex *src) {
	if (c->lost)
		return;
	if (!sampled(c, src) || !ensure_open(c))
		return;
	VkPipeline p = blit_for(c, c->pfmt);
	if (!p)
		return;
	bind(c, p);
	int on = c->scissorOn;
	c->scissorOn = 0;
	apply_scissor(c);
	c->scissorOn = on;
	VkDescriptorImageInfo ii;
	VkWriteDescriptorSet w = texWrite(src, &ii, c->nearest);
	c->pushDescriptors(cb(c), VK_PIPELINE_BIND_POINT_GRAPHICS, c->layout, 0, 1, &w);
	vkCmdDraw(cb(c), 4, 1, 0, 0);
	apply_scissor(c);
}

int vk_read(vkctx *c, vktex *t, void *out) {
	if (c->lost)
		return 0;
	flush_pending(c);
	if (!ensure_cb(c))
		return 0;
	VkDeviceSize n = (VkDeviceSize)t->w * t->h * 4;
	if (c->readback.size < n) {
		freebuf(c, &c->readback);
		if (!mkbuf(c, n, VK_BUFFER_USAGE_TRANSFER_DST_BIT, VK_MEMORY_PROPERTY_HOST_CACHED_BIT, &c->readback))
			return 0;
	}
	barrier(c, t->img, &t->layout, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL);
	VkBufferImageCopy r = {
		.imageSubresource = {VK_IMAGE_ASPECT_COLOR_BIT, 0, 0, 1},
		.imageExtent = {(uint32_t)t->w, (uint32_t)t->h, 1},
	};
	vkCmdCopyImageToBuffer(cb(c), t->img, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, c->readback.buf, 1, &r);
	VkMemoryBarrier mb = {
		.sType = VK_STRUCTURE_TYPE_MEMORY_BARRIER,
		.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT,
		.dstAccessMask = VK_ACCESS_HOST_READ_BIT,
	};
	vkCmdPipelineBarrier(cb(c), VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_HOST_BIT, 0, 1, &mb, 0, NULL, 0, NULL);
	frame *f = &c->frames[c->fi];
	submit(c, 0);
	if (c->lost)
		return 0;
	VkResult wr = vkWaitForFences(c->dev, 1, &f->fence, VK_TRUE, UINT64_MAX);
	if (wr != VK_SUCCESS) {
		lose(c, "fence wait", wr);
		return 0;
	}
	memcpy(out, c->readback.map, n);
	return 1;
}

int vk_present(vkctx *c) {
	if (c->lost)
		return 0;
	flush_pending(c);
	return submit(c, 1);
}


typedef struct {
	VkStructureType sType;
	const void *pNext;
	VkFlags flags;
	void *dpy;
	unsigned long window;
} xlibSurfaceInfo;

typedef struct {
	VkStructureType sType;
	const void *pNext;
	VkFlags flags;
	void *display;
	void *surface;
} waylandSurfaceInfo;

typedef VkResult (*createSurfaceFn)(VkInstance, const void *, const VkAllocationCallbacks *, VkSurfaceKHR *);

static int hasExt(VkExtensionProperties *e, uint32_t n, const char *name) {
	for (uint32_t i = 0; i < n; i++)
		if (!strcmp(e[i].extensionName, name))
			return 1;
	return 0;
}

const char *vk_device_name(vkctx *c) { return c->name; }

int vk_is_lost(vkctx *c) { return c->lost; }

int vk_max_texture(vkctx *c) { return (int)c->maxTex; }

vkctx *vk_new(int platform, void *a, uint64_t b, const uint32_t *blitV, int blitVWords, const uint32_t *blitF,
              int blitFWords, char **err) {
	vkctx *c = calloc(1, sizeof *c);
	c->img = -1;
	c->fi = IN_FLIGHT - 1;
	VkResult r;

	uint32_t instApi = VK_API_VERSION_1_3;
#ifdef VK_API_VERSION_1_4
	uint32_t loaderApi = VK_API_VERSION_1_0;
	vkEnumerateInstanceVersion(&loaderApi);
	if (loaderApi >= VK_API_VERSION_1_4)
		instApi = VK_API_VERSION_1_4;
#endif
	const char *iexts[2];
	uint32_t niext = 0;
	if (platform != VKW_NONE) {
		iexts[niext++] = VK_KHR_SURFACE_EXTENSION_NAME;
		iexts[niext++] = platform == VKW_XLIB ? "VK_KHR_xlib_surface" : "VK_KHR_wayland_surface";
		uint32_t n = 0;
		vkEnumerateInstanceExtensionProperties(NULL, &n, NULL);
		VkExtensionProperties *e = calloc(n ? n : 1, sizeof *e);
		vkEnumerateInstanceExtensionProperties(NULL, &n, e);
		int ok = hasExt(e, n, iexts[0]) && hasExt(e, n, iexts[1]);
		free(e);
		if (!ok) {
			*err = errf(platform == VKW_XLIB ? "loader lacks VK_KHR_xlib_surface" : "loader lacks VK_KHR_wayland_surface",
			            VK_ERROR_EXTENSION_NOT_PRESENT);
			return NULL;
		}
	}
	const char *layers[1] = {"VK_LAYER_KHRONOS_validation"};
	const char *val = getenv("CAUTION_VK_VALIDATION");
	int validate = val && *val && strcmp(val, "0");
	VkApplicationInfo app = {
		.sType = VK_STRUCTURE_TYPE_APPLICATION_INFO,
		.pApplicationName = "caution",
		.pEngineName = "caution",
		.apiVersion = instApi,
	};
	VkInstanceCreateInfo ici = {
		.sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
		.pApplicationInfo = &app,
		.enabledLayerCount = validate ? 1 : 0,
		.ppEnabledLayerNames = layers,
		.enabledExtensionCount = niext,
		.ppEnabledExtensionNames = iexts,
	};
	if ((r = vkCreateInstance(&ici, NULL, &c->inst)) != VK_SUCCESS) {
		*err = errf("create instance (is a Vulkan driver installed?)", r);
		return NULL;
	}

	if (platform == VKW_XLIB) {
		createSurfaceFn fn = (createSurfaceFn)vkGetInstanceProcAddr(c->inst, "vkCreateXlibSurfaceKHR");
		xlibSurfaceInfo si = {VK_STRUCTURE_TYPE_XLIB_SURFACE_CREATE_INFO_KHR, NULL, 0, a, (unsigned long)b};
		r = fn ? fn(c->inst, &si, NULL, &c->surface) : VK_ERROR_EXTENSION_NOT_PRESENT;
	} else if (platform == VKW_WAYLAND) {
		createSurfaceFn fn = (createSurfaceFn)vkGetInstanceProcAddr(c->inst, "vkCreateWaylandSurfaceKHR");
		waylandSurfaceInfo si = {VK_STRUCTURE_TYPE_WAYLAND_SURFACE_CREATE_INFO_KHR, NULL, 0, a, (void *)(uintptr_t)b};
		r = fn ? fn(c->inst, &si, NULL, &c->surface) : VK_ERROR_EXTENSION_NOT_PRESENT;
	}
	if (platform != VKW_NONE && r != VK_SUCCESS) {
		*err = errf("create window surface", r);
		return NULL;
	}

	// push descriptors come from VK_KHR_push_descriptor or 1.4 core
	uint32_t np = 0;
	vkEnumeratePhysicalDevices(c->inst, &np, NULL);
	if (np == 0) {
		*err = errf("no physical devices", VK_ERROR_INITIALIZATION_FAILED);
		return NULL;
	}
	VkPhysicalDevice *pds = calloc(np, sizeof *pds);
	vkEnumeratePhysicalDevices(c->inst, &np, pds);
	int best = -1, bestScore = -1, bestKHRPush = 0;
	uint32_t bestFam = 0;
	for (uint32_t i = 0; i < np; i++) {
		VkPhysicalDeviceProperties pr;
		vkGetPhysicalDeviceProperties(pds[i], &pr);
		uint32_t api = pr.apiVersion < instApi ? pr.apiVersion : instApi;
		if (api < VK_API_VERSION_1_3)
			continue;
		uint32_t ne = 0;
		vkEnumerateDeviceExtensionProperties(pds[i], NULL, &ne, NULL);
		VkExtensionProperties *e = calloc(ne ? ne : 1, sizeof *e);
		vkEnumerateDeviceExtensionProperties(pds[i], NULL, &ne, e);
		int khrPush = hasExt(e, ne, VK_KHR_PUSH_DESCRIPTOR_EXTENSION_NAME);
		int swap = hasExt(e, ne, VK_KHR_SWAPCHAIN_EXTENSION_NAME);
		free(e);
		int corePush = 0;
#ifdef VK_API_VERSION_1_4
		corePush = api >= VK_API_VERSION_1_4;
#endif
		if (!khrPush && !corePush)
			continue;
		if (c->surface && !swap)
			continue;
		uint32_t nq = 0;
		vkGetPhysicalDeviceQueueFamilyProperties(pds[i], &nq, NULL);
		VkQueueFamilyProperties *qs = calloc(nq ? nq : 1, sizeof *qs);
		vkGetPhysicalDeviceQueueFamilyProperties(pds[i], &nq, qs);
		int fam = -1;
		for (uint32_t k = 0; k < nq && fam < 0; k++) {
			if (!(qs[k].queueFlags & VK_QUEUE_GRAPHICS_BIT))
				continue;
			VkBool32 present = VK_TRUE;
			if (c->surface)
				vkGetPhysicalDeviceSurfaceSupportKHR(pds[i], k, c->surface, &present);
			if (present)
				fam = (int)k;
		}
		free(qs);
		if (fam < 0)
			continue;
		int score = 0;
		switch (pr.deviceType) {
		case VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU: score = 4; break;
		case VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU: score = 3; break;
		case VK_PHYSICAL_DEVICE_TYPE_VIRTUAL_GPU: score = 2; break;
		case VK_PHYSICAL_DEVICE_TYPE_CPU: score = 1; break;
		default: break;
		}
		if (score > bestScore) {
			best = (int)i, bestScore = score, bestFam = (uint32_t)fam, bestKHRPush = khrPush;
		}
	}
	if (best < 0) {
		free(pds);
		*err = errf("no device with Vulkan 1.3, push descriptors and a graphics queue that can present",
		            VK_ERROR_INCOMPATIBLE_DRIVER);
		return NULL;
	}
	c->phys = pds[best];
	c->qfam = bestFam;
	free(pds);
	VkPhysicalDeviceProperties pr;
	vkGetPhysicalDeviceProperties(c->phys, &pr);
	snprintf(c->name, sizeof c->name, "%s", pr.deviceName);
	c->maxTex = pr.limits.maxImageDimension2D;
	c->uboAlign = pr.limits.minUniformBufferOffsetAlignment;
	if (c->uboAlign < 16)
		c->uboAlign = 16;
	vkGetPhysicalDeviceMemoryProperties(c->phys, &c->memProps);

	float prio = 1;
	VkDeviceQueueCreateInfo qci = {
		.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
		.queueFamilyIndex = c->qfam,
		.queueCount = 1,
		.pQueuePriorities = &prio,
	};
	const char *dexts[2];
	uint32_t ndext = 0;
	if (c->surface)
		dexts[ndext++] = VK_KHR_SWAPCHAIN_EXTENSION_NAME;
	if (bestKHRPush)
		dexts[ndext++] = VK_KHR_PUSH_DESCRIPTOR_EXTENSION_NAME;
	VkPhysicalDeviceVulkan13Features f13 = {
		.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_3_FEATURES,
		.dynamicRendering = VK_TRUE,
	};
#ifdef VK_API_VERSION_1_4
	VkPhysicalDeviceVulkan14Features f14 = {
		.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_4_FEATURES,
		.pushDescriptor = VK_TRUE,
	};
	if (!bestKHRPush)
		f13.pNext = &f14;
#endif
	VkPhysicalDeviceFeatures2 f2 = {.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2, .pNext = &f13};
	VkDeviceCreateInfo dci = {
		.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
		.pNext = &f2,
		.queueCreateInfoCount = 1,
		.pQueueCreateInfos = &qci,
		.enabledExtensionCount = ndext,
		.ppEnabledExtensionNames = dexts,
	};
	if ((r = vkCreateDevice(c->phys, &dci, NULL, &c->dev)) != VK_SUCCESS) {
		*err = errf("create device", r);
		return NULL;
	}
	vkGetDeviceQueue(c->dev, c->qfam, 0, &c->q);
	c->beginRendering = (PFN_vkCmdBeginRendering)vkGetDeviceProcAddr(c->dev, "vkCmdBeginRendering");
	c->endRendering = (PFN_vkCmdEndRendering)vkGetDeviceProcAddr(c->dev, "vkCmdEndRendering");
	c->pushDescriptors = (PFN_vkCmdPushDescriptorSetKHR)vkGetDeviceProcAddr(
		c->dev, bestKHRPush ? "vkCmdPushDescriptorSetKHR" : "vkCmdPushDescriptorSet");
	if (!c->beginRendering || !c->endRendering || !c->pushDescriptors) {
		*err = errf("missing dynamic rendering or push descriptor entry points", VK_ERROR_INITIALIZATION_FAILED);
		return NULL;
	}

	VkDescriptorSetLayoutBinding binds[3] = {
		{0, VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, 1, VK_SHADER_STAGE_VERTEX_BIT | VK_SHADER_STAGE_FRAGMENT_BIT, NULL},
		{1, VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, 1, VK_SHADER_STAGE_VERTEX_BIT | VK_SHADER_STAGE_FRAGMENT_BIT, NULL},
		{2, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 1, VK_SHADER_STAGE_VERTEX_BIT | VK_SHADER_STAGE_FRAGMENT_BIT,
		 NULL},
	};
	VkDescriptorSetLayoutCreateInfo dl = {
		.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,
		.flags = VK_DESCRIPTOR_SET_LAYOUT_CREATE_PUSH_DESCRIPTOR_BIT_KHR,
		.bindingCount = 3,
		.pBindings = binds,
	};
	vkCreateDescriptorSetLayout(c->dev, &dl, NULL, &c->dsl);
	VkPipelineLayoutCreateInfo pl = {
		.sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
		.setLayoutCount = 1,
		.pSetLayouts = &c->dsl,
	};
	vkCreatePipelineLayout(c->dev, &pl, NULL, &c->layout);

	VkSamplerCreateInfo sci = {
		.sType = VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
		.magFilter = VK_FILTER_LINEAR,
		.minFilter = VK_FILTER_LINEAR,
		.mipmapMode = VK_SAMPLER_MIPMAP_MODE_NEAREST,
		.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.maxLod = 0,
	};
	vkCreateSampler(c->dev, &sci, NULL, &c->linear);
	sci.magFilter = sci.minFilter = VK_FILTER_NEAREST;
	vkCreateSampler(c->dev, &sci, NULL, &c->nearest);

	for (int i = 0; i < IN_FLIGHT; i++) {
		frame *f = &c->frames[i];
		VkCommandPoolCreateInfo cpi = {
			.sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
			.flags = VK_COMMAND_POOL_CREATE_TRANSIENT_BIT,
			.queueFamilyIndex = c->qfam,
		};
		vkCreateCommandPool(c->dev, &cpi, NULL, &f->pool);
		VkCommandBufferAllocateInfo cai = {
			.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
			.commandPool = f->pool,
			.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY,
			.commandBufferCount = 1,
		};
		vkAllocateCommandBuffers(c->dev, &cai, &f->cb);
		VkFenceCreateInfo fci = {.sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO, .flags = VK_FENCE_CREATE_SIGNALED_BIT};
		vkCreateFence(c->dev, &fci, NULL, &f->fence);
		VkSemaphoreCreateInfo si = {.sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
		vkCreateSemaphore(c->dev, &si, NULL, &f->acquire);
		if (!mkbuf(c, ARENA_START,
		           VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT | VK_BUFFER_USAGE_VERTEX_BUFFER_BIT |
		               VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		           0, &f->arena)) {
			*err = errf("frame arena", VK_ERROR_OUT_OF_DEVICE_MEMORY);
			return NULL;
		}
	}
	float unit[] = {0, 0, 1, 0, 0, 1, 1, 1};
	if (!mkbuf(c, sizeof unit, VK_BUFFER_USAGE_VERTEX_BUFFER_BIT, 0, &c->quad)) {
		*err = errf("quad buffer", VK_ERROR_OUT_OF_DEVICE_MEMORY);
		return NULL;
	}
	memcpy(c->quad.map, unit, sizeof unit);

	c->blitV = module(c, blitV, blitVWords);
	c->blitF = module(c, blitF, blitFWords);
	if (!c->blitV || !c->blitF) {
		*err = errf("blit shaders", VK_ERROR_INITIALIZATION_FAILED);
		return NULL;
	}
	c->dummy = vk_texture(c, 1, 1, 1);
	if (!c->dummy) {
		*err = errf("dummy texture", VK_ERROR_OUT_OF_DEVICE_MEMORY);
		return NULL;
	}
	uint8_t zero[4] = {0};
	if (!vk_write(c, c->dummy, zero, 0, c->dummy->h)) {
		*err = errf("dummy texture upload", VK_ERROR_OUT_OF_DEVICE_MEMORY);
		return NULL;
	}
	return c;
}
