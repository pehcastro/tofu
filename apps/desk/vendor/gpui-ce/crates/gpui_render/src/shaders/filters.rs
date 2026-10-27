#[wgsl_rs::wgsl]
pub mod surface {
    use super::super::common::*;
    use wgsl_rs::std::*;

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct SurfaceUniforms {
        pub bounds: Bounds,
        pub content_mask: ContentMask,
        pub color_format: SurfaceColorFormat,
        pub opacity: f32,
        pub padding0: u32,
        pub padding1: u32,
        pub padding2: u32,
        pub padding3: u32,
        pub padding4: u32,
        pub padding5: u32,
    }
    uniform!(group(1), binding(0), SURFACE_LOCALS: SurfaceUniforms);
    texture!(group(1), binding(1), SURFACE_TEXTURE: Texture2D<f32>);
    texture!(group(1), binding(2), SURFACE_CHROMA_TEXTURE: Texture2D<f32>);
    sampler!(group(1), binding(3), SURFACE_SAMPLER: Sampler);

    pub const YCBCR_TO_LINEAR_RGB: Mat4x4f = mat4x4f(
        vec4f(1.0000, 1.0000, 1.0000, 0.0),
        vec4f(0.0000, -0.3441, 1.7720, 0.0),
        vec4f(1.4020, -0.7141, 0.0000, 0.0),
        vec4f(-0.7010, 0.5291, -0.8860, 1.0),
    );

    pub fn sample_yuv_surface(texture_position: Vec2f) -> Vec4f {
        let luma = texture_sample_level(SURFACE_TEXTURE, SURFACE_SAMPLER, texture_position, 0.0).x;
        let chroma = texture_sample_level(
            SURFACE_CHROMA_TEXTURE,
            SURFACE_SAMPLER,
            texture_position,
            0.0,
        )
        .xy();
        YCBCR_TO_LINEAR_RGB * vec4f(luma, chroma.x, chroma.y, 1.0)
    }

    #[derive(Wgsl)]
    pub struct SurfaceVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_position: Vec2f,
        #[location(3)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_surface(#[builtin(vertex_index)] vertex_id: u32) -> SurfaceVarying {
        let vertex = rectangle_vertex(vertex_id, get!(SURFACE_LOCALS).bounds);
        SurfaceVarying {
            position: vertex.clip_position,
            texture_position: vertex.unit_position,
            clip_distances: clip_distances(
                vertex.viewport_position,
                get!(SURFACE_LOCALS).content_mask.bounds,
            ),
        }
    }

    #[fragment]
    pub fn fragment_surface(input: SurfaceVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let locals = get!(SURFACE_LOCALS);
        let fade = ContentMask::alpha(locals.content_mask, input.position.xy());
        if locals.color_format == SurfaceColorFormat::Yuv {
            return sample_yuv_surface(input.texture_position) * locals.opacity * fade;
        }
        texture_sample_level(
            SURFACE_TEXTURE,
            SURFACE_SAMPLER,
            input.texture_position,
            0.0,
        ) * locals.opacity
            * fade
    }
}

#[wgsl_rs::wgsl]
pub mod blur {
    use super::super::common::*;
    use super::super::corner_smoothing::*;
    use wgsl_rs::std::*;

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct BlurUniforms {
        pub bounds: Bounds,
        pub content_mask: Bounds,
        pub corner_radii: Corners,
        pub direction: Vec2f,
        pub standard_deviation: f32,
        pub opacity: f32,
        pub sample_count: u32,
        pub sample_step: f32,
        pub composite_clip: BlurCompositeClip,
        pub downsample_mode: DownsampleMode,
        pub source_size: Vec2f,
        pub target_size: Vec2f,
        pub corner_smoothing: f32,
        pub frosted: ShaderBool,
        pub padding1: u32,
        pub padding2: u32,
        pub lens: Vec4f,
    }
    uniform!(group(1), binding(0), BLUR_LOCALS: BlurUniforms);
    texture!(group(1), binding(1), BLUR_TEXTURE: Texture2D<f32>);
    sampler!(group(1), binding(2), BLUR_SAMPLER: Sampler);

    #[derive(Clone, Copy, Wgsl)]
    pub struct BlurCompositeVertexData {
        pub position: Vec4f,
        pub texture_coordinates: Vec2f,
        pub clip_distances: Vec4f,
    }

    pub fn prepare_blur_composite_vertex(vertex_id: u32) -> BlurCompositeVertexData {
        let vertex = rectangle_vertex(vertex_id, get!(BLUR_LOCALS).bounds);
        BlurCompositeVertexData {
            position: vertex.clip_position,
            texture_coordinates: vertex.unit_position,
            clip_distances: clip_distances(
                vertex.viewport_position,
                get!(BLUR_LOCALS).content_mask,
            ),
        }
    }

    pub fn blur_composite_color(position: Vec2f, coverage: f32) -> Vec4f {
        let blurred = texture_sample_level(
            BLUR_TEXTURE,
            BLUR_SAMPLER,
            position / get!(BLUR_LOCALS).target_size,
            0.0,
        );
        let factor = coverage * get!(BLUR_LOCALS).opacity;
        vec4f(
            blurred.x * factor,
            blurred.y * factor,
            blurred.z * factor,
            blurred.w * factor,
        )
    }

