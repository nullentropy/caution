#define COBJMACROS
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <initguid.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <d3dcompiler.h>
#include <float.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "d3d11_windows.h"

#define INST_STRIDE (7 * 16)

struct dx_tex {
	ID3D11Texture2D *tex;
	ID3D11ShaderResourceView *srv;
	ID3D11RenderTargetView *rtv;
	int w, h;
};

struct dx_pipe {
	ID3D11VertexShader *vs;
	ID3D11PixelShader *ps;
	ID3D11InputLayout *layout;
	ID3D11Buffer *vcb, *fcb;
	int vlen, flen, instanced;
};

struct dx_ctx {
	ID3D11Device *dev;
	ID3D11DeviceContext *ctx;
	pD3DCompile compile;

	HWND hwnd;
	IDXGISwapChain1 *sc;
	ID3D11RenderTargetView *bbrtv;
	int bbw, bbh;
	int drawn;
	int lost;

	ID3D11Buffer *quad;
	ID3D11Buffer *inst;
	UINT instCap, instUsed;
	ID3D11SamplerState *linear, *nearest;
	ID3D11BlendState *blend;
	ID3D11RasterizerState *raster;

	ID3D11RenderTargetView *rtv;
	ID3D11Resource *res;
	int tw, th, onWindow;
};

static char *errf(const char *what, HRESULT hr) {
	char buf[256];
	snprintf(buf, sizeof buf, "d3d11: %s failed (hr 0x%08lx)", what, (unsigned long)hr);
	return strdup(buf);
}

#define REL(x)                                                                                                         \
	do {                                                                                                               \
		if (x) {                                                                                                       \
			IUnknown_Release((IUnknown *)(x));                                                                         \
			(x) = NULL;                                                                                                \
		}                                                                                                              \
	} while (0)

static void stopDevice(dx_ctx *c, const char *what) {
	if (!c->lost)
		fprintf(stderr, "d3d11: %s failed, drawing stopped\n", what);
	c->lost = 1;
}

// the device reports removal on any call, even on a device with no window
// or one that hasn't presented since
static int checkRemoved(dx_ctx *c) {
	HRESULT why = ID3D11Device_GetDeviceRemovedReason(c->dev);
	if (why == S_OK)
		return 0;
	if (!c->lost)
		fprintf(stderr, "d3d11: device removed (reason 0x%08lx), drawing stopped\n", (unsigned long)why);
	c->lost = 1;
	return 1;
}

static HRESULT createDevice(D3D_DRIVER_TYPE type, UINT flags, ID3D11Device **dev, ID3D11DeviceContext **ctx) {
	D3D_FEATURE_LEVEL levels[] = {D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0};
	HRESULT hr = D3D11CreateDevice(NULL, type, NULL, flags, levels, 2, D3D11_SDK_VERSION, dev, NULL, ctx);
	// the 11.0 runtime rejects 11_1 outright
	if (hr == E_INVALIDARG)
		hr = D3D11CreateDevice(NULL, type, NULL, flags, levels + 1, 1, D3D11_SDK_VERSION, dev, NULL, ctx);
	return hr;
}

