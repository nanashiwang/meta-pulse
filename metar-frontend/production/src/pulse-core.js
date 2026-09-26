/* On-demand Pulse presentation. Outcomes only come from the authenticated API. */
'use strict';
(() => {
  const clamp = x => Math.max(0, Math.min(1, x));
  const smooth = x => { x = clamp(x); return x * x * (3 - 2 * x); };
  const out = x => 1 - Math.pow(1 - clamp(x), 3);
  const esc = value => String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[char]));
  const icon = type => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${type === 'community_exp' ? '<path d="m12 2 3 6 7 1-5 5 1 7-6-3-6 3 1-7-5-5 7-1Z"/>' : '<path d="m12 2 8 7-8 13L4 9 8 2Zm0 0L8 9l4 13 4-13-4-7ZM4 9h16"/>'}</svg>`;

  // Hold before fracture until a server result exists. No animation chooses a prize.
  class Timeline {
    constructor(now, reduced = false) {
      this.start = now; this.duration = 3900; this.hold = 0.44;
      this.readyAt = null; this.skipped = reduced;
    }
    receive(now) { if (this.readyAt === null) this.readyAt = now; }
    skip() { this.skipped = true; }
    sample(now) {
      const ready = this.readyAt !== null;
      if (this.skipped) return { progress: ready ? 1 : this.hold, phase: ready ? 'result' : 'waiting', animate: false };
      const elapsed = Math.max(0, now - this.start);
      const wait = ready ? Math.max(0, this.readyAt - this.start - this.duration * this.hold) : 0;
      const progress = ready ? clamp((elapsed - wait) / this.duration) : Math.min(this.hold, elapsed / this.duration);
      const phase = progress >= 1 ? 'result' : !ready && progress >= this.hold ? 'waiting' : progress < .29 ? 'gather' : progress < .54 ? 'charge' : 'reveal';
      return { progress, phase, animate: phase !== 'result' && phase !== 'waiting' };
    }
  }

  function markup({ t }) {
    return `<section class="pulse-core" data-pulse-core data-stage="idle" aria-label="${esc(t('脉冲核心'))}">
      <div class="pc-header"><span class="pc-brand">METAR / PULSE</span><label class="pc-palette-label">${esc(t('光效'))}<select data-core-palette aria-label="${esc(t('选择脉冲光效'))}"><option value="jade">${esc(t('翡翠极光'))}</option><option value="nebula">${esc(t('紫蓝星云'))}</option></select></label></div>
      <div class="pc-heading"><p class="pc-eyebrow">EVERY SPARK RETURNS</p><h2>${esc(t('开启你的脉冲'))}</h2><p class="pc-subtitle">${esc(t('让每一次积累，在这一刻绽放。'))}</p></div>
      <div class="pc-stage"><canvas aria-hidden="true"></canvas><div class="pc-horizon" aria-hidden="true"></div><span class="pc-annotation" aria-hidden="true">PULSE CORE</span>
        <div class="pc-reward" aria-hidden="true"><div class="pc-reward-inner"><div class="pc-reward-label">${esc(t('这一份回馈，属于你'))}</div><div class="pc-emblem"></div><div class="pc-amount"></div><div class="pc-unit"></div><div class="pc-divider"></div><div class="pc-card-foot"></div></div></div>
      </div>
      <div class="pc-bottom"><p class="pc-state" role="status" aria-live="polite" aria-atomic="true"></p><p class="pc-tickets"></p><button type="button" class="pc-primary" data-action="pulse-draw" disabled>${esc(t('开启一次脉冲 · 1 券'))}</button><div class="pc-skip-wrap"><button type="button" class="pc-skip" data-core-skip hidden>${esc(t('跳过动画'))}</button></div></div>
      <div class="pc-footer"><span>${esc(t('每次开启消耗 1 张脉冲券'))}</span><button type="button" class="pc-refresh" data-action="pulse-refresh">${esc(t('刷新奖励状态'))}</button></div>
    </section>`;
  }

  function scene(root) {
    const canvas = root.querySelector('canvas');
    let ctx = null;
    try { ctx = canvas.getContext('2d'); } catch (_) { /* Keep the result card usable without Canvas. */ }
    const stage = root.querySelector('.pc-stage'), reward = root.querySelector('.pc-reward');
    const annotation = root.querySelector('.pc-annotation');
    let w = 0, h = 0, colors = {}, progress = 0;
    const frac = x => x - Math.floor(x);
    const stars = Array.from({length:76}, (_, i) => ({a:frac(Math.sin(i*91.13+4)*874.73)*Math.PI*2,r:.25+frac(Math.sin(i*6.7+2)*476.1)*.75,size:.6+frac(Math.sin(i*71.1)*162.9)*1.6,delay:frac(Math.sin(i*4.92)*381.1)}));
    function size() {
      w = stage.clientWidth; h = stage.clientHeight;
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(w*dpr); canvas.height = Math.round(h*dpr);
      if (ctx) ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      const css = getComputedStyle(root);
      for (const key of ['energy','secondary','core','gold','bg']) colors[key] = css.getPropertyValue('--pc-'+key).trim();
      if (ctx) draw(progress);
    }
  function ellipse(cx,cy,rx,ry,angle,alpha,color=colors.energy,width=1) {
    ctx.save();ctx.globalAlpha=alpha;ctx.strokeStyle=color;ctx.lineWidth=width;ctx.beginPath();ctx.ellipse(cx,cy,rx,ry,angle,0,Math.PI*2);ctx.stroke();ctx.restore();
  }
  function dot(x,y,r,alpha,color=colors.energy) {ctx.globalAlpha=alpha;ctx.fillStyle=color;ctx.beginPath();ctx.arc(x,y,Math.max(.1,r),0,Math.PI*2);ctx.fill();ctx.globalAlpha=1;}
  function aura(x,y,r,alpha) {
    ctx.save();ctx.globalAlpha=alpha;const g=ctx.createRadialGradient(x,y,0,x,y,r);g.addColorStop(0,colors.energy);g.addColorStop(1,'transparent');ctx.fillStyle=g;ctx.fillRect(x-r,y-r,2*r,2*r);ctx.restore();
  }
  function crystal(x,y,s,p) {
    const gather=smooth(p/.44), split=out((p-.46)/.22), disappear=1-smooth((p-.57)/.16);
    if(disappear<=0) return;
    const points=[[0,-1.08],[.68,-.43],[.62,.45],[0,1.1],[-.62,.45],[-.68,-.43],[0,-.35],[0,.44]];
    const faces=[[0,1,6],[0,6,5],[1,2,7,6],[5,6,7,4],[2,3,7],[4,7,3],[6,7,3],[6,0,5]];
    const scale=1-gather*.13;
    ctx.save();ctx.translate(x,y);ctx.rotate(-.10+gather*.10);ctx.scale(s*scale,s*scale);
    faces.forEach((face,i)=>{
      const angle=i/faces.length*Math.PI*2-.4, distance=split*2.3;
      ctx.save();ctx.translate(Math.cos(angle)*distance,Math.sin(angle)*distance*.7);ctx.rotate(split*(i%2?1:-1)*.8);
      ctx.globalAlpha=disappear;
      const g=ctx.createLinearGradient(-.7,-1,.7,1);g.addColorStop(0,i%3===0?colors.core:colors.energy);g.addColorStop(.54,i%2===0?colors.energy:colors.secondary);g.addColorStop(1,colors.bg);
      ctx.fillStyle=g;ctx.strokeStyle=colors.core;ctx.lineWidth=.009;
      ctx.beginPath();face.forEach((v,j)=>{const q=points[v];j?ctx.lineTo(q[0],q[1]):ctx.moveTo(q[0],q[1]);});ctx.closePath();ctx.fill();ctx.globalAlpha=disappear*.55;ctx.stroke();ctx.restore();
    });
    ctx.restore();
    if(p<.51) {ctx.save();ctx.strokeStyle=colors.core;ctx.lineWidth=1+gather*2;ctx.globalAlpha=.3+gather*.6;ctx.shadowColor=colors.energy;ctx.shadowBlur=12+gather*20;ctx.beginPath();ctx.moveTo(x,y-s*.84);ctx.lineTo(x+3,y-s*.1);ctx.lineTo(x-4,y+s*.2);ctx.lineTo(x,y+s*.82);ctx.stroke();ctx.restore();}
  }
  function draw(p) {
    if(!w||!h) return;
    ctx.clearRect(0,0,w,h);
    const cx=w/2,cy=h*.46,base=Math.min(w*.23,82),bound=Math.min(w*.46,260);
    const gather=smooth(p/.43),release=clamp((p-.46)/.36),isResult=p>=1;
    aura(cx,cy,base*(2.4+gather*.5),isResult?.10:.13+gather*.12);
    ellipse(cx,cy+base*1.47,base*1.55,base*.20,0,.18);
    ellipse(cx,cy+base*1.47,base*1.05,base*.12,0,.12);
    const ringAlpha=(1-smooth((p-.48)/.25))*.36;
    ellipse(cx,cy,base*1.85,base*.77,-.43+gather*.5,ringAlpha,colors.energy);
    ellipse(cx,cy,base*1.65,base*.6,.58-gather*.8,ringAlpha*.6,colors.secondary);
    const count=w < 500 ? 40 : stars.length;
    stars.slice(0,count).forEach((star,i)=>{
      const staticX=cx+Math.cos(star.a)*bound*star.r,staticY=cy+Math.sin(star.a)*h*.43*star.r;
      if(p===0||isResult){dot(staticX,staticY,star.size*.7,.12+star.delay*.25,i%4===0?colors.secondary:colors.energy);return;}
      if(p<.47) {
        const f=clamp((p/.46-star.delay*.25)/.75),r=bound*star.r*(1-f*.91),a=star.a+f*1.3;
        const x=cx+Math.cos(a)*r,y=cy+Math.sin(a)*r*.72;
        ctx.save();ctx.globalAlpha=.10+f*.5;ctx.strokeStyle=i%3===0?colors.secondary:colors.energy;ctx.lineWidth=star.size*.65;
        ctx.beginPath();ctx.moveTo(x,y);ctx.lineTo(cx+Math.cos(a-.06-f*.15)*(r+8+f*14),cy+Math.sin(a-.06-f*.15)*(r+8+f*14)*.72);ctx.stroke();ctx.restore();dot(x,y,star.size,.4+f*.5);
      } else {
        const f=out((p-.47-star.delay*.04)/.4),distance=(base*.5+bound*star.r)*f;
        const x=cx+Math.cos(star.a)*distance,y=cy+Math.sin(star.a)*distance*.78+Math.max(0,p-.72)*60;
        const opacity=(1-smooth((p-.66)/.34))*.9;
        ctx.save();ctx.globalAlpha=opacity;ctx.strokeStyle=i%3===0?colors.gold:colors.energy;ctx.lineWidth=star.size*.7;ctx.beginPath();ctx.moveTo(x,y);ctx.lineTo(x-Math.cos(star.a)*(4+(1-f)*24),y-Math.sin(star.a)*(4+(1-f)*24));ctx.stroke();ctx.restore();
      }
    });
    crystal(cx,cy,base,p);
    if(release>0&&release<1) {
      const radius=base*.6+out(release)*bound*1.4;
      ellipse(cx,cy,radius,radius*.74,0,(1-release)*.7,colors.energy,1.6);
      ellipse(cx,cy,radius*.81,radius*.6,0,(1-release)*.35,colors.secondary,1);
      ctx.save();ctx.globalAlpha=(1-release)*.6;const g=ctx.createLinearGradient(cx-bound,cy,cx+bound,cy);g.addColorStop(0,'transparent');g.addColorStop(.5,colors.core);g.addColorStop(1,'transparent');ctx.fillStyle=g;ctx.fillRect(cx-bound,cy-1,bound*2,2);ctx.restore();
    }
    const reveal=out((p-.58)/.3);
    reward.style.opacity=String(reveal);reward.style.visibility=reveal>0?'visible':'hidden';
    reward.style.transform=`translateY(${55*(1-reveal)}px) rotateY(${-22*(1-reveal)}deg) rotateX(${14*(1-reveal)}deg) scale(${.76+.24*reveal})`;
    reward.style.setProperty('--shine',`${clamp((p-.69)/.29)*620}px`);
    annotation.style.opacity=String(1-gather);
  }
    const resize = new ResizeObserver(size); resize.observe(stage);
    const theme = new MutationObserver(size); theme.observe(document.documentElement, { attributes:true, attributeFilter:['data-theme'] });
    size();
    return {
      paint(p) {
        progress = p;
        if (ctx) draw(p);
        else {
          reward.style.opacity = p >= 1 ? '1' : '0';
          reward.style.visibility = p >= 1 ? 'visible' : 'hidden';
          reward.style.transform = 'none';
        }
      },
      size,
      dispose() { resize.disconnect(); theme.disconnect(); }
    };
  }

  class View {
    constructor(root, { t }) {
      this.root = root; this.t = t; this.dead = false; this.frame = 0;
      this.button = root.querySelector('.pc-primary');
      this.skipButton = root.querySelector('[data-core-skip]');
      this.status = root.querySelector('.pc-state');
      this.reward = root.querySelector('.pc-reward');
      this.palette = root.querySelector('[data-core-palette]');
      this.refresh = root.querySelector('.pc-refresh');
      this.motion = window.matchMedia('(prefers-reduced-motion: reduce)');
      let palette = 'jade';
      try { if (window.localStorage.getItem('_metar_pulse_palette') === 'nebula') palette = 'nebula'; } catch (_) { /* Visual preference is optional. */ }
      root.dataset.palette = this.palette.value = palette;
      this.scene = scene(root);
      this.onPalette = () => {
        root.dataset.palette = this.palette.value;
        try { window.localStorage.setItem('_metar_pulse_palette', this.palette.value); } catch (_) { /* Keep this page's selection. */ }
        this.scene.size();
      };
      this.onSkip = () => { if (this.timeline) { this.restoreFocus = document.activeElement === this.skipButton; this.timeline.skip(); this.tick(); } };
      this.onVisibility = () => { if (document.hidden) this.onSkip(); };
      this.onMotion = () => { if (this.motion.matches) this.onSkip(); };
      this.palette.addEventListener('change', this.onPalette);
      this.skipButton.addEventListener('click', this.onSkip);
      document.addEventListener('visibilitychange', this.onVisibility);
      this.motion.addEventListener('change', this.onMotion);
    }
    update(options) {
      if (this.dead) return;
      this.options = options;
      this.root.querySelector('.pc-tickets').textContent = this.t('可用脉冲券：{count}', {count: options.tickets});
      this.button.dataset.action = options.pending ? 'pulse-resume' : 'pulse-draw';
      this.button.disabled = options.busy || (!options.pending && !options.canDraw);
      this.button.textContent = this.t(options.busy ? '正在处理…' : options.pending ? '继续处理原请求' : '开启一次脉冲 · 1 券');
      this.refresh.disabled = options.busy;
      this.palette.disabled = options.busy;
      if (this.restoreFocus && !options.busy) {
        (this.button.disabled ? this.refresh : this.button).focus({ preventScroll:true });
        this.restoreFocus = false;
      }
      if (!this.timeline) {
        if (options.result) this.showResult(options.result);
        else this.status.textContent = options.message || options.reason || this.t('一枚核心，等待被点亮');
      }
    }
    begin() {
      if (this.dead) return;
      this.resolve?.(false); this.resolve = null;
      this.timeline = new Timeline(performance.now(), this.motion.matches || document.hidden);
      this.button.disabled = this.refresh.disabled = this.palette.disabled = true;
      this.button.textContent = this.t('正在开启');
      this.reward.setAttribute('aria-hidden', 'true');
      this.tick();
    }
    reveal(result) {
      if (this.dead) return Promise.resolve(false);
      this.result = result;
      this.fillResult(result);
      if (!this.timeline) { this.showResult(result); return Promise.resolve(true); }
      const done = new Promise(resolve => { this.resolve = resolve; });
      this.timeline.receive(performance.now());
      this.tick();
      return done;
    }
    fillResult(result) {
      this.root.querySelector('.pc-amount').textContent = result.amount;
      this.root.querySelector('.pc-unit').textContent = result.unit;
      this.root.querySelector('.pc-emblem').innerHTML = icon(result.type);
      this.root.querySelector('.pc-card-foot').textContent = result.status;
    }
    showResult(result) {
      this.fillResult(result); this.root.dataset.stage = 'result'; this.scene.paint(1);
      this.reward.setAttribute('aria-hidden', 'false');
      this.status.textContent = this.t('本次回馈：{amount} {unit} · {status}', result);
    }
    tick() {
      cancelAnimationFrame(this.frame); this.frame = 0;
      if (this.dead || !this.timeline) return;
      const state = this.timeline.sample(performance.now());
      this.root.dataset.stage = state.phase;
      this.scene.paint(state.progress);
      this.skipButton.hidden = this.timeline.skipped || state.phase === 'result';
      this.skipButton.disabled = this.skipButton.hidden;
      if (state.phase === 'result') {
        this.timeline = null; this.showResult(this.result);
        this.resolve?.(true); this.resolve = null;
        return;
      }
      const messages = {gather:'微光汇聚，点亮你的积累', charge:'脉冲共振，即将绽放', waiting:'正在确认本次结果，请稍候…', reveal:'这一刻，惊喜正在成形'};
      const message = this.t(messages[state.phase]);
      if (this.status.textContent !== message) this.status.textContent = message;
      if (state.animate) this.frame = requestAnimationFrame(() => this.tick());
    }
    fail(message) {
      if (this.dead) return;
      cancelAnimationFrame(this.frame); this.frame = 0; this.timeline = null;
      this.resolve?.(false); this.resolve = null;
      this.root.dataset.stage = 'idle'; this.scene.paint(0);
      this.reward.setAttribute('aria-hidden', 'true'); this.skipButton.hidden = true;
      this.status.textContent = message;
    }
    dispose() {
      this.dead = true; cancelAnimationFrame(this.frame); this.frame = 0;
      this.resolve?.(false); this.resolve = null;
      this.scene.dispose();
      this.palette.removeEventListener('change', this.onPalette);
      this.skipButton.removeEventListener('click', this.onSkip);
      document.removeEventListener('visibilitychange', this.onVisibility);
      this.motion.removeEventListener('change', this.onMotion);
    }
  }
  window.MetarPulseCore = Object.freeze({ Timeline, markup, View });
})();
