import { useEffect, useRef } from 'react';
import { Mesh, Program, Renderer, Triangle } from 'ogl';

type GalaxyProps = {
  mouseRepulsion?: boolean;
  mouseInteraction?: boolean;
  density?: number;
  glowIntensity?: number;
  saturation?: number;
  hueShift?: number;
  twinkleIntensity?: number;
  rotationSpeed?: number;
  repulsionStrength?: number;
  autoCenterRepulsion?: number;
  starSpeed?: number;
  speed?: number;
  transparent?: boolean;
};

const vertexShader = `
attribute vec2 position;
varying vec2 vUv;

void main() {
  vUv = position * 0.5 + 0.5;
  gl_Position = vec4(position, 0.0, 1.0);
}
`;

const fragmentShader = `
precision highp float;

uniform float uTime;
uniform vec2 uResolution;
uniform vec2 uMouse;
uniform float uMouseInteraction;
uniform float uMouseRepulsion;
uniform float uDensity;
uniform float uGlowIntensity;
uniform float uSaturation;
uniform float uHueShift;
uniform float uTwinkleIntensity;
uniform float uRotationSpeed;
uniform float uRepulsionStrength;
uniform float uAutoCenterRepulsion;
uniform float uStarSpeed;
uniform float uSpeed;
uniform float uTransparent;

varying vec2 vUv;

mat2 rotate2d(float angle) {
  float s = sin(angle);
  float c = cos(angle);
  return mat2(c, -s, s, c);
}

float hash21(vec2 p) {
  p = fract(p * vec2(123.34, 456.21));
  p += dot(p, p + 45.32);
  return fract(p.x * p.y);
}

vec3 hsv2rgb(vec3 c) {
  vec4 K = vec4(1.0, 2.0 / 3.0, 1.0 / 3.0, 3.0);
  vec3 p = abs(fract(c.xxx + K.xyz) * 6.0 - K.www);
  return c.z * mix(K.xxx, clamp(p - K.xxx, 0.0, 1.0), c.y);
}

float starLayer(vec2 uv, float scale, float seed, float density, float time) {
  vec2 grid = uv * scale;
  vec2 id = floor(grid);
  vec2 gv = fract(grid) - 0.5;
  float rnd = hash21(id + seed);
  float threshold = 1.0 - clamp(density, 0.0, 2.0) * 0.42;
  float starMask = step(threshold, rnd);
  vec2 offset = vec2(hash21(id + seed + 8.7), hash21(id + seed + 31.1)) - 0.5;
  float d = length(gv - offset * 0.48);
  float core = smoothstep(0.06, 0.0, d);
  float glow = smoothstep(0.22, 0.0, d) * 0.28;
  float twinkle = 1.0 + sin(time * (2.0 + rnd * 4.0) + rnd * 38.0) * uTwinkleIntensity;
  return starMask * (core + glow * uGlowIntensity) * twinkle;
}

void main() {
  vec2 uv = vUv;
  vec2 p = (uv - 0.5) * vec2(uResolution.x / max(uResolution.y, 1.0), 1.0);
  vec2 mouse = (uMouse - 0.5) * vec2(uResolution.x / max(uResolution.y, 1.0), 1.0);
  float t = uTime * uSpeed;

  if (uMouseInteraction > 0.5) {
    vec2 diff = p - mouse;
    float dist = max(length(diff), 0.05);
    float falloff = smoothstep(0.72, 0.0, dist);
    float direction = uMouseRepulsion > 0.5 ? 1.0 : -1.0;
    p += normalize(diff) * falloff * direction * uRepulsionStrength * 0.035;
  }

  if (uAutoCenterRepulsion > 0.0) {
    vec2 diff = p;
    float dist = max(length(diff), 0.05);
    p += normalize(diff) * smoothstep(0.72, 0.0, dist) * uAutoCenterRepulsion * 0.02;
  }

  p = rotate2d(t * uRotationSpeed) * p;

  float radial = length(p);
  vec2 flow = normalize(p + 0.0001) * t * uStarSpeed * 0.04;
  float galaxyBand = exp(-abs(p.y + sin(p.x * 2.8 + t * 0.16) * 0.08) * 4.2);

  float stars = 0.0;
  stars += starLayer(p + flow, 54.0, 3.0, uDensity, t);
  stars += starLayer(p * 1.35 - flow * 0.7, 86.0, 11.0, uDensity * 0.9, t);
  stars += starLayer(p * 1.9 + flow * 1.2, 132.0, 27.0, uDensity * 0.72, t);

  float haze = galaxyBand * (0.09 + 0.18 * uGlowIntensity) * smoothstep(1.0, 0.12, radial);
  float hue = fract((uHueShift / 360.0) + radial * 0.18 + p.x * 0.035);
  vec3 tint = hsv2rgb(vec3(hue, clamp(uSaturation, 0.0, 1.0), 1.0));
  vec3 color = tint * (stars * (1.0 + uGlowIntensity * 1.8) + haze);
  color += vec3(0.02, 0.026, 0.045) * smoothstep(1.2, 0.0, radial);

  float alpha = uTransparent > 0.5 ? clamp(stars + haze, 0.0, 1.0) : 1.0;
  gl_FragColor = vec4(color, alpha);
}
`;

