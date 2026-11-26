#import <Metal/Metal.h>
#import <QuartzCore/CAMetalLayer.h>
#import <Cocoa/Cocoa.h>
#include <stdatomic.h>
#include <stdio.h>
#include <string.h>
#include "metal_darwin.h"

#define IN_FLIGHT 3

static NSString *const blitSrc =
	@"#include <metal_stdlib>\n"
	 "using namespace metal;\n"
	 "struct V { float4 pos [[position]]; float2 uv; };\n"
	 "vertex V blit_v(uint vid [[vertex_id]]) {\n"
	 "  float2 p = float2(vid & 1, vid >> 1);\n"
	 "  V o; o.pos = float4(p.x * 2.0 - 1.0, 1.0 - p.y * 2.0, 0.0, 1.0); o.uv = p; return o;\n"
	 "}\n"
	 "fragment float4 blit_f(V in [[stage_in]], texture2d<float> t [[texture(0)]]) {\n"
	 "  constexpr sampler s(filter::nearest, address::clamp_to_edge);\n"
	 "  return t.sample(s, in.uv);\n"
	 "}\n";

@interface Ctx : NSObject {
@public
	id<MTLDevice> dev;
	id<MTLCommandQueue> queue;
	CAMetalLayer *layer;
	id<MTLCommandBuffer> cb;
	id<MTLRenderCommandEncoder> enc;
	id<CAMetalDrawable> drawable;
	id<MTLTexture> encTarget;
	id<MTLBuffer> quad;
	id<MTLBuffer> arena[IN_FLIGHT];
	NSUInteger arenaUsed;
	// buffers are released on this thread when the frame's slot is reused,
	// because if the command buffer released them last they would be freed on
	// Metal's completion thread, which has no autorelease pool
	NSMutableArray *keep[IN_FLIGHT];
	int frame;
	dispatch_semaphore_t inflight;
	atomic_int *lost;
	id<MTLSamplerState> linear, nearest;
	id<MTLRenderPipelineState> blitRGBA, blitBGRA;
}
@end
@implementation Ctx
@end

static char *errdup(NSError *e, const char *what) {
	NSString *s = [NSString stringWithFormat:@"%s: %@", what, e ? e.localizedDescription : @"unknown error"];
	return strdup(s.UTF8String);
}

static id<MTLRenderPipelineState> blitPipeline(Ctx *c, id<MTLLibrary> lib, MTLPixelFormat f, NSError **e) {
	MTLRenderPipelineDescriptor *d = [MTLRenderPipelineDescriptor new];
	d.vertexFunction = [lib newFunctionWithName:@"blit_v"];
	d.fragmentFunction = [lib newFunctionWithName:@"blit_f"];
	d.colorAttachments[0].pixelFormat = f;
	return [c->dev newRenderPipelineStateWithDescriptor:d error:e];
}

void *mtl_new(void *nswindow, char **err) {
	@autoreleasepool {
		Ctx *c = [Ctx new];
		c->dev = MTLCreateSystemDefaultDevice();
		if (!c->dev) {
			*err = strdup("metal: no device");
			return NULL;
		}
		c->queue = [c->dev newCommandQueue];
		c->inflight = dispatch_semaphore_create(IN_FLIGHT);
		c->lost = calloc(1, sizeof *c->lost);
		float unit[] = {0, 0, 1, 0, 0, 1, 1, 1};
		c->quad = [c->dev newBufferWithBytes:unit length:sizeof unit options:MTLResourceStorageModeShared];
		for (int i = 0; i < IN_FLIGHT; i++) {
			c->arena[i] = [c->dev newBufferWithLength:1 << 20 options:MTLResourceStorageModeShared];
			c->keep[i] = [NSMutableArray new];
			if (!c->arena[i]) {
				*err = strdup("metal: buffer allocation failed");
				return NULL;
			}
		}
		MTLSamplerDescriptor *sd = [MTLSamplerDescriptor new];
		sd.sAddressMode = sd.tAddressMode = MTLSamplerAddressModeClampToEdge;
		sd.minFilter = sd.magFilter = MTLSamplerMinMagFilterLinear;
		c->linear = [c->dev newSamplerStateWithDescriptor:sd];
		sd.minFilter = sd.magFilter = MTLSamplerMinMagFilterNearest;
		c->nearest = [c->dev newSamplerStateWithDescriptor:sd];

		NSError *e = nil;
		id<MTLLibrary> lib = [c->dev newLibraryWithSource:blitSrc options:nil error:&e];
		if (!lib) {
			*err = errdup(e, "metal: blit library");
			return NULL;
		}
		c->blitRGBA = blitPipeline(c, lib, MTLPixelFormatRGBA8Unorm, &e);
		c->blitBGRA = blitPipeline(c, lib, MTLPixelFormatBGRA8Unorm, &e);
		if (!c->blitRGBA || !c->blitBGRA) {
			*err = errdup(e, "metal: blit pipeline");
			return NULL;
		}

		if (nswindow) {
			NSWindow *win = (__bridge NSWindow *)nswindow;
			CAMetalLayer *l = [CAMetalLayer layer];
			l.device = c->dev;
			l.pixelFormat = MTLPixelFormatBGRA8Unorm;
			l.framebufferOnly = YES;
			l.maximumDrawableCount = 3;
			l.displaySyncEnabled = YES;
			l.contentsScale = win.backingScaleFactor;
			NSView *v = win.contentView;
			v.wantsLayer = YES;
			v.layer = l;
			c->layer = l;
		}
		return (__bridge_retained void *)c;
	}
}