    #[derive(Wgsl)]
    pub struct BlurVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_coordinates: Vec2f,
        #[location(3)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_blur_fullscreen(#[builtin(vertex_index)] vertex_id: u32) -> BlurVarying {
        let vertex = fullscreen_vertex(vertex_id);
        BlurVarying {
            position: vertex.clip_position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: unclipped_distances(),
        }
    }

    #[fragment]
    pub fn fragment_blur_downsample(input: BlurVarying) -> Vec4f {
        if get!(BLUR_LOCALS).downsample_mode == DownsampleMode::HalfResolution {
            let destination = floor(input.position.xy());
            let source_coordinates = min(
                (destination + 0.5) / get!(BLUR_LOCALS).target_size,
                (get!(BLUR_LOCALS).source_size - 0.5) / get!(BLUR_LOCALS).source_size,
            );
            return texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, source_coordinates, 0.0);
        }
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, input.texture_coordinates, 0.0)
    }

    pub fn sample_blur_texture(texture_coordinates: Vec2f) -> Vec4f {
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, texture_coordinates, 0.0)
    }

    pub fn gaussian_blur(texture_coordinates: Vec2f) -> Vec4f {
        let uniforms = get!(BLUR_LOCALS);
        let center_weight = gaussian(0.0, uniforms.standard_deviation);
        let mut weighted_color = sample_blur_texture(texture_coordinates) * center_weight;
        let mut weight_sum = center_weight;
        let mut sample_index = 1u32;

        while sample_index <= uniforms.sample_count {
            let distance = sample_index as f32 * uniforms.sample_step;
            let weight = gaussian(distance, uniforms.standard_deviation);
            let coordinate_offset = uniforms.direction * distance;
            let symmetric_pair = sample_blur_texture(texture_coordinates - coordinate_offset)
                + sample_blur_texture(texture_coordinates + coordinate_offset);
            weighted_color = weighted_color + symmetric_pair * weight;
            weight_sum += 2.0 * weight;
            sample_index += 1u32;
        }
        weighted_color / max(weight_sum, MIN_NORMALIZED_WEIGHT)
    }

    #[fragment]
    pub fn fragment_blur(input: BlurVarying) -> Vec4f {
        gaussian_blur(input.texture_coordinates)
    }

    #[vertex]
    pub fn vertex_blur_composite(#[builtin(vertex_index)] vertex_id: u32) -> BlurVarying {
        let vertex = prepare_blur_composite_vertex(vertex_id);
        BlurVarying {
            position: vertex.position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: vertex.clip_distances,
        }
    }

    #[fragment]
    pub fn fragment_blur_composite(input: BlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(rounded_rectangle_signed_distance(
                input.position.xy(),
                get!(BLUR_LOCALS).bounds,
                get!(BLUR_LOCALS).corner_radii,
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        blur_composite_color(input.position.xy(), coverage)
    }

    pub const LENS_LIGHT: Vec2f = vec2f(-0.371, -0.928);
    pub const LENS_BACK_LIGHT: f32 = 0.35;
    pub const LENS_RIM_SHARE: f32 = 0.2;
    pub const LENS_RIM_MIN_WIDTH: f32 = 1.0;
    pub const LENS_RIM_STRENGTH: f32 = 0.32;
    pub const LENS_MIN_EDGE: f32 = 0.5;
    pub const LENS_MIN_GRADIENT: f32 = 0.0001;
    pub const LENS_MIN_PROFILE: f32 = 0.001;
    pub const LENS_CLEAR_SHARE: f32 = 0.6;
    pub const LENS_RED_WEIGHTS: Vec4f = vec4f(0.22, 0.04, 0.01, 0.2);
    pub const LENS_ORANGE_WEIGHTS: Vec4f = vec4f(0.40, 0.22, 0.10, 0.2);
    pub const LENS_GREEN_WEIGHTS: Vec4f = vec4f(0.27, 0.48, 0.27, 0.2);
    pub const LENS_CYAN_WEIGHTS: Vec4f = vec4f(0.10, 0.22, 0.40, 0.2);
    pub const LENS_BLUE_WEIGHTS: Vec4f = vec4f(0.01, 0.04, 0.22, 0.2);

    pub fn lens_distance(position: Vec2f) -> f32 {
        rounded_rectangle_signed_distance(
            position,
            get!(BLUR_LOCALS).bounds,
            get!(BLUR_LOCALS).corner_radii,
        )
    }

    pub fn lens_sample(position: Vec2f) -> Vec4f {
        texture_sample_level(
            BLUR_TEXTURE,
            BLUR_SAMPLER,
            position / get!(BLUR_LOCALS).target_size,
            0.0,
        )
    }

    pub fn lens_profile(depth: f32, edge: f32, shape: f32) -> f32 {
        let rise = saturate(1.0 - depth / edge);
        let circular = 1.0 - sqrt(max(1.0 - rise * rise, 0.0));
        let blended = mix(rise, circular, saturate(shape));
        let squared = rise * rise;
        let superellipse = 1.0 - sqrt(sqrt(max(1.0 - squared * squared, 0.0)));
        let profile = mix(blended, superellipse, saturate(shape - 1.0));
        select(0.0, profile, depth < edge)
    }

    pub fn lens_normal(position: Vec2f) -> Vec2f {
        let gradient = vec2f(
            lens_distance(position + vec2f(1.0, 0.0)) - lens_distance(position - vec2f(1.0, 0.0)),
            lens_distance(position + vec2f(0.0, 1.0)) - lens_distance(position - vec2f(0.0, 1.0)),
        );
        gradient / max(length(gradient), LENS_MIN_GRADIENT)
    }

    pub fn lens_color(center: Vec2f, split: Vec2f, dispersed: bool) -> Vec4f {
        if dispersed {
            return lens_sample(center - split) * LENS_RED_WEIGHTS
                + lens_sample(center - split * 0.5) * LENS_ORANGE_WEIGHTS
                + lens_sample(center) * LENS_GREEN_WEIGHTS
                + lens_sample(center + split * 0.5) * LENS_CYAN_WEIGHTS
                + lens_sample(center + split) * LENS_BLUE_WEIGHTS;
        }
        lens_sample(center)
    }

    #[fragment]
    pub fn fragment_refract_composite(input: BlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let locals = get!(BLUR_LOCALS);
        let position = input.position.xy();
        let distance = lens_distance(position);
        let depth = max(-distance, 0.0);
        let edge = max(locals.lens.y, LENS_MIN_EDGE);
        if depth >= edge && locals.frosted == ShaderBool::Disabled {
            return transparent();
        }
        let normal = lens_normal(position);
        let profile = lens_profile(depth, edge, locals.lens.z);
        let center = position - normal * (locals.lens.x * profile);
        let split = normal * (locals.lens.w * profile);
        let color = lens_color(
            center,
            split,
            locals.lens.w > 0.0 && profile > LENS_MIN_PROFILE,
        );
        let rim = saturate(1.0 - depth / max(edge * LENS_RIM_SHARE, LENS_RIM_MIN_WIDTH));
        let facing = saturate(dot(normal, LENS_LIGHT))
            + LENS_BACK_LIGHT * saturate(-dot(normal, LENS_LIGHT));
        let glint = saturate(LENS_RIM_STRENGTH * rim * facing);
        let lit = vec4f(
            color.x + (color.w - color.x) * glint,
            color.y + (color.w - color.y) * glint,
            color.z + (color.w - color.z) * glint,
            color.w,
        );
        let reach = saturate(saturate(1.0 - depth / edge) / LENS_CLEAR_SHARE);
        let clear = select(
            1.0,
            reach * reach * (3.0 - 2.0 * reach),
            locals.frosted == ShaderBool::Disabled,
        );
        lit * (antialiased_coverage(distance) * locals.opacity * clear)
    }

    #[derive(Wgsl)]
    pub struct SmoothedBlurVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_coordinates: Vec2f,
        #[location(1)]
        #[interpolate(flat)]
        pub horizontal_corner_reaches: Vec4f,
        #[location(2)]
        #[interpolate(flat)]
        pub vertical_corner_reaches: Vec4f,
        #[location(3)]
        pub clip_distances: Vec4f,
        #[location(4)]
        #[interpolate(flat)]
        pub smoothing_factors: Vec4f,
        #[location(5)]
        #[interpolate(flat)]
        pub superellipse_power: f32,
    }

    #[vertex]
    pub fn vertex_smoothed_blur_composite(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> SmoothedBlurVarying {
        let vertex = prepare_blur_composite_vertex(vertex_id);
        let prepared = prepare_corners(
            get!(BLUR_LOCALS).bounds.size,
            get!(BLUR_LOCALS).corner_radii,
            get!(BLUR_LOCALS).corner_smoothing,
            true,
        );
        SmoothedBlurVarying {
            position: vertex.position,
            texture_coordinates: vertex.texture_coordinates,
            horizontal_corner_reaches: prepared.horizontal_reaches,
            vertical_corner_reaches: prepared.vertical_reaches,
            clip_distances: vertex.clip_distances,
            smoothing_factors: prepared.smoothing_factors,
            superellipse_power: prepared.superellipse_power,
        }
    }

    #[fragment]
    pub fn fragment_smoothed_blur_composite(input: SmoothedBlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(prepared_corner_signed_distance(
                input.position.xy(),
                get!(BLUR_LOCALS).bounds,
                get!(BLUR_LOCALS).corner_radii,
                get!(BLUR_LOCALS).corner_smoothing,
                PreparedCorners {
                    horizontal_reaches: input.horizontal_corner_reaches,
                    vertical_reaches: input.vertical_corner_reaches,
                    smoothing_factors: input.smoothing_factors,
                    superellipse_power: input.superellipse_power,
                },
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        blur_composite_color(input.position.xy(), coverage)
    }
}