dx_ctx *dx_new(void *hwnd, char **err) {
	HMODULE dll = LoadLibraryA("d3dcompiler_47.dll");
	if (!dll) {
		*err = strdup("d3d11: d3dcompiler_47.dll not found");
		return NULL;
	}
	pD3DCompile compile = (pD3DCompile)(void *)GetProcAddress(dll, "D3DCompile");
	if (!compile) {
		*err = strdup("d3d11: D3DCompile missing from d3dcompiler_47.dll");
		return NULL;
	}

	dx_ctx *c = calloc(1, sizeof *c);
	c->compile = compile;
	c->hwnd = (HWND)hwnd;

	UINT flags = 0;
	const char *dbg = getenv("CAUTION_D3D_DEBUG");
	if (dbg && *dbg && strcmp(dbg, "0") != 0)
		flags |= D3D11_CREATE_DEVICE_DEBUG;
	HRESULT hr = createDevice(D3D_DRIVER_TYPE_HARDWARE, flags, &c->dev, &c->ctx);
	// the debug layer is an optional Windows feature
	if (FAILED(hr) && flags) {
		fprintf(stderr, "d3d11: no debug layer (hr 0x%08lx), continuing without\n", (unsigned long)hr);
		flags = 0;
		hr = createDevice(D3D_DRIVER_TYPE_HARDWARE, flags, &c->dev, &c->ctx);
	}
	if (FAILED(hr))
		hr = createDevice(D3D_DRIVER_TYPE_WARP, flags, &c->dev, &c->ctx);
	if (FAILED(hr)) {
		*err = errf("D3D11CreateDevice", hr);
		free(c);
		return NULL;
	}

	float unit[] = {0, 0, 1, 0, 0, 1, 1, 1};
	D3D11_BUFFER_DESC bd = {sizeof unit, D3D11_USAGE_IMMUTABLE, D3D11_BIND_VERTEX_BUFFER, 0, 0, 0};
	D3D11_SUBRESOURCE_DATA sd = {unit, 0, 0};
	hr = ID3D11Device_CreateBuffer(c->dev, &bd, &sd, &c->quad);
	if (FAILED(hr)) {
		*err = errf("quad buffer", hr);
		return NULL;
	}

	D3D11_SAMPLER_DESC smp = {0};
	smp.AddressU = smp.AddressV = smp.AddressW = D3D11_TEXTURE_ADDRESS_CLAMP;
	smp.ComparisonFunc = D3D11_COMPARISON_NEVER;
	smp.MaxLOD = FLT_MAX;
	smp.Filter = D3D11_FILTER_MIN_MAG_MIP_LINEAR;
	hr = ID3D11Device_CreateSamplerState(c->dev, &smp, &c->linear);
	smp.Filter = D3D11_FILTER_MIN_MAG_MIP_POINT;
	if (SUCCEEDED(hr))
		hr = ID3D11Device_CreateSamplerState(c->dev, &smp, &c->nearest);
	if (FAILED(hr)) {
		*err = errf("sampler", hr);
		return NULL;
	}

	D3D11_BLEND_DESC bl = {0};
	bl.RenderTarget[0].BlendEnable = TRUE;
	bl.RenderTarget[0].SrcBlend = bl.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
	bl.RenderTarget[0].DestBlend = bl.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_INV_SRC_ALPHA;
	bl.RenderTarget[0].BlendOp = bl.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
	bl.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL;
	hr = ID3D11Device_CreateBlendState(c->dev, &bl, &c->blend);
	if (FAILED(hr)) {
		*err = errf("blend state", hr);
		return NULL;
	}

	D3D11_RASTERIZER_DESC rs = {0};
	rs.FillMode = D3D11_FILL_SOLID;
	rs.CullMode = D3D11_CULL_NONE;
	rs.DepthClipEnable = TRUE;
	rs.ScissorEnable = TRUE;
	hr = ID3D11Device_CreateRasterizerState(c->dev, &rs, &c->raster);
	if (FAILED(hr)) {
		*err = errf("rasterizer state", hr);
		return NULL;
	}

	UINT stride = 8, off = 0;
	ID3D11DeviceContext_IASetVertexBuffers(c->ctx, 0, 1, &c->quad, &stride, &off);
	ID3D11DeviceContext_IASetPrimitiveTopology(c->ctx, D3D11_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP);
	float factor[4] = {0, 0, 0, 0};
	ID3D11DeviceContext_OMSetBlendState(c->ctx, c->blend, factor, 0xffffffff);
	ID3D11DeviceContext_RSSetState(c->ctx, c->raster);
	return c;
}