void mtl_release(void *obj) {
	@autoreleasepool {
		if (obj)
			CFRelease(obj);
	}
}

void mtl_release_later(void *ctx, void *obj) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		if (obj)
			[c->keep[c->frame] addObject:(__bridge_transfer id)obj];
	}
}

// the SDK deprecates this because Apple silicon can't lose its GPU, an Intel
// Mac still loses an unplugged eGPU
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
static const NSInteger deviceRemoved = MTLCommandBufferErrorDeviceRemoved;
#pragma clang diagnostic pop

static void ensureCB(Ctx *c) {
	if (c->cb)
		return;
	dispatch_semaphore_wait(c->inflight, DISPATCH_TIME_FOREVER);
	c->frame = (c->frame + 1) % IN_FLIGHT;
	[c->keep[c->frame] removeAllObjects];
	c->arenaUsed = 0;
	c->cb = [c->queue commandBuffer];
	dispatch_semaphore_t s = c->inflight;
	atomic_int *lost = c->lost;
	[c->cb addCompletedHandler:^(id<MTLCommandBuffer> done) {
		if (done.status == MTLCommandBufferStatusError && done.error.code == deviceRemoved && !atomic_exchange(lost, 1))
			fprintf(stderr, "metal: GPU removed, drawing stopped\n");
		dispatch_semaphore_signal(s);
	}];
}

static void stop(Ctx *c, const char *what) {
	if (!atomic_exchange(c->lost, 1))
		fprintf(stderr, "metal: %s failed, drawing stopped\n", what);
}

static id<MTLBuffer> arenaAlloc(Ctx *c, const void *p, NSUInteger n, NSUInteger *off) {
	NSUInteger o = (c->arenaUsed + 255) & ~(NSUInteger)255;
	id<MTLBuffer> b = c->arena[c->frame];
	if (o + n > b.length) {
		NSUInteger sz = b.length * 2;
		while (sz < n)
			sz *= 2;
		b = [c->dev newBufferWithLength:sz options:MTLResourceStorageModeShared];
		if (!b) {
			stop(c, "buffer allocation");
			return nil;
		}
		[c->keep[c->frame] addObject:c->arena[c->frame]];
		c->arena[c->frame] = b;
		o = 0;
	}
	memcpy((char *)b.contents + o, p, n);
	c->arenaUsed = o + n;
	*off = o;
	return b;
}

void *mtl_texture(void *ctx, int w, int h) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		MTLTextureDescriptor *d = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm
		                                                                             width:w
		                                                                            height:h
		                                                                         mipmapped:NO];
		d.usage = MTLTextureUsageShaderRead | MTLTextureUsageRenderTarget;
		d.storageMode = MTLStorageModePrivate;
		id<MTLTexture> t = [c->dev newTextureWithDescriptor:d];
		return t ? (__bridge_retained void *)t : NULL;
	}
}

void mtl_end(void *ctx) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		if (c->enc) {
			[c->enc endEncoding];
			c->enc = nil;
			c->encTarget = nil;
		}
	}
}

