import { Camera, Mesh, Plane, Program, Renderer, Texture, Transform } from 'ogl';
import { useEffect, useRef } from 'react';

type GL = Renderer['gl'];

export type CircularGalleryItem = {
  id: string;
  title: string;
  subtitle?: string;
  image: string;
  prompt?: string;
};

type GalleryTextureItem = CircularGalleryItem & {
  text: string;
  sourceIndex: number;
};

type CircularGalleryProps = {
  items: CircularGalleryItem[];
  onSelect: (item: CircularGalleryItem) => void;
  className?: string;
  bend?: number;
  textColor?: string;
  borderRadius?: number;
  font?: string;
  scrollSpeed?: number;
  scrollEase?: number;
};

function debounce<T extends (...args: unknown[]) => void>(func: T, wait: number) {
  let timeout: number;
  return function debounced(this: unknown, ...args: Parameters<T>) {
    window.clearTimeout(timeout);
    timeout = window.setTimeout(() => func.apply(this, args), wait);
  };
}

function lerp(p1: number, p2: number, t: number) {
  return p1 + (p2 - p1) * t;
}

function getFontSize(font: string) {
  const match = font.match(/(\d+)px/);
  return match?.[1] ? parseInt(match[1], 10) : 30;
}

function createTextTexture(gl: GL, text: string, font = 'bold 30px monospace', color = '#ffffff') {
  const canvas = document.createElement('canvas');
  const context = canvas.getContext('2d');
  if (!context) throw new Error('Could not get 2d context');

  context.font = font;
  const metrics = context.measureText(text);
  const textWidth = Math.ceil(metrics.width);
  const fontSize = getFontSize(font);
  const textHeight = Math.ceil(fontSize * 1.2);

  canvas.width = textWidth + 20;
  canvas.height = textHeight + 20;

  context.font = font;
  context.fillStyle = color;
  context.textBaseline = 'middle';
  context.textAlign = 'center';
  context.clearRect(0, 0, canvas.width, canvas.height);
  context.fillText(text, canvas.width / 2, canvas.height / 2);

  const texture = new Texture(gl, { generateMipmaps: false });
  texture.image = canvas;
  return { texture, width: canvas.width, height: canvas.height };
}

class Title {
  mesh!: Mesh;

  constructor(
    private readonly gl: GL,
    private readonly plane: Mesh,
    private readonly text: string,
    private readonly textColor = '#ffffff',
    private readonly font = 'bold 30px Figtree',
  ) {
    this.createMesh();
  }

  private createMesh() {
    const { texture, width, height } = createTextTexture(this.gl, this.text, this.font, this.textColor);
    const geometry = new Plane(this.gl);
    const program = new Program(this.gl, {
      vertex: `
        attribute vec3 position;
        attribute vec2 uv;
        uniform mat4 modelViewMatrix;
        uniform mat4 projectionMatrix;
        varying vec2 vUv;
        void main() {
          vUv = uv;
          gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
        }
      `,
      fragment: `
        precision highp float;
        uniform sampler2D tMap;
        varying vec2 vUv;
        void main() {
          vec4 color = texture2D(tMap, vUv);
          if (color.a < 0.1) discard;
          gl_FragColor = color;
        }
      `,
      uniforms: { tMap: { value: texture } },
      transparent: true,
    });
    this.mesh = new Mesh(this.gl, { geometry, program });
    const aspect = width / height;
    const textHeightScaled = this.plane.scale.y * 0.15;
    const textWidthScaled = textHeightScaled * aspect;
    this.mesh.scale.set(textWidthScaled, textHeightScaled, 1);
    this.mesh.position.y = -this.plane.scale.y * 0.5 - textHeightScaled * 0.5 - 0.05;
    this.mesh.setParent(this.plane);
  }
}

class Media {
  extra = 0;
  plane!: Mesh;
  title!: Title;
  scale = 1;
  padding = 2;
  width = 1;
  widthTotal = 1;
  x = 0;
  speed = 0;
  isBefore = false;
  isAfter = false;

  constructor(
    private readonly geometry: Plane,
    private readonly gl: GL,
    readonly item: GalleryTextureItem,
    private readonly index: number,
    private readonly length: number,
    private readonly scene: Transform,
    private screen: { width: number; height: number },
    private viewport: { width: number; height: number },
    private readonly bend: number,
    private readonly textColor: string,
    private readonly borderRadius = 0,
    private readonly font = 'bold 30px Figtree',
  ) {
    this.createMesh();
    this.createTitle();
    this.onResize();
  }