dx_tex *dx_texture(dx_ctx *c, int w, int h) {
	if (c->lost)
		return NULL;
	dx_tex *t = calloc(1, sizeof *t);
	t->w = w, t->h = h;
	D3D11_TEXTURE2D_DESC d = {0};
	d.Width = w > 0 ? w : 1;
	d.Height = h > 0 ? h : 1;
	d.MipLevels = d.ArraySize = 1;
	d.Format = DXGI_FORMAT_R8G8B8A8_UNORM;
	d.SampleDesc.Count = 1;
	d.Usage = D3D11_USAGE_DEFAULT;
	d.BindFlags = D3D11_BIND_SHADER_RESOURCE | D3D11_BIND_RENDER_TARGET;
	if (FAILED(ID3D11Device_CreateTexture2D(c->dev, &d, NULL, &t->tex)) ||
	    FAILED(ID3D11Device_CreateShaderResourceView(c->dev, (ID3D11Resource *)t->tex, NULL, &t->srv)) ||
	    FAILED(ID3D11Device_CreateRenderTargetView(c->dev, (ID3D11Resource *)t->tex, NULL, &t->rtv))) {
		checkRemoved(c);
		REL(t->rtv);
		REL(t->srv);
		REL(t->tex);
		free(t);
		return NULL;
	}
	return t;
}

// the write is ordered after draws already issued, and the runtime renames
// the resource when it must
void dx_write(dx_ctx *c, dx_tex *t, const void *pix, int y0, int h) {
	if (c->lost)
		return;
	D3D11_BOX box = {0, (UINT)y0, 0, (UINT)t->w, (UINT)(y0 + h), 1};
	if (t->tex)
		ID3D11DeviceContext_UpdateSubresource(c->ctx, (ID3D11Resource *)t->tex, 0, &box, pix, (UINT)t->w * 4, 0);
}

void dx_tex_free(dx_ctx *c, dx_tex *t) {
	if (t->rtv && c->rtv == t->rtv) {
		c->rtv = NULL;
		c->res = NULL;
		ID3D11DeviceContext_OMSetRenderTargets(c->ctx, 0, NULL, NULL);
	}
	REL(t->rtv);
	REL(t->srv);
	REL(t->tex);
	free(t);
}

static ID3DBlob *compileStage(dx_ctx *c, const char *src, const char *profile, char **err) {
	ID3DBlob *code = NULL, *msgs = NULL;
	HRESULT hr = c->compile(src, strlen(src), NULL, NULL, NULL, "main", profile, D3DCOMPILE_OPTIMIZATION_LEVEL3, 0,
	                        &code, &msgs);
	if (FAILED(hr)) {
		const char *m = msgs ? (const char *)ID3D10Blob_GetBufferPointer(msgs) : "no message";
		size_t n = strlen(m) + strlen(profile) + 64;
		*err = malloc(n);
		snprintf(*err, n, "d3d11: %s: %s", profile, m);
		REL(msgs);
		REL(code);
		return NULL;
	}
	REL(msgs);
	return code;
}

static ID3D11Buffer *constantBuffer(dx_ctx *c, int len) {
	if (len <= 0)
		return NULL;
	D3D11_BUFFER_DESC d = {(UINT)((len + 15) & ~15), D3D11_USAGE_DYNAMIC, D3D11_BIND_CONSTANT_BUFFER,
	                       D3D11_CPU_ACCESS_WRITE, 0, 0};
	ID3D11Buffer *b = NULL;
	ID3D11Device_CreateBuffer(c->dev, &d, NULL, &b);
	return b;
}

