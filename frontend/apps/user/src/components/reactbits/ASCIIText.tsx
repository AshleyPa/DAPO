import { useEffect, useRef } from 'react';
import * as THREE from 'three';

const vertexShader = `
varying vec2 vUv;
uniform float uTime;
uniform float uEnableWaves;

void main() {
  vUv = uv;
  float time = uTime * 5.0;
  float waveFactor = uEnableWaves;
  vec3 transformed = position;
  transformed.x += sin(time + position.y) * 0.5 * waveFactor;
  transformed.y += cos(time + position.z) * 0.15 * waveFactor;
  transformed.z += sin(time + position.x) * waveFactor;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(transformed, 1.0);
}
`;

const fragmentShader = `
varying vec2 vUv;
uniform float uTime;
uniform sampler2D uTexture;

void main() {
  float time = uTime;
  vec2 pos = vUv;
  float r = texture2D(uTexture, pos + cos(time * 2.0 - time + pos.x) * 0.01).r;
  float g = texture2D(uTexture, pos + tan(time * 0.5 + pos.x - time) * 0.01).g;
  float b = texture2D(uTexture, pos - cos(time * 2.0 + time + pos.y) * 0.01).b;
  float a = texture2D(uTexture, pos).a;
  gl_FragColor = vec4(r, g, b, a);
}
`;

type ASCIITextProps = {
  text?: string;
  asciiFontSize?: number;
  textFontSize?: number;
  textColor?: string;
  planeBaseHeight?: number;
  enableWaves?: boolean;
};

const pixelRatio = () => (typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1);
const mapRange = (n: number, start: number, stop: number, start2: number, stop2: number) => ((n - start) / (stop - start)) * (stop2 - start2) + start2;

class AsciiFilter {
  readonly domElement: HTMLDivElement;
  private readonly pre: HTMLPreElement;
  private readonly canvas: HTMLCanvasElement;
  private readonly context: CanvasRenderingContext2D;
  private width = 1;
  private height = 1;
  private deg = 0;
  private center = { x: 0, y: 0 };
  private mouse = { x: 0, y: 0 };
  private readonly fontSize: number;
  private readonly fontFamily: string;
  private readonly charset: string;
  private readonly invert: boolean;

  constructor(private readonly renderer: THREE.WebGLRenderer, options: { fontSize?: number; fontFamily?: string; charset?: string; invert?: boolean } = {}) {
    this.domElement = document.createElement('div');
    this.domElement.className = 'ascii-text-container';

    this.pre = document.createElement('pre');
    this.domElement.appendChild(this.pre);

    this.canvas = document.createElement('canvas');
    const context = this.canvas.getContext('2d', { willReadFrequently: true });
    if (!context) throw new Error('Canvas 2D context is unavailable');
    this.context = context;

    this.fontSize = options.fontSize ?? 12;
    this.fontFamily = options.fontFamily ?? '"IBM Plex Mono", "Courier New", monospace';
    this.charset = options.charset ?? ' .\'`^",:;Il!i~+_-?][}{1)(|/tfjrxnuvczXYUJCLQ0OZmwqpdbkhao*#MW&8%B@$';
    this.invert = options.invert ?? true;
    this.context.imageSmoothingEnabled = false;
    this.onMouseMove = this.onMouseMove.bind(this);
    document.addEventListener('mousemove', this.onMouseMove);
  }

  setSize(width: number, height: number) {
    this.width = Math.max(1, width);
    this.height = Math.max(1, height);
    this.renderer.setSize(this.width, this.height, false);
    this.reset();
    this.center = { x: this.width / 2, y: this.height / 2 };
    this.mouse = { x: this.center.x, y: this.center.y };
  }

  reset() {
    this.context.font = `${this.fontSize}px ${this.fontFamily}`;
    const charWidth = Math.max(1, this.context.measureText('A').width);
    this.canvas.width = Math.max(1, Math.floor(this.width / charWidth));
    this.canvas.height = Math.max(1, Math.floor(this.height / this.fontSize));

    this.pre.style.fontFamily = this.fontFamily;
    this.pre.style.fontSize = `${this.fontSize}px`;
    this.pre.style.lineHeight = '1em';
  }

  render(scene: THREE.Scene, camera: THREE.Camera) {
    this.renderer.render(scene, camera);
    this.context.clearRect(0, 0, this.canvas.width, this.canvas.height);
    this.context.drawImage(this.renderer.domElement, 0, 0, this.canvas.width, this.canvas.height);
    this.asciify();
    this.hue();
  }

  dispose() {
    document.removeEventListener('mousemove', this.onMouseMove);
  }

  private onMouseMove(event: MouseEvent) {
    this.mouse = { x: event.clientX * pixelRatio(), y: event.clientY * pixelRatio() };
  }