  private createMesh() {
    const texture = new Texture(this.gl, { generateMipmaps: true });
    const program = new Program(this.gl, {
      depthTest: false,
      depthWrite: false,
      vertex: `
        precision highp float;
        attribute vec3 position;
        attribute vec2 uv;
        uniform mat4 modelViewMatrix;
        uniform mat4 projectionMatrix;
        uniform float uTime;
        uniform float uSpeed;
        varying vec2 vUv;
        void main() {
          vUv = uv;
          vec3 p = position;
          p.z = (sin(p.x * 4.0 + uTime) * 1.5 + cos(p.y * 2.0 + uTime) * 1.5) * (0.1 + uSpeed * 0.5);
          gl_Position = projectionMatrix * modelViewMatrix * vec4(p, 1.0);
        }
      `,
      fragment: `
        precision highp float;
        uniform vec2 uImageSizes;
        uniform vec2 uPlaneSizes;
        uniform sampler2D tMap;
        uniform float uBorderRadius;
        varying vec2 vUv;

        float roundedBoxSDF(vec2 p, vec2 b, float r) {
          vec2 d = abs(p) - b;
          return length(max(d, vec2(0.0))) + min(max(d.x, d.y), 0.0) - r;
        }

        void main() {
          vec2 ratio = vec2(
            min((uPlaneSizes.x / uPlaneSizes.y) / (uImageSizes.x / uImageSizes.y), 1.0),
            min((uPlaneSizes.y / uPlaneSizes.x) / (uImageSizes.y / uImageSizes.x), 1.0)
          );
          vec2 uv = vec2(
            vUv.x * ratio.x + (1.0 - ratio.x) * 0.5,
            vUv.y * ratio.y + (1.0 - ratio.y) * 0.5
          );
          vec4 color = texture2D(tMap, uv);
          float d = roundedBoxSDF(vUv - 0.5, vec2(0.5 - uBorderRadius), uBorderRadius);
          float alpha = 1.0 - smoothstep(-0.002, 0.002, d);
          gl_FragColor = vec4(color.rgb, alpha);
        }
      `,
      uniforms: {
        tMap: { value: texture },
        uPlaneSizes: { value: [0, 0] },
        uImageSizes: { value: [1, 1] },
        uSpeed: { value: 0 },
        uTime: { value: 100 * Math.random() },
        uBorderRadius: { value: this.borderRadius },
      },
      transparent: true,
    });

    this.plane = new Mesh(this.gl, { geometry: this.geometry, program });
    this.plane.setParent(this.scene);

    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.src = this.item.image;
    img.onload = () => {
      texture.image = img;
      program.uniforms.uImageSizes.value = [img.naturalWidth, img.naturalHeight];
    };
  }

  private createTitle() {
    this.title = new Title(this.gl, this.plane, this.item.text, this.textColor, this.font);
  }

  update(scroll: { current: number; last: number }, direction: 'right' | 'left') {
    this.plane.position.x = this.x - scroll.current - this.extra;

    const x = this.plane.position.x;
    const halfViewport = this.viewport.width / 2;
    if (this.bend === 0) {
      this.plane.position.y = 0;
      this.plane.rotation.z = 0;
    } else {
      const bendAbs = Math.abs(this.bend);
      const radius = (halfViewport * halfViewport + bendAbs * bendAbs) / (2 * bendAbs);
      const effectiveX = Math.min(Math.abs(x), halfViewport);
      const arc = radius - Math.sqrt(radius * radius - effectiveX * effectiveX);
      if (this.bend > 0) {
        this.plane.position.y = -arc;
        this.plane.rotation.z = -Math.sign(x) * Math.asin(effectiveX / radius);
      } else {
        this.plane.position.y = arc;
        this.plane.rotation.z = Math.sign(x) * Math.asin(effectiveX / radius);
      }
    }

    this.speed = scroll.current - scroll.last;
    const program = this.plane.program as Program;
    program.uniforms.uTime.value += 0.04;
    program.uniforms.uSpeed.value = this.speed;

    const planeOffset = this.plane.scale.x / 2;
    const viewportOffset = this.viewport.width / 2;
    this.isBefore = this.plane.position.x + planeOffset < -viewportOffset;
    this.isAfter = this.plane.position.x - planeOffset > viewportOffset;
    if (direction === 'right' && this.isBefore) {
      this.extra -= this.widthTotal;
      this.isBefore = this.isAfter = false;
    }
    if (direction === 'left' && this.isAfter) {
      this.extra += this.widthTotal;
      this.isBefore = this.isAfter = false;
    }
  }

  onResize({ screen, viewport }: { screen?: { width: number; height: number }; viewport?: { width: number; height: number } } = {}) {
    if (screen) this.screen = screen;
    if (viewport) this.viewport = viewport;
    this.scale = this.screen.height / 1500;
    this.plane.scale.y = (this.viewport.height * (900 * this.scale)) / this.screen.height;
    this.plane.scale.x = (this.viewport.width * (700 * this.scale)) / this.screen.width;
    const program = this.plane.program as Program;
    program.uniforms.uPlaneSizes.value = [this.plane.scale.x, this.plane.scale.y];
    this.padding = 2;
    this.width = this.plane.scale.x + this.padding;
    this.widthTotal = this.width * this.length;
    this.x = this.width * this.index;
  }
}