dx_pipe *dx_pipeline(dx_ctx *c, const char *vsrc, const char *fsrc, int vubo, int fubo, int instanced, char **err) {
	if (c->lost) {
		*err = strdup("d3d11: device removed");
		return NULL;
	}
	ID3DBlob *vb = compileStage(c, vsrc, "vs_5_0", err);
	if (!vb)
		return NULL;
	ID3DBlob *fb = compileStage(c, fsrc, "ps_5_0", err);
	if (!fb) {
		REL(vb);
		return NULL;
	}
	dx_pipe *p = calloc(1, sizeof *p);
	p->instanced = instanced;
	p->vlen = vubo, p->flen = fubo;
	HRESULT hr = ID3D11Device_CreateVertexShader(c->dev, ID3D10Blob_GetBufferPointer(vb), ID3D10Blob_GetBufferSize(vb),
	                                             NULL, &p->vs);
	if (SUCCEEDED(hr))
		hr = ID3D11Device_CreatePixelShader(c->dev, ID3D10Blob_GetBufferPointer(fb), ID3D10Blob_GetBufferSize(fb), NULL,
		                                    &p->ps);
	// SPIRV-Cross names vertex inputs TEXCOORD<location>
	D3D11_INPUT_ELEMENT_DESC el[8];
	el[0] = (D3D11_INPUT_ELEMENT_DESC){"TEXCOORD", 0, DXGI_FORMAT_R32G32_FLOAT, 0, 0, D3D11_INPUT_PER_VERTEX_DATA, 0};
	for (int i = 1; i < 8; i++)
		el[i] = (D3D11_INPUT_ELEMENT_DESC){"TEXCOORD", (UINT)i, DXGI_FORMAT_R32G32B32A32_FLOAT, 1, (UINT)(i - 1) * 16,
		                                   D3D11_INPUT_PER_INSTANCE_DATA, 1};
	if (SUCCEEDED(hr))
		hr = ID3D11Device_CreateInputLayout(c->dev, el, instanced ? 8 : 1, ID3D10Blob_GetBufferPointer(vb),
		                                    ID3D10Blob_GetBufferSize(vb), &p->layout);
	REL(vb);
	REL(fb);
	if (FAILED(hr)) {
		*err = errf("pipeline", hr);
		dx_pipe_free(p);
		return NULL;
	}
	p->vcb = constantBuffer(c, vubo);
	p->fcb = constantBuffer(c, fubo);
	if ((vubo > 0 && !p->vcb) || (fubo > 0 && !p->fcb)) {
		*err = strdup("d3d11: constant buffer");
		dx_pipe_free(p);
		return NULL;
	}
	return p;
}

void dx_pipe_free(dx_pipe *p) {
	REL(p->vs);
	REL(p->ps);
	REL(p->layout);
	REL(p->vcb);
	REL(p->fcb);
	free(p);
}

static void releaseBackbuffer(dx_ctx *c) {
	if (c->onWindow) {
		c->rtv = NULL;
		c->res = NULL;
	}
	ID3D11DeviceContext_OMSetRenderTargets(c->ctx, 0, NULL, NULL);
	REL(c->bbrtv);
	ID3D11DeviceContext_Flush(c->ctx);
}

static int acquireBackbuffer(dx_ctx *c) {
	ID3D11Texture2D *bb = NULL;
	HRESULT hr = IDXGISwapChain1_GetBuffer(c->sc, 0, &IID_ID3D11Texture2D, (void **)&bb);
	if (FAILED(hr))
		return 0;
	hr = ID3D11Device_CreateRenderTargetView(c->dev, (ID3D11Resource *)bb, NULL, &c->bbrtv);
	REL(bb);
	return SUCCEEDED(hr);
}

