import { onMounted, onScopeDispose, type Ref } from "vue";

const MONO = 'ui-monospace, "SF Mono", "Cascadia Mono", Menlo, Consolas, monospace';

/* Role terminals flow in from the left, deliverables flow out to the right. */
type IconKind = "code" | "check" | "spec" | "rack" | "box" | "book" | "db" | "screen";

interface TerminalDef {
  label: string;
  icon: IconKind;
}

const SOURCES: TerminalDef[] = [
  { label: "开发端", icon: "code" },
  { label: "测试端", icon: "check" },
  { label: "产品端", icon: "spec" },
  { label: "运维端", icon: "rack" },
];

const TARGETS: TerminalDef[] = [
  { label: "客户交付端", icon: "box" },
  { label: "开发文档端", icon: "book" },
  { label: "制品库端", icon: "db" },
  { label: "生产环境端", icon: "screen" },
];

const MIN_WIDTH = 760; // below this the card fills the stage: draw grid only
const ICON_SCALE = 1.5; // icons are authored in a ~15px box, drawn larger
const ICON_PAD = 14; // gap between icon edge and link start
const GRID_STEP = 34;
const MAX_PARTICLES = 10;

interface Pt {
  x: number;
  y: number;
}

interface FlowNode extends Pt {
  label: string;
  icon: IconKind;
  phase: number;
}

interface Link {
  p0: Pt;
  p1: Pt;
  c1: Pt;
  c2: Pt;
  len: number;
}

interface Particle {
  link: Link;
  inbound: boolean;
  t: number;
  speed: number;
}

interface Ripple {
  x: number;
  y: number;
  age: number;
}

interface Layout {
  w: number;
  h: number;
  nodes: FlowNode[];
  links: Link[];
}

interface Tokens {
  primary: string;
  textAssist: string;
  grid: string;
}

function bezier(l: Link, t: number): Pt {
  const mt = 1 - t;
  const a = mt * mt * mt;
  const b = 3 * mt * mt * t;
  const c = 3 * mt * t * t;
  const d = t * t * t;
  return {
    x: a * l.p0.x + b * l.c1.x + c * l.c2.x + d * l.p1.x,
    y: a * l.p0.y + b * l.c1.y + c * l.c2.y + d * l.p1.y,
  };
}

function linkLength(l: Link): number {
  let len = 0;
  let prev = l.p0;
  for (let i = 1; i <= 24; i++) {
    const p = bezier(l, i / 24);
    len += Math.hypot(p.x - prev.x, p.y - prev.y);
    prev = p;
  }
  return len;
}