export default function Galaxy({
  mouseRepulsion = false,
  mouseInteraction = true,
  density = 1,
  glowIntensity = 0.4,
  saturation = 0.4,
  hueShift = 80,
  twinkleIntensity = 0.3,
  rotationSpeed = 0.1,
  repulsionStrength = 2,
  autoCenterRepulsion = 0,
  starSpeed = 0.8,
  speed = 0.7,
  transparent = false,
}: GalaxyProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const uniformsRef = useRef<Program['uniforms'] | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return undefined;

    const renderer = new Renderer({
      alpha: true,
      antialias: false,
      dpr: Math.min(window.devicePixelRatio || 1, 2),
    });
    const { gl } = renderer;
    gl.clearColor(0, 0, 0, transparent ? 0 : 1);
    gl.canvas.className = 'galaxy-canvas';
    container.appendChild(gl.canvas);

    const geometry = new Triangle(gl);
    const program = new Program(gl, {
      vertex: vertexShader,
      fragment: fragmentShader,
      uniforms: {
        uTime: { value: 0 },
        uResolution: { value: [1, 1] },
        uMouse: { value: [0.5, 0.5] },
        uMouseInteraction: { value: mouseInteraction ? 1 : 0 },
        uMouseRepulsion: { value: mouseRepulsion ? 1 : 0 },
        uDensity: { value: density },
        uGlowIntensity: { value: glowIntensity },
        uSaturation: { value: saturation },
        uHueShift: { value: hueShift },
        uTwinkleIntensity: { value: twinkleIntensity },
        uRotationSpeed: { value: rotationSpeed },
        uRepulsionStrength: { value: repulsionStrength },
        uAutoCenterRepulsion: { value: autoCenterRepulsion },
        uStarSpeed: { value: starSpeed },
        uSpeed: { value: speed },
        uTransparent: { value: transparent ? 1 : 0 },
      },
    });
    uniformsRef.current = program.uniforms;

    const mesh = new Mesh(gl, { geometry, program });
    let animationFrame = 0;

    const setSize = () => {
      const rect = container.getBoundingClientRect();
      renderer.setSize(Math.max(1, rect.width), Math.max(1, rect.height));
      program.uniforms.uResolution.value = [gl.canvas.width, gl.canvas.height];
    };

    const resizeObserver = new ResizeObserver(setSize);
    resizeObserver.observe(container);
    setSize();

    const onMouseMove = (event: MouseEvent) => {
      const rect = container.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0) return;
      program.uniforms.uMouse.value = [
        (event.clientX - rect.left) / rect.width,
        1 - (event.clientY - rect.top) / rect.height,
      ];
    };
    window.addEventListener('mousemove', onMouseMove);

    const animate = (time: number) => {
      program.uniforms.uTime.value = time * 0.001;
      renderer.render({ scene: mesh });
      animationFrame = window.requestAnimationFrame(animate);
    };
    animationFrame = window.requestAnimationFrame(animate);

    return () => {
      window.cancelAnimationFrame(animationFrame);
      window.removeEventListener('mousemove', onMouseMove);
      resizeObserver.disconnect();
      uniformsRef.current = null;
      geometry.remove();
      program.remove();
      gl.canvas.remove();
    };
  }, [
    autoCenterRepulsion,
    density,
    glowIntensity,
    hueShift,
    mouseInteraction,
    mouseRepulsion,
    repulsionStrength,
    rotationSpeed,
    saturation,
    speed,
    starSpeed,
    transparent,
    twinkleIntensity,
  ]);

  return <div ref={containerRef} className="galaxy-container" />;
}