int mtl_write(void *ctx, void *tex, const void *pix, int w, int y0, int h) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		mtl_end(ctx);
		ensureCB(c);
		id<MTLBuffer> staging = [c->dev newBufferWithBytes:pix length:(NSUInteger)w * h * 4 options:MTLResourceStorageModeShared];
		if (!staging)
			return 0;
		[c->keep[c->frame] addObject:staging];
		id<MTLBlitCommandEncoder> b = [c->cb blitCommandEncoder];
		[b copyFromBuffer:staging
		           sourceOffset:0
		      sourceBytesPerRow:(NSUInteger)w * 4
		    sourceBytesPerImage:(NSUInteger)w * h * 4
		             sourceSize:MTLSizeMake(w, h, 1)
		              toTexture:(__bridge id<MTLTexture>)tex
		       destinationSlice:0
		       destinationLevel:0
		      destinationOrigin:MTLOriginMake(0, y0, 0)];
		[b endEncoding];
		return 1;
	}
}

void *mtl_pipeline(void *ctx, const char *vsrc, const char *ventry, const char *fsrc, const char *fentry,
                   int instanced, char **err) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		NSError *e = nil;
		id<MTLLibrary> vl = [c->dev newLibraryWithSource:@(vsrc) options:nil error:&e];
		if (!vl) {
			*err = errdup(e, "metal vertex");
			return NULL;
		}
		id<MTLLibrary> fl = [c->dev newLibraryWithSource:@(fsrc) options:nil error:&e];
		if (!fl) {
			*err = errdup(e, "metal fragment");
			return NULL;
		}
		MTLVertexDescriptor *vd = [MTLVertexDescriptor vertexDescriptor];
		vd.attributes[0].format = MTLVertexFormatFloat2;
		vd.attributes[0].bufferIndex = 1;
		vd.layouts[1].stride = 8;
		if (instanced) {
			for (int i = 0; i < 7; i++) {
				vd.attributes[1 + i].format = MTLVertexFormatFloat4;
				vd.attributes[1 + i].offset = i * 16;
				vd.attributes[1 + i].bufferIndex = 2;
			}
			vd.layouts[2].stride = 7 * 16;
			vd.layouts[2].stepFunction = MTLVertexStepFunctionPerInstance;
		}
		MTLRenderPipelineDescriptor *d = [MTLRenderPipelineDescriptor new];
		d.vertexFunction = [vl newFunctionWithName:@(ventry)];
		d.fragmentFunction = [fl newFunctionWithName:@(fentry)];
		d.vertexDescriptor = vd;
		MTLRenderPipelineColorAttachmentDescriptor *a = d.colorAttachments[0];
		a.pixelFormat = MTLPixelFormatRGBA8Unorm;
		a.blendingEnabled = YES;
		a.sourceRGBBlendFactor = a.sourceAlphaBlendFactor = MTLBlendFactorOne;
		a.destinationRGBBlendFactor = a.destinationAlphaBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
		id<MTLRenderPipelineState> p = [c->dev newRenderPipelineStateWithDescriptor:d error:&e];
		if (!p) {
			*err = errdup(e, "metal pipeline");
			return NULL;
		}
		return (__bridge_retained void *)p;
	}
}

int mtl_begin(void *ctx, void *tex, int w, int h, int clear, float r, float g, float b, float a) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		mtl_end(ctx);
		ensureCB(c);
		id<MTLTexture> t;
		if (tex) {
			t = (__bridge id<MTLTexture>)tex;
		} else {
			if (!c->layer)
				return 0;
			if (!c->drawable) {
				CGSize want = CGSizeMake(w, h);
				if (!CGSizeEqualToSize(c->layer.drawableSize, want))
					c->layer.drawableSize = want;
				c->drawable = [c->layer nextDrawable];
				if (!c->drawable)
					return 0;
			}
			t = c->drawable.texture;
		}
		MTLRenderPassDescriptor *pd = [MTLRenderPassDescriptor renderPassDescriptor];
		pd.colorAttachments[0].texture = t;
		pd.colorAttachments[0].loadAction = clear ? MTLLoadActionClear : MTLLoadActionLoad;
		pd.colorAttachments[0].clearColor = MTLClearColorMake(r, g, b, a);
		pd.colorAttachments[0].storeAction = MTLStoreActionStore;
		c->enc = [c->cb renderCommandEncoderWithDescriptor:pd];
		c->encTarget = t;
		[c->enc setVertexBuffer:c->quad offset:0 atIndex:1];
		return 1;
	}
}