/** Minimal stroke-only line icons, centered at the origin in a ~15px box. */
function drawIcon(c: CanvasRenderingContext2D, kind: IconKind): void {
  c.save();
  c.scale(ICON_SCALE, ICON_SCALE);
  c.lineWidth = 1.3;
  c.lineCap = "round";
  c.lineJoin = "round";
  c.beginPath();
  switch (kind) {
    case "code": // </>
      c.moveTo(-4.6, -4.2);
      c.lineTo(-7.6, 0);
      c.lineTo(-4.6, 4.2);
      c.moveTo(4.6, -4.2);
      c.lineTo(7.6, 0);
      c.lineTo(4.6, 4.2);
      c.moveTo(-1.4, -5.8);
      c.lineTo(1.4, 5.8);
      break;
    case "check": // circled checkmark
      c.arc(0, 0, 6.8, 0, Math.PI * 2);
      c.moveTo(-3, 0.3);
      c.lineTo(-0.7, 2.6);
      c.lineTo(3.4, -2.4);
      break;
    case "spec": // document with folded corner
      c.moveTo(-4.4, -6.3);
      c.lineTo(2, -6.3);
      c.lineTo(5.4, -2.9);
      c.lineTo(5.4, 6.3);
      c.lineTo(-4.4, 6.3);
      c.closePath();
      c.moveTo(2, -6.3);
      c.lineTo(2, -2.9);
      c.lineTo(5.4, -2.9);
      c.moveTo(-2.2, 0.2);
      c.lineTo(3.2, 0.2);
      c.moveTo(-2.2, 3);
      c.lineTo(3.2, 3);
      break;
    case "rack": // stacked server units
      c.roundRect(-5.8, -6.4, 11.6, 5.5, 1.2);
      c.roundRect(-5.8, 0.9, 11.6, 5.5, 1.2);
      break;
    case "box": // parcel with tape
      c.roundRect(-5.2, -1.6, 10.4, 7.8, 1);
      c.moveTo(-5.2, -1.6);
      c.lineTo(0, -5.8);
      c.lineTo(5.2, -1.6);
      c.moveTo(0, -5.8);
      c.lineTo(0, 6.2);
      break;
    case "book": // open book
      c.moveTo(0, -5.2);
      c.lineTo(0, 5.8);
      c.moveTo(0, -5.2);
      c.lineTo(-5.4, -3.8);
      c.lineTo(-5.4, 4.4);
      c.lineTo(0, 5.8);
      c.moveTo(0, -5.2);
      c.lineTo(5.4, -3.8);
      c.lineTo(5.4, 4.4);
      c.lineTo(0, 5.8);
      break;
    case "db": // database cylinder
      c.moveTo(5.4, -4.4);
      c.ellipse(0, -4.4, 5.4, 2.2, 0, 0, Math.PI * 2);
      c.moveTo(-5.4, -4.4);
      c.lineTo(-5.4, 4.4);
      c.moveTo(5.4, -4.4);
      c.lineTo(5.4, 4.4);
      c.moveTo(5.4, 4.4);
      c.ellipse(0, 4.4, 5.4, 2.2, 0, 0, Math.PI);
      c.moveTo(5.4, 0);
      c.ellipse(0, 0, 5.4, 2.2, 0, 0, Math.PI);
      break;
    case "screen": // monitor with stand
      c.roundRect(-6.3, -5, 12.6, 8.6, 1.4);
      c.moveTo(0, 3.6);
      c.lineTo(0, 6.2);
      c.moveTo(-3.2, 6.2);
      c.lineTo(3.2, 6.2);
      break;
  }
  c.stroke();
  if (kind === "rack") {
    c.fillStyle = c.strokeStyle;
    for (const y of [-3.65, 3.65]) {
      c.beginPath();
      c.arc(-3.2, y, 0.75, 0, Math.PI * 2);
      c.fill();
    }
  }
  c.restore();
}

/**
 * Login page network animation (build-tool homepage style): the login card is
 * the hub, source terminals flow into it and deliverables flow out. Layout,
 * theme tokens and the frame-invariant layer (grid, links, labels) are baked
 * only when the canvas size changes, so each frame is one blit plus particles,
 * ripples and pulsing icons. Honors prefers-reduced-motion with a static
 * single frame.
 */
