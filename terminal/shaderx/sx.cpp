#include "sx.h"

#include <cstdlib>
#include <cstring>
#include <exception>
#include <string>
#include <vector>

#include "glslang/Public/ShaderLang.h"
#include "glslang/Public/ResourceLimits.h"
#include "SPIRV/GlslangToSpv.h"

#include "spirv_hlsl.hpp"
#include "spirv_msl.hpp"

static char *dup(const std::string &s) {
	char *p = (char *)malloc(s.size() + 1);
	memcpy(p, s.c_str(), s.size() + 1);
	return p;
}

static bool initialized = false;

static int toMSL(const std::vector<uint32_t> &spv, spv::ExecutionModel model, unsigned ubo, sx_out *o) {
	spirv_cross::CompilerMSL msl(spv);
	auto opts = msl.get_msl_options();
	opts.platform = spirv_cross::CompilerMSL::Options::macOS;
	opts.set_msl_version(2, 1);
	msl.set_msl_options(opts);
	spirv_cross::MSLResourceBinding b;
	b.stage = model;
	b.desc_set = 0;
	b.binding = ubo;
	b.msl_buffer = 0;
	msl.add_msl_resource_binding(b);
	spirv_cross::MSLResourceBinding t;
	t.stage = model;
	t.desc_set = 0;
	t.binding = 2;
	t.msl_texture = 0;
	t.msl_sampler = 0;
	msl.add_msl_resource_binding(t);
	o->msl = dup(msl.compile());
	o->msl_entry = dup(msl.get_cleansed_entry_point_name("main", model));
	return 0;
}

static int toHLSL(const std::vector<uint32_t> &spv, spv::ExecutionModel model, unsigned ubo, sx_out *o) {
	spirv_cross::CompilerHLSL hlsl(spv);
	auto opts = hlsl.get_hlsl_options();
	opts.shader_model = 50;
	hlsl.set_hlsl_options(opts);
	spirv_cross::HLSLResourceBinding b;
	b.stage = model;
	b.desc_set = 0;
	b.binding = ubo;
	b.cbv.register_binding = 0;
	hlsl.add_hlsl_resource_binding(b);
	spirv_cross::HLSLResourceBinding t;
	t.stage = model;
	t.desc_set = 0;
	t.binding = 2;
	t.srv.register_binding = 0;
	t.sampler.register_binding = 0;
	hlsl.add_hlsl_resource_binding(t);
	o->hlsl = dup(hlsl.compile());
	return 0;
}

static void reflect(const std::vector<uint32_t> &spv, sx_out *o) {
	spirv_cross::Compiler c(spv);
	auto res = c.get_shader_resources();
	o->has_tex = !res.sampled_images.empty();
	if (res.uniform_buffers.empty())
		return;
	auto &ub = res.uniform_buffers[0];
	auto &type = c.get_type(ub.base_type_id);
	int n = (int)type.member_types.size();
	o->ubo_size = (int)c.get_declared_struct_size(type);
	o->n_members = n;
	o->names = (char **)calloc(n, sizeof(char *));
	o->offsets = (int *)calloc(n, sizeof(int));
	o->sizes = (int *)calloc(n, sizeof(int));
	for (int i = 0; i < n; i++) {
		o->names[i] = dup(c.get_member_name(ub.base_type_id, i));
		o->offsets[i] = (int)c.type_struct_member_offset(type, i);
		o->sizes[i] = (int)c.get_declared_struct_member_size(type, i);
	}
}

extern "C" int sx_compile(int frag, const char *src, unsigned ubo_binding, int want, sx_out *o) {
	memset(o, 0, sizeof(*o));
	if (!initialized) {
		glslang::InitializeProcess();
		initialized = true;
	}
	EShLanguage lang = frag ? EShLangFragment : EShLangVertex;
	spv::ExecutionModel model = frag ? spv::ExecutionModelFragment : spv::ExecutionModelVertex;

	glslang::TShader sh(lang);
	sh.setStrings(&src, 1);
	sh.setEnvInput(glslang::EShSourceGlsl, lang, glslang::EShClientVulkan, 100);
	sh.setEnvClient(glslang::EShClientVulkan, glslang::EShTargetVulkan_1_0);
	sh.setEnvTarget(glslang::EShTargetSpv, glslang::EShTargetSpv_1_0);
	// GL-style loose uniforms are collected into one block per stage
	sh.setEnvInputVulkanRulesRelaxed();
	sh.setGlobalUniformBlockName("caution_uniforms");
	sh.setGlobalUniformSet(0);
	sh.setGlobalUniformBinding(ubo_binding);
	sh.setAutoMapLocations(true);
	sh.setAutoMapBindings(true);
	EShMessages msgs = (EShMessages)(EShMsgSpvRules | EShMsgVulkanRules);
	if (!sh.parse(GetDefaultResources(), 450, false, msgs)) {
		o->err = dup(std::string(sh.getInfoLog()));
		return 1;
	}
	glslang::TProgram prog;
	prog.addShader(&sh);
	if (!prog.link(msgs) || !prog.mapIO()) {
		o->err = dup(std::string(prog.getInfoLog()));
		return 1;
	}
	std::vector<uint32_t> spv;
	glslang::SpvOptions so;
	so.disableOptimizer = true;
	glslang::GlslangToSpv(*prog.getIntermediate(lang), spv, &so);

	try {
		reflect(spv, o);
		if (want & SX_MSL)
			toMSL(spv, model, ubo_binding, o);
		if (want & SX_HLSL)
			toHLSL(spv, model, ubo_binding, o);
	} catch (const std::exception &e) {
		o->err = dup(std::string("spirv-cross: ") + e.what());
		return 1;
	}
	o->spirv_words = (int)spv.size();
	o->spirv = (unsigned int *)malloc(spv.size() * 4);
	memcpy(o->spirv, spv.data(), spv.size() * 4);
	return 0;
}

extern "C" void sx_free(sx_out *o) {
	free(o->spirv);
	free(o->msl);
	free(o->msl_entry);
	free(o->hlsl);
	free(o->err);
	for (int i = 0; i < o->n_members; i++)
		free(o->names[i]);
	free(o->names);
	free(o->offsets);
	free(o->sizes);
	memset(o, 0, sizeof(*o));
}