class GalleryApp {
  private renderer!: Renderer;
  private gl!: GL;
  private camera!: Camera;
  private scene!: Transform;
  private planeGeometry!: Plane;
  private medias: Media[] = [];
  private screen = { width: 1, height: 1 };
  private viewport = { width: 1, height: 1 };
  private raf = 0;
  private isDown = false;
  private didDrag = false;
  private start = 0;
  private startY = 0;
  private readonly scroll: { ease: number; current: number; target: number; last: number; position?: number };
  private readonly onCheckDebounce: () => void;

  constructor(
    private readonly container: HTMLElement,
    items: CircularGalleryItem[],
    private readonly onSelect: (item: CircularGalleryItem) => void,
    private readonly config: Required<Pick<CircularGalleryProps, 'bend' | 'textColor' | 'borderRadius' | 'font' | 'scrollSpeed' | 'scrollEase'>>,
  ) {
    this.scroll = { ease: config.scrollEase, current: 0, target: 0, last: 0 };
    this.onCheckDebounce = debounce(this.onCheck.bind(this), 200);
    this.createRenderer();
    this.createCamera();
    this.createScene();
    this.onResize();
    this.createGeometry();
    this.createMedias(items);
    this.update();
    this.addEventListeners();
  }

  private createRenderer() {
    this.renderer = new Renderer({
      alpha: true,
      antialias: true,
      dpr: Math.min(window.devicePixelRatio || 1, 2),
    });
    this.gl = this.renderer.gl;
    this.gl.clearColor(0, 0, 0, 0);
    this.container.appendChild(this.renderer.gl.canvas as HTMLCanvasElement);
  }

  private createCamera() {
    this.camera = new Camera(this.gl);
    this.camera.fov = 45;
    this.camera.position.z = 20;
  }

  private createScene() {
    this.scene = new Transform();
  }

  private createGeometry() {
    this.planeGeometry = new Plane(this.gl, {
      heightSegments: 50,
      widthSegments: 100,
    });
  }

  private createMedias(items: CircularGalleryItem[]) {
    const galleryItems = items.map((item, sourceIndex) => ({
      ...item,
      text: item.title,
      sourceIndex,
    }));
    const doubledItems = galleryItems.concat(galleryItems);
    this.medias = doubledItems.map(
      (item, index) =>
        new Media(
          this.planeGeometry,
          this.gl,
          item,
          index,
          doubledItems.length,
          this.scene,
          this.screen,
          this.viewport,
          this.config.bend,
          this.config.textColor,
          this.config.borderRadius,
          this.config.font,
        ),
    );
  }

  private onTouchDown(e: MouseEvent | TouchEvent) {
    this.isDown = true;
    this.didDrag = false;
    this.scroll.position = this.scroll.current;
    this.start = 'touches' in e ? e.touches[0]?.clientX ?? 0 : e.clientX;
    this.startY = 'touches' in e ? e.touches[0]?.clientY ?? 0 : e.clientY;
  }

  private onTouchMove(e: MouseEvent | TouchEvent) {
    if (!this.isDown) return;
    const x = 'touches' in e ? e.touches[0]?.clientX ?? this.start : e.clientX;
    const y = 'touches' in e ? e.touches[0]?.clientY ?? this.startY : e.clientY;
    const distance = (this.start - x) * (this.config.scrollSpeed * 0.025);
    this.scroll.target = (this.scroll.position ?? 0) + distance;
    if (Math.abs(this.start - x) > 4 || Math.abs(this.startY - y) > 4) this.didDrag = true;
  }

  private onTouchUp() {
    this.isDown = false;
    this.onCheck();
  }

  private onWheel(e: WheelEvent) {
    if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return;
    e.preventDefault();
    this.scroll.target += (e.deltaY > 0 ? this.config.scrollSpeed : -this.config.scrollSpeed) * 0.2;
    this.onCheckDebounce();
  }

  private onClick(e: MouseEvent) {
    if (this.didDrag) return;
    const media = this.mediaAtPointer(e.clientX);
    if (media) this.onSelect(media.item);
  }

  private onCheck() {
    if (!this.medias[0]) return;
    const width = this.medias[0].width;
    const itemIndex = Math.round(Math.abs(this.scroll.target) / width);
    const item = width * itemIndex;
    this.scroll.target = this.scroll.target < 0 ? -item : item;
  }