void mtl_scissor(void *ctx, int on, int x, int y, int w, int h) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		if (!c->enc)
			return;
		int tw = (int)c->encTarget.width, th = (int)c->encTarget.height;
		if (!on) {
			x = 0, y = 0, w = tw, h = th;
		}
		int x1 = x + w, y1 = y + h;
		x = x < 0 ? 0 : (x > tw ? tw : x);
		y = y < 0 ? 0 : (y > th ? th : y);
		x1 = x1 < x ? x : (x1 > tw ? tw : x1);
		y1 = y1 < y ? y : (y1 > th ? th : y1);
		[c->enc setScissorRect:(MTLScissorRect){(NSUInteger)x, (NSUInteger)y, (NSUInteger)(x1 - x), (NSUInteger)(y1 - y)}];
	}
}

void mtl_draw(void *ctx, void *pipe, void *tex, int nearest, const void *vub, int vlen, const void *fub, int flen,
              const void *inst, int instBytes, int count, int instanced) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		id<MTLRenderCommandEncoder> e = c->enc;
		[e setRenderPipelineState:(__bridge id<MTLRenderPipelineState>)pipe];
		if (vlen > 0)
			[e setVertexBytes:vub length:vlen atIndex:0];
		if (flen > 0)
			[e setFragmentBytes:fub length:flen atIndex:0];
		if (tex) {
			[e setFragmentTexture:(__bridge id<MTLTexture>)tex atIndex:0];
			[e setFragmentSamplerState:nearest ? c->nearest : c->linear atIndex:0];
		}
		if (instanced) {
			if (instBytes <= 4096) {
				[e setVertexBytes:inst length:instBytes atIndex:2];
			} else {
				NSUInteger off;
				id<MTLBuffer> b = arenaAlloc(c, inst, instBytes, &off);
				if (!b)
					return;
				[e setVertexBuffer:b offset:off atIndex:2];
			}
			[e drawPrimitives:MTLPrimitiveTypeTriangleStrip vertexStart:0 vertexCount:4 instanceCount:count];
		} else {
			[e drawPrimitives:MTLPrimitiveTypeTriangleStrip vertexStart:0 vertexCount:4];
		}
	}
}

void mtl_copy(void *ctx, void *src) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		id<MTLRenderCommandEncoder> e = c->enc;
		BOOL bgra = c->encTarget.pixelFormat == MTLPixelFormatBGRA8Unorm;
		[e setRenderPipelineState:bgra ? c->blitBGRA : c->blitRGBA];
		[e setFragmentTexture:(__bridge id<MTLTexture>)src atIndex:0];
		[e drawPrimitives:MTLPrimitiveTypeTriangleStrip vertexStart:0 vertexCount:4];
	}
}

static id<MTLCommandBuffer> submit(Ctx *c, BOOL wait, BOOL *presented) {
	if (!c->cb)
		return nil;
	if (c->drawable) {
		[c->cb presentDrawable:c->drawable];
		c->drawable = nil;
		if (presented)
			*presented = YES;
	}
	id<MTLCommandBuffer> cb = c->cb;
	c->cb = nil;
	[cb commit];
	if (wait)
		[cb waitUntilCompleted];
	return cb;
}

int mtl_read(void *ctx, void *tex, void *out, int w, int h) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		mtl_end(ctx);
		ensureCB(c);
		NSUInteger n = (NSUInteger)w * h * 4;
		id<MTLBuffer> buf = [c->dev newBufferWithLength:n options:MTLResourceStorageModeShared];
		if (!buf)
			return 0;
		[c->keep[c->frame] addObject:buf];
		id<MTLBlitCommandEncoder> b = [c->cb blitCommandEncoder];
		[b copyFromTexture:(__bridge id<MTLTexture>)tex
		                 sourceSlice:0
		                 sourceLevel:0
		                sourceOrigin:MTLOriginMake(0, 0, 0)
		                  sourceSize:MTLSizeMake(w, h, 1)
		                    toBuffer:buf
		           destinationOffset:0
		      destinationBytesPerRow:(NSUInteger)w * 4
		    destinationBytesPerImage:n];
		[b endEncoding];
		if (submit(c, YES, NULL).status != MTLCommandBufferStatusCompleted)
			return 0;
		memcpy(out, buf.contents, n);
		return 1;
	}
}

int mtl_present(void *ctx) {
	@autoreleasepool {
		Ctx *c = (__bridge Ctx *)ctx;
		mtl_end(ctx);
		BOOL presented = NO;
		submit(c, NO, &presented);
		return presented;
	}
}

int mtl_is_lost(void *ctx) {
	Ctx *c = (__bridge Ctx *)ctx;
	return atomic_load(c->lost);
}