static int createSwapchain(dx_ctx *c, int w, int h) {
	IDXGIDevice *dxdev = NULL;
	IDXGIAdapter *adapter = NULL;
	IDXGIFactory2 *factory = NULL;
	HRESULT hr = ID3D11Device_QueryInterface(c->dev, &IID_IDXGIDevice, (void **)&dxdev);
	if (SUCCEEDED(hr))
		hr = IDXGIDevice_GetAdapter(dxdev, &adapter);
	if (SUCCEEDED(hr))
		hr = IDXGIAdapter_GetParent(adapter, &IID_IDXGIFactory2, (void **)&factory);
	if (SUCCEEDED(hr)) {
		DXGI_SWAP_CHAIN_DESC1 d = {0};
		d.Width = w;
		d.Height = h;
		d.Format = DXGI_FORMAT_R8G8B8A8_UNORM;
		d.SampleDesc.Count = 1;
		d.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;
		d.BufferCount = 2;
		d.Scaling = DXGI_SCALING_NONE;
		d.SwapEffect = DXGI_SWAP_EFFECT_FLIP_DISCARD;
		d.AlphaMode = DXGI_ALPHA_MODE_IGNORE;
		hr = IDXGIFactory2_CreateSwapChainForHwnd(factory, (IUnknown *)c->dev, c->hwnd, &d, NULL, NULL, &c->sc);
		// FLIP_DISCARD needs Windows 10 and FLIP_SEQUENTIAL needs Windows 8
		if (FAILED(hr)) {
			d.SwapEffect = DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL;
			hr = IDXGIFactory2_CreateSwapChainForHwnd(factory, (IUnknown *)c->dev, c->hwnd, &d, NULL, NULL, &c->sc);
		}
		if (FAILED(hr)) {
			d.Scaling = DXGI_SCALING_STRETCH;
			hr = IDXGIFactory2_CreateSwapChainForHwnd(factory, (IUnknown *)c->dev, c->hwnd, &d, NULL, NULL, &c->sc);
		}
		// GLFW owns fullscreen, and DXGI's alt-enter would fight it
		if (SUCCEEDED(hr))
			IDXGIFactory2_MakeWindowAssociation(factory, c->hwnd, DXGI_MWA_NO_ALT_ENTER);
	}
	REL(factory);
	REL(adapter);
	REL(dxdev);
	if (FAILED(hr)) {
		if (!checkRemoved(c))
			fprintf(stderr, "d3d11: swapchain failed (hr 0x%08lx)\n", (unsigned long)hr);
		return 0;
	}
	c->bbw = w, c->bbh = h;
	return acquireBackbuffer(c);
}

static int windowTarget(dx_ctx *c, int w, int h) {
	if (!c->hwnd || w <= 0 || h <= 0)
		return 0;
	if (!c->sc)
		return createSwapchain(c, w, h);
	if (w != c->bbw || h != c->bbh || !c->bbrtv) {
		releaseBackbuffer(c);
		HRESULT hr = IDXGISwapChain1_ResizeBuffers(c->sc, 0, w, h, DXGI_FORMAT_UNKNOWN, 0);
		if (FAILED(hr)) {
			if (checkRemoved(c))
				return 0;
			fprintf(stderr, "d3d11: ResizeBuffers %dx%d failed (hr 0x%08lx)\n", w, h, (unsigned long)hr);
			return 0;
		}
		c->bbw = w, c->bbh = h;
		return acquireBackbuffer(c);
	}
	return 1;
}

static void bind(dx_ctx *c) {
	// a texture still bound for sampling cannot also be the render target
	ID3D11ShaderResourceView *none = NULL;
	ID3D11DeviceContext_PSSetShaderResources(c->ctx, 0, 1, &none);
	ID3D11DeviceContext_OMSetRenderTargets(c->ctx, c->rtv ? 1 : 0, c->rtv ? &c->rtv : NULL, NULL);
	D3D11_VIEWPORT vp = {0, 0, (float)c->tw, (float)c->th, 0, 1};
	ID3D11DeviceContext_RSSetViewports(c->ctx, 1, &vp);
}

int dx_target(dx_ctx *c, dx_tex *t, int w, int h) {
	if (c->lost)
		return 0;
	c->rtv = NULL;
	c->res = NULL;
	c->onWindow = t == NULL;
	if (t) {
		c->rtv = t->rtv;
		c->res = (ID3D11Resource *)t->tex;
		c->tw = t->w, c->th = t->h;
	} else {
		c->tw = w, c->th = h;
		if (windowTarget(c, w, h)) {
			c->rtv = c->bbrtv;
			ID3D11RenderTargetView_GetResource(c->bbrtv, &c->res);
			// the view keeps the backbuffer alive, so a second reference isn't needed
			ID3D11Resource_Release(c->res);
		}
	}
	bind(c);
	return c->rtv != NULL;
}

void dx_clear(dx_ctx *c, float r, float g, float b, float a) {
	if (c->lost)
		return;
	if (!c->rtv)
		return;
	float rgba[4] = {r, g, b, a};
	ID3D11DeviceContext_ClearRenderTargetView(c->ctx, c->rtv, rgba);
	if (c->onWindow)
		c->drawn = 1;
}