export function useLoginFlow(
  canvasRef: Readonly<Ref<HTMLCanvasElement | null>>,
  hubRef: Readonly<Ref<HTMLElement | null>>,
): void {
  let raf = 0;
  let last = 0;
  let startedAt = 0;
  let nextSpawnAt = 0;
  let dpr = 1;
  let ctx: CanvasRenderingContext2D | null = null;
  let layout: Layout | null = null;
  let tokens: Tokens = {
    primary: "#3d6b58",
    textAssist: "#a89f8c",
    grid: "#d2c8ac",
  };
  const staticLayer = document.createElement("canvas");
  const particles: Particle[] = [];
  const ripples: Ripple[] = [];
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  function buildLayout(): Layout {
    const canvas = canvasRef.value;
    const hub = hubRef.value;
    const w = canvas?.clientWidth ?? 0;
    const h = canvas?.clientHeight ?? 0;
    if (!canvas || !hub || w < MIN_WIDTH) return { w, h, nodes: [], links: [] };

    // The hub is centered by place-items over symmetric page padding, so its
    // center is always the canvas center; offsetWidth ignores the tilt transform.
    const hubW = hub.offsetWidth;
    const hubX = w / 2 - hubW / 2;
    const hubCY = h / 2;

    const colX = Math.min(Math.max(w * 0.15, 48), 320);
    const gap = Math.min(104, Math.max(60, h * 0.15));
    const nodes: FlowNode[] = [];
    const links: Link[] = [];

    for (let i = 0; i < 4; i++) {
      const y = Math.min(Math.max(hubCY + (i - 1.5) * gap, 44), h - 64);
      const src = SOURCES[i]!;
      const dst = TARGETS[i]!;
      nodes.push({ x: colX, y, ...src, phase: i * 1.4 });
      nodes.push({ x: w - colX, y, ...dst, phase: 0.7 + i * 1.4 });

      const mk = (p0: Pt, p1: Pt): Link => {
        const dx = p1.x - p0.x;
        const l: Link = {
          p0,
          p1,
          c1: { x: p0.x + dx * 0.45, y: p0.y },
          c2: { x: p1.x - dx * 0.45, y: p1.y },
          len: 1,
        };
        l.len = linkLength(l);
        return l;
      };
      // 每侧只有一个锚点：左列汇入一点，右列自一点发出
      links.push(mk({ x: colX + ICON_PAD, y }, { x: hubX, y: hubCY }));
      links.push(mk({ x: hubX + hubW, y: hubCY }, { x: w - colX - ICON_PAD, y }));
    }
    return { w, h, nodes, links };
  }

  /** Theme tokens are read once per rebuild instead of once per frame. */
  function readTokens(): void {
    const style = getComputedStyle(canvasRef.value!);
    tokens = {
      primary: style.getPropertyValue("--u-color-primary").trim() || tokens.primary,
      textAssist: style.getPropertyValue("--u-text-color-assist").trim() || tokens.textAssist,
      grid: style.getPropertyValue("--u-border-muted-color").trim() || tokens.grid,
    };
  }

  /** Bake the frame-invariant layer (grid, links, labels) into an offscreen canvas. */
  function bakeStatic(): void {
    const canvas = canvasRef.value;
    const l = layout;
    if (!canvas || !l) return;
    staticLayer.width = canvas.width;
    staticLayer.height = canvas.height;
    const c = staticLayer.getContext("2d")!;
    c.setTransform(dpr, 0, 0, dpr, 0, 0);

    // Blueprint grid: minor every step, major every 5th
    c.lineWidth = 1;
    c.strokeStyle = tokens.grid;
    for (const [alpha, step] of [
      [0.45, GRID_STEP],
      [0.9, GRID_STEP * 5],
    ] as const) {
      c.globalAlpha = alpha;
      c.beginPath();
      for (let x = step; x < l.w; x += step) {
        c.moveTo(x + 0.5, 0);
        c.lineTo(x + 0.5, l.h);
      }
      for (let y = step; y < l.h; y += step) {
        c.moveTo(0, y + 0.5);
        c.lineTo(l.w, y + 0.5);
      }
      c.stroke();
    }

    // Links
    c.lineWidth = 1.6;
    c.globalAlpha = 0.3;
    c.strokeStyle = tokens.primary;
    c.beginPath();
    for (const link of l.links) {
      c.moveTo(link.p0.x, link.p0.y);
      c.bezierCurveTo(link.c1.x, link.c1.y, link.c2.x, link.c2.y, link.p1.x, link.p1.y);
    }
    c.stroke();

    // Terminal labels, centered under the icon
    c.globalAlpha = 1;
    c.textAlign = "center";
    c.textBaseline = "middle";
    c.fillStyle = tokens.textAssist;
    c.font = `600 12px ${MONO}`;
    for (const n of l.nodes) {
      c.fillText(n.label, n.x, n.y + 22);
    }
  }

  function draw(now: number, dt: number): void {
    const c = ctx;
    const l = layout;
    if (!c || !l?.w) return;
    const { nodes, links } = l;
    const intro = reduced ? 1 : Math.min(1, (now - startedAt) / 900);

    c.clearRect(0, 0, l.w, l.h);
    c.globalAlpha = intro;
    c.drawImage(staticLayer, 0, 0, l.w, l.h);
    if (!nodes.length) {
      c.globalAlpha = 1;
      return;
    }

    // Particles: relay inbound -> outbound so the flow visibly transits the hub
    if (!reduced) {
      for (let i = particles.length - 1; i >= 0; i--) {
        const p = particles[i]!;
        p.t += (dt * p.speed) / p.link.len;
        if (p.t < 1) continue;
        ripples.push({ x: p.link.p1.x, y: p.link.p1.y, age: 0 });
        if (p.inbound) {
          p.link = links[4 + Math.floor(Math.random() * 4)]!;
          p.inbound = false;
          p.t = 0;
          p.speed = 120 + Math.random() * 50;
        } else {
          particles.splice(i, 1);
        }
      }
      if (particles.length < MAX_PARTICLES && now >= nextSpawnAt) {
        particles.push({
          link: links[Math.floor(Math.random() * 4)]!,
          inbound: true,
          t: 0,
          speed: 120 + Math.random() * 50,
        });
        nextSpawnAt = now + 380 + Math.random() * 420;
      }
      for (const p of particles) {
        for (let k = 5; k >= 0; k--) {
          const t = p.t - k * 0.02;
          if (t <= 0) continue;
          const pos = bezier(p.link, t);
          c.globalAlpha = (0.85 - k * 0.13) * intro;
          c.fillStyle = tokens.primary;
          c.beginPath();
          c.arc(pos.x, pos.y, 2.5 - k * 0.32, 0, Math.PI * 2);
          c.fill();
        }
      }
    }

    // Arrival ripples at hub anchors / target nodes
    for (let i = ripples.length - 1; i >= 0; i--) {
      const r = ripples[i]!;
      r.age += dt * 1000;
      if (r.age >= 650) {
        ripples.splice(i, 1);
        continue;
      }
      const k = r.age / 650;
      c.globalAlpha = (1 - k) * 0.45 * intro;
      c.strokeStyle = tokens.primary;
      c.beginPath();
      c.arc(r.x, r.y, 3 + k * 16, 0, Math.PI * 2);
      c.stroke();
    }

    // Terminals: line icon with glow + pulse
    for (const n of nodes) {
      const pulse = reduced ? 1 : 1 + Math.sin((now / 1000) * 1.7 + n.phase) * 0.04;
      c.save();
      c.translate(n.x, n.y);
      c.scale(pulse, pulse);
      c.globalAlpha = intro;
      c.strokeStyle = tokens.primary;
      c.shadowColor = tokens.primary;
      c.shadowBlur = 8;
      drawIcon(c, n.icon);
      c.restore();
    }
    c.globalAlpha = 1;
  }

  /** Re-sync canvas backing size, theme tokens, layout and the baked static layer. */
  function rebuild(): boolean {
    const canvas = canvasRef.value;
    const w = canvas?.clientWidth ?? 0;
    const h = canvas?.clientHeight ?? 0;
    if (!canvas || !w || !h) return false;
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    const bw = Math.round(w * dpr);
    const bh = Math.round(h * dpr);
    if (canvas.width !== bw || canvas.height !== bh) {
      canvas.width = bw;
      canvas.height = bh;
    }
    ctx = canvas.getContext("2d");
    if (!ctx) return false;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    layout = buildLayout();
    readTokens();
    bakeStatic();
    return true;
  }

  function frame(now: number): void {
    const dt = Math.min(0.05, (now - last) / 1000);
    last = now;
    draw(now, dt);
    raf = requestAnimationFrame(frame);
  }

  function start(now: number): void {
    last = now;
    startedAt = now;
    raf = requestAnimationFrame(frame);
  }

  onMounted(() => {
    const canvas = canvasRef.value;
    if (!canvas) return;
    const ro = new ResizeObserver(() => {
      if (!rebuild()) {
        cancelAnimationFrame(raf); // canvas hidden (narrow viewport): wait for the next resize
        raf = 0;
        return;
      }
      if (reduced) draw(performance.now(), 0);
      else if (!raf && !document.hidden) start(performance.now());
    });
    ro.observe(canvas);

    const onVisibility = () => {
      if (reduced) return;
      if (document.hidden) {
        cancelAnimationFrame(raf);
        raf = 0;
      } else if (!raf && rebuild()) {
        start(performance.now());
      }
    };
    document.addEventListener("visibilitychange", onVisibility);

    if (rebuild()) {
      if (reduced) draw(performance.now(), 0);
      else start(performance.now());
    }

    onScopeDispose(() => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      document.removeEventListener("visibilitychange", onVisibility);
    });
  });
}