  private onResize() {
    this.screen = {
      width: Math.max(1, this.container.clientWidth),
      height: Math.max(1, this.container.clientHeight),
    };
    this.renderer.setSize(this.screen.width, this.screen.height);
    this.camera.perspective({ aspect: this.screen.width / this.screen.height });
    const fov = (this.camera.fov * Math.PI) / 180;
    const height = 2 * Math.tan(fov / 2) * this.camera.position.z;
    const width = height * this.camera.aspect;
    this.viewport = { width, height };
    this.medias.forEach((media) => media.onResize({ screen: this.screen, viewport: this.viewport }));
  }

  private mediaAtPointer(clientX: number) {
    const rect = this.container.getBoundingClientRect();
    const viewportX = ((clientX - rect.left) / Math.max(1, rect.width) - 0.5) * this.viewport.width;
    return this.medias
      .filter((media) => {
        const halfPlane = media.plane.scale.x / 2;
        const isVisible = media.plane.position.x + halfPlane >= -this.viewport.width / 2 && media.plane.position.x - halfPlane <= this.viewport.width / 2;
        const hitX = viewportX >= media.plane.position.x - halfPlane && viewportX <= media.plane.position.x + halfPlane;
        return isVisible && hitX;
      })
      .reduce<Media | null>((closest, media) => {
        if (!closest) return media;
        const distance = Math.abs(media.plane.position.x - viewportX);
        const closestDistance = Math.abs(closest.plane.position.x - viewportX);
        return distance < closestDistance ? media : closest;
      }, null);
  }

  private update() {
    this.scroll.current = lerp(this.scroll.current, this.scroll.target, this.scroll.ease);
    const direction = this.scroll.current > this.scroll.last ? 'right' : 'left';
    this.medias.forEach((media) => media.update(this.scroll, direction));
    this.renderer.render({ scene: this.scene, camera: this.camera });
    this.scroll.last = this.scroll.current;
    this.raf = window.requestAnimationFrame(this.update.bind(this));
  }

  private addEventListeners() {
    window.addEventListener('resize', this.boundOnResize);
    this.container.addEventListener('wheel', this.boundOnWheel, { passive: false });
    this.container.addEventListener('mousedown', this.boundOnTouchDown);
    this.container.addEventListener('mousemove', this.boundOnTouchMove);
    this.container.addEventListener('mouseup', this.boundOnTouchUp);
    this.container.addEventListener('mouseleave', this.boundOnTouchUp);
    this.container.addEventListener('touchstart', this.boundOnTouchDown, { passive: true });
    this.container.addEventListener('touchmove', this.boundOnTouchMove, { passive: true });
    this.container.addEventListener('touchend', this.boundOnTouchUp);
    this.container.addEventListener('click', this.boundOnClick);
  }

  destroy() {
    window.cancelAnimationFrame(this.raf);
    window.removeEventListener('resize', this.boundOnResize);
    this.container.removeEventListener('wheel', this.boundOnWheel);
    this.container.removeEventListener('mousedown', this.boundOnTouchDown);
    this.container.removeEventListener('mousemove', this.boundOnTouchMove);
    this.container.removeEventListener('mouseup', this.boundOnTouchUp);
    this.container.removeEventListener('mouseleave', this.boundOnTouchUp);
    this.container.removeEventListener('touchstart', this.boundOnTouchDown);
    this.container.removeEventListener('touchmove', this.boundOnTouchMove);
    this.container.removeEventListener('touchend', this.boundOnTouchUp);
    this.container.removeEventListener('click', this.boundOnClick);
    this.scene.children.forEach((child) => child.setParent(null));
    if (this.renderer?.gl?.canvas.parentNode) this.renderer.gl.canvas.parentNode.removeChild(this.renderer.gl.canvas as HTMLCanvasElement);
    this.renderer?.gl.getExtension('WEBGL_lose_context')?.loseContext();
  }

  private readonly boundOnResize = this.onResize.bind(this);
  private readonly boundOnWheel = this.onWheel.bind(this);
  private readonly boundOnTouchDown = this.onTouchDown.bind(this);
  private readonly boundOnTouchMove = this.onTouchMove.bind(this);
  private readonly boundOnTouchUp = this.onTouchUp.bind(this);
  private readonly boundOnClick = this.onClick.bind(this);
}

export default function CircularGallery({
  items,
  onSelect,
  className,
  bend = -1,
  textColor = '#ffffff',
  borderRadius = 0.11,
  font = 'bold 30px Figtree',
  scrollSpeed = 2.3,
  scrollEase = 0.04,
}: CircularGalleryProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || items.length === 0) return undefined;
    const app = new GalleryApp(container, items, onSelect, {
      bend,
      textColor,
      borderRadius,
      font,
      scrollSpeed,
      scrollEase,
    });
    return () => app.destroy();
  }, [bend, borderRadius, font, items, onSelect, scrollEase, scrollSpeed, textColor]);

  return (
    <div className={className ? `circular-gallery ${className}` : 'circular-gallery'}>
      <div ref={containerRef} className="circular-gallery__viewport" />
    </div>
  );
}