void dx_scissor(dx_ctx *c, int on, int x, int y, int w, int h) {
	if (c->lost)
		return;
	int tw = c->tw, th = c->th;
	if (!on) {
		x = 0, y = 0, w = tw, h = th;
	}
	int x1 = x + w, y1 = y + h;
	x = x < 0 ? 0 : (x > tw ? tw : x);
	y = y < 0 ? 0 : (y > th ? th : y);
	x1 = x1 < x ? x : (x1 > tw ? tw : x1);
	y1 = y1 < y ? y : (y1 > th ? th : y1);
	D3D11_RECT r = {x, y, x1, y1};
	ID3D11DeviceContext_RSSetScissorRects(c->ctx, 1, &r);
}

static void upload(dx_ctx *c, ID3D11Buffer *b, const void *p, int n) {
	D3D11_MAPPED_SUBRESOURCE m;
	if (SUCCEEDED(ID3D11DeviceContext_Map(c->ctx, (ID3D11Resource *)b, 0, D3D11_MAP_WRITE_DISCARD, 0, &m))) {
		memcpy(m.pData, p, n);
		ID3D11DeviceContext_Unmap(c->ctx, (ID3D11Resource *)b, 0);
	}
}

static int instances(dx_ctx *c, const void *p, UINT n) {
	D3D11_MAP mode = D3D11_MAP_WRITE_NO_OVERWRITE;
	if (n > c->instCap) {
		UINT cap = c->instCap ? c->instCap : 1 << 20;
		while (cap < n)
			cap *= 2;
		REL(c->inst);
		D3D11_BUFFER_DESC d = {cap, D3D11_USAGE_DYNAMIC, D3D11_BIND_VERTEX_BUFFER, D3D11_CPU_ACCESS_WRITE, 0, 0};
		if (FAILED(ID3D11Device_CreateBuffer(c->dev, &d, NULL, &c->inst))) {
			c->instCap = 0;
			stopDevice(c, "instance buffer allocation");
			return -1;
		}
		c->instCap = cap;
		c->instUsed = 0;
		mode = D3D11_MAP_WRITE_DISCARD;
	} else if (c->instUsed + n > c->instCap) {
		c->instUsed = 0;
		mode = D3D11_MAP_WRITE_DISCARD;
	}
	D3D11_MAPPED_SUBRESOURCE m;
	if (FAILED(ID3D11DeviceContext_Map(c->ctx, (ID3D11Resource *)c->inst, 0, mode, 0, &m)))
		return -1;
	UINT off = c->instUsed;
	memcpy((char *)m.pData + off, p, n);
	ID3D11DeviceContext_Unmap(c->ctx, (ID3D11Resource *)c->inst, 0);
	c->instUsed = (off + n + 15) & ~15u;
	return (int)off;
}

void dx_draw(dx_ctx *c, dx_pipe *p, dx_tex *t, int nearest, const void *vub, int vlen, const void *fub, int flen,
             const void *inst, int instBytes, int count) {
	if (c->lost)
		return;
	if (!c->rtv)
		return;
	ID3D11DeviceContext *x = c->ctx;
	ID3D11DeviceContext_IASetInputLayout(x, p->layout);
	ID3D11DeviceContext_VSSetShader(x, p->vs, NULL, 0);
	ID3D11DeviceContext_PSSetShader(x, p->ps, NULL, 0);
	if (p->vcb && vlen > 0) {
		upload(c, p->vcb, vub, vlen < p->vlen ? vlen : p->vlen);
		ID3D11DeviceContext_VSSetConstantBuffers(x, 0, 1, &p->vcb);
	}
	if (p->fcb && flen > 0) {
		upload(c, p->fcb, fub, flen < p->flen ? flen : p->flen);
		ID3D11DeviceContext_PSSetConstantBuffers(x, 0, 1, &p->fcb);
	}
	ID3D11ShaderResourceView *srv = t ? t->srv : NULL;
	ID3D11DeviceContext_PSSetShaderResources(x, 0, 1, &srv);
	ID3D11DeviceContext_PSSetSamplers(x, 0, 1, nearest ? &c->nearest : &c->linear);
	if (p->instanced) {
		if (count <= 0 || instBytes < count * INST_STRIDE)
			return;
		int off = instances(c, inst, (UINT)count * INST_STRIDE);
		if (off < 0)
			return;
		UINT stride = INST_STRIDE, uoff = (UINT)off;
		ID3D11DeviceContext_IASetVertexBuffers(x, 1, 1, &c->inst, &stride, &uoff);
		ID3D11DeviceContext_DrawInstanced(x, 4, (UINT)count, 0, 0);
	} else {
		ID3D11DeviceContext_Draw(x, 4, 0);
	}
	if (c->onWindow)
		c->drawn = 1;
}