  private hue() {
    const deg = (Math.atan2(this.mouse.y - this.center.y, this.mouse.x - this.center.x) * 180) / Math.PI;
    this.deg += (deg - this.deg) * 0.075;
    this.domElement.style.filter = `hue-rotate(${this.deg.toFixed(1)}deg)`;
  }

  private asciify() {
    const data = this.context.getImageData(0, 0, this.canvas.width, this.canvas.height).data;
    let output = '';
    for (let y = 0; y < this.canvas.height; y += 1) {
      for (let x = 0; x < this.canvas.width; x += 1) {
        const i = x * 4 + y * 4 * this.canvas.width;
        const r = data[i] ?? 0;
        const g = data[i + 1] ?? 0;
        const b = data[i + 2] ?? 0;
        const a = data[i + 3] ?? 0;
        if (a === 0) {
          output += ' ';
          continue;
        }
        const gray = (0.3 * r + 0.6 * g + 0.1 * b) / 255;
        let idx = Math.floor((1 - gray) * (this.charset.length - 1));
        if (this.invert) idx = this.charset.length - idx - 1;
        output += this.charset[Math.max(0, Math.min(this.charset.length - 1, idx))] ?? ' ';
      }
      output += '\n';
    }
    this.pre.textContent = output;
  }
}

class CanvasText {
  readonly canvas = document.createElement('canvas');
  private readonly context: CanvasRenderingContext2D;
  private readonly fontFamily = '"IBM Plex Mono", "Courier New", monospace';

  constructor(private readonly text: string, private readonly fontSize: number, private readonly color: string) {
    const context = this.canvas.getContext('2d');
    if (!context) throw new Error('Canvas 2D context is unavailable');
    this.context = context;
  }

  resize() {
    this.context.font = this.font;
    const metrics = this.context.measureText(this.text);
    this.canvas.width = Math.ceil(metrics.width) + 28;
    this.canvas.height = Math.ceil(metrics.actualBoundingBoxAscent + metrics.actualBoundingBoxDescent) + 28;
  }

  render() {
    this.context.clearRect(0, 0, this.canvas.width, this.canvas.height);
    this.context.fillStyle = this.color;
    this.context.font = this.font;
    const metrics = this.context.measureText(this.text);
    this.context.fillText(this.text, 14, 14 + metrics.actualBoundingBoxAscent);
  }

  get aspect() {
    return this.canvas.width / Math.max(1, this.canvas.height);
  }

  private get font() {
    return `600 ${this.fontSize}px ${this.fontFamily}`;
  }
}

class CanvAscii {
  private readonly camera: THREE.PerspectiveCamera;
  private readonly scene = new THREE.Scene();
  private readonly textCanvas: CanvasText;
  private readonly texture: THREE.CanvasTexture;
  private readonly mesh: THREE.Mesh<THREE.PlaneGeometry, THREE.ShaderMaterial>;
  private readonly renderer: THREE.WebGLRenderer;
  private readonly filter: AsciiFilter;
  private readonly planeWidth: number;
  private readonly planeHeight: number;
  private animationFrameId = 0;
  private mouse = { x: 0, y: 0 };

  constructor(
    options: Required<ASCIITextProps>,
    private readonly container: HTMLElement,
    private width: number,
    private height: number,
  ) {
    this.camera = new THREE.PerspectiveCamera(45, this.width / this.height, 1, 1000);
    this.camera.position.z = 30;

    this.textCanvas = new CanvasText(options.text, options.textFontSize, options.textColor);
    this.textCanvas.resize();
    this.textCanvas.render();
    this.texture = new THREE.CanvasTexture(this.textCanvas.canvas);
    this.texture.minFilter = THREE.NearestFilter;

    this.planeHeight = options.planeBaseHeight;
    this.planeWidth = this.planeHeight * this.textCanvas.aspect;
    const geometry = new THREE.PlaneGeometry(this.planeWidth, this.planeHeight, 36, 36);
    const material = new THREE.ShaderMaterial({
      vertexShader,
      fragmentShader,
      transparent: true,
      uniforms: {
        uTime: { value: 0 },
        uTexture: { value: this.texture },
        uEnableWaves: { value: options.enableWaves ? 1 : 0 },
      },
    });
    this.mesh = new THREE.Mesh(geometry, material);
    this.scene.add(this.mesh);

    this.renderer = new THREE.WebGLRenderer({ antialias: false, alpha: true });
    this.renderer.setPixelRatio(1);
    this.renderer.setClearColor(0x000000, 0);
    this.filter = new AsciiFilter(this.renderer, {
      fontFamily: '"IBM Plex Mono", "Courier New", monospace',
      fontSize: options.asciiFontSize,
      invert: true,
    });
    this.container.appendChild(this.filter.domElement);
    this.setSize(this.width, this.height);
    this.onMouseMove = this.onMouseMove.bind(this);
    this.container.addEventListener('mousemove', this.onMouseMove);
    this.container.addEventListener('touchmove', this.onMouseMove, { passive: true });
  }

  load() {
    const animate = () => {
      this.animationFrameId = window.requestAnimationFrame(animate);
      this.render();
    };
    animate();
  }

  setSize(width: number, height: number) {
    this.width = Math.max(1, width);
    this.height = Math.max(1, height);
    this.camera.aspect = this.width / this.height;
    this.camera.updateProjectionMatrix();
    this.filter.setSize(this.width, this.height);
    this.fitMeshToViewport();
  }

  dispose() {
    window.cancelAnimationFrame(this.animationFrameId);
    this.container.removeEventListener('mousemove', this.onMouseMove);
    this.container.removeEventListener('touchmove', this.onMouseMove);
    this.filter.dispose();
    this.filter.domElement.remove();
    this.scene.traverse((object) => {
      const mesh = object as THREE.Mesh;
      if (mesh.isMesh) {
        mesh.geometry.dispose();
        if (Array.isArray(mesh.material)) mesh.material.forEach((material) => material.dispose());
        else mesh.material.dispose();
      }
    });
    this.scene.clear();
    this.renderer.dispose();
    this.renderer.forceContextLoss();
  }

  private onMouseMove(event: MouseEvent | TouchEvent) {
    const pointer = 'touches' in event ? event.touches[0] : event;
    if (!pointer) return;
    const bounds = this.container.getBoundingClientRect();
    this.mouse = { x: pointer.clientX - bounds.left, y: pointer.clientY - bounds.top };
  }

  private render() {
    const time = Date.now() * 0.001;
    this.textCanvas.render();
    this.texture.needsUpdate = true;
    const timeUniform = this.mesh.material.uniforms.uTime;
    if (timeUniform) timeUniform.value = Math.sin(time);
    this.mesh.rotation.x += (mapRange(this.mouse.y, 0, this.height, 0.5, -0.5) - this.mesh.rotation.x) * 0.05;
    this.mesh.rotation.y += (mapRange(this.mouse.x, 0, this.width, -0.5, 0.5) - this.mesh.rotation.y) * 0.05;
    this.filter.render(this.scene, this.camera);
  }

  private fitMeshToViewport() {
    const visibleHeight = 2 * Math.tan(THREE.MathUtils.degToRad(this.camera.fov / 2)) * this.camera.position.z;
    const visibleWidth = visibleHeight * this.camera.aspect;
    const scale = Math.min(1, (visibleWidth * 0.86) / this.planeWidth, (visibleHeight * 0.72) / this.planeHeight);
    this.mesh.scale.setScalar(Math.max(0.1, scale));
  }
}

export default function ASCIIText({
  text = 'Hello World!',
  asciiFontSize = 8,
  textFontSize = 180,
  textColor = '#fdf9f3',
  planeBaseHeight = 7,
  enableWaves = true,
}: ASCIITextProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const asciiRef = useRef<CanvAscii | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return undefined;

    let cancelled = false;
    let resizeObserver: ResizeObserver | null = null;
    let intersectionObserver: IntersectionObserver | null = null;

    const create = (width: number, height: number) => {
      if (cancelled || asciiRef.current) return;
      const instance = new CanvAscii({ text, asciiFontSize, textFontSize, textColor, planeBaseHeight, enableWaves }, container, width, height);
      asciiRef.current = instance;
      instance.load();
      resizeObserver = new ResizeObserver((entries) => {
        const rect = entries[0]?.contentRect;
        if (rect && rect.width > 0 && rect.height > 0) asciiRef.current?.setSize(rect.width, rect.height);
      });
      resizeObserver.observe(container);
    };

    const rect = container.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0) {
      create(rect.width, rect.height);
    } else {
      intersectionObserver = new IntersectionObserver((entries) => {
        const entry = entries[0];
        if (!entry || !entry.isIntersecting) return;
        const visibleRect = entry.boundingClientRect;
        if (visibleRect.width > 0 && visibleRect.height > 0) {
          intersectionObserver?.disconnect();
          intersectionObserver = null;
          create(visibleRect.width, visibleRect.height);
        }
      });
      intersectionObserver.observe(container);
    }

    return () => {
      cancelled = true;
      resizeObserver?.disconnect();
      intersectionObserver?.disconnect();
      asciiRef.current?.dispose();
      asciiRef.current = null;
    };
  }, [asciiFontSize, enableWaves, planeBaseHeight, text, textColor, textFontSize]);

  return <div ref={containerRef} className="ascii-text-container" aria-hidden="true" />;
}