// textures and the swapchain are both RGBA8, so a resource copy works
void dx_copy(dx_ctx *c, dx_tex *src) {
	if (c->lost)
		return;
	if (!c->rtv || !src->tex)
		return;
	ID3D11ShaderResourceView *none = NULL;
	ID3D11DeviceContext_PSSetShaderResources(c->ctx, 0, 1, &none);
	int w = src->w < c->tw ? src->w : c->tw, h = src->h < c->th ? src->h : c->th;
	if (w <= 0 || h <= 0)
		return;
	D3D11_BOX box = {0, 0, 0, (UINT)w, (UINT)h, 1};
	ID3D11DeviceContext_CopySubresourceRegion(c->ctx, c->res, 0, 0, 0, 0, (ID3D11Resource *)src->tex, 0, &box);
	if (c->onWindow)
		c->drawn = 1;
}

int dx_read(dx_ctx *c, dx_tex *t, void *out) {
	if (c->lost)
		return 0;
	if (!t->tex)
		return 0;
	D3D11_TEXTURE2D_DESC d = {0};
	d.Width = t->w;
	d.Height = t->h;
	d.MipLevels = d.ArraySize = 1;
	d.Format = DXGI_FORMAT_R8G8B8A8_UNORM;
	d.SampleDesc.Count = 1;
	d.Usage = D3D11_USAGE_STAGING;
	d.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
	ID3D11Texture2D *st = NULL;
	if (FAILED(ID3D11Device_CreateTexture2D(c->dev, &d, NULL, &st)))
		return 0;
	ID3D11DeviceContext_CopyResource(c->ctx, (ID3D11Resource *)st, (ID3D11Resource *)t->tex);
	D3D11_MAPPED_SUBRESOURCE m;
	int ok = 0;
	if (SUCCEEDED(ID3D11DeviceContext_Map(c->ctx, (ID3D11Resource *)st, 0, D3D11_MAP_READ, 0, &m))) {
		size_t row = (size_t)t->w * 4;
		for (int y = 0; y < t->h; y++)
			memcpy((char *)out + y * row, (const char *)m.pData + (size_t)y * m.RowPitch, row);
		ID3D11DeviceContext_Unmap(c->ctx, (ID3D11Resource *)st, 0);
		ok = 1;
	}
	REL(st);
	return ok;
}

int dx_present(dx_ctx *c) {
	if (c->lost || checkRemoved(c))
		return 0;
	if (!c->sc || !c->drawn) {
		ID3D11DeviceContext_Flush(c->ctx);
		return 0;
	}
	c->drawn = 0;
	HRESULT hr = IDXGISwapChain1_Present(c->sc, 1, 0);
	if ((hr == DXGI_ERROR_DEVICE_REMOVED || hr == DXGI_ERROR_DEVICE_RESET) && checkRemoved(c))
		return 0;
	if (FAILED(hr))
		fprintf(stderr, "d3d11: Present failed (hr 0x%08lx)\n", (unsigned long)hr);
	// the flip model unbinds the backbuffer on present. buffer 0 then names the
	// next backbuffer, and the same view still points at it
	if (c->onWindow)
		bind(c);
	return SUCCEEDED(hr);
}

int dx_is_lost(dx_ctx *c) { return c->lost || checkRemoved(c); }
