/* On-demand Pulse presentation. Outcomes only come from the authenticated API. */
'use strict';
(() => {
  const clamp = x => Math.max(0, Math.min(1, x));
  const smooth = x => { x = clamp(x); return x * x * (3 - 2 * x); };
  const out = x => 1 - Math.pow(1 - clamp(x), 3);
  const esc = value => String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[char]));
  const icon = type => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${type === 'community_exp' ? '<path d="m12 2 3 6 7 1-5 5 1 7-6-3-6 3 1-7-5-5 7-1Z"/>' : '<path d="m12 2 8 7-8 13L4 9 8 2Zm0 0L8 9l4 13 4-13-4-7ZM4 9h16"/>'}</svg>`;
  const tiers = ['white', 'blue', 'purple', 'gold', 'red'];
  const tierLabels = {white:'白光', blue:'蓝光', purple:'紫光', gold:'金光', red:'红光'};
  const tier = value => tiers.includes(value) ? value : 'white';
  const strongest = rewards => tiers[Math.max(0, ...rewards.map(reward => tiers.indexOf(tier(reward.tier))))];
  // All five cards finish inside the existing timeline, including a partial batch.
  function cardFrame(progress, index = 0) {
    const p = progress - Math.min(4, index) * .027;
    const enter = out((p - .49) / .15);
    return {
      enter,
      light: smooth((p - .49) / .10) * (1 - smooth((p - .68) / .17)),
      bloom: smooth((p - .50) / .12) * (1 - smooth((p - .69) / .20)),
      silhouette: smooth((p - .61) / .045) * (1 - smooth((p - .72) / .09)),
      content: smooth((p - .70) / .14),
      lift: p > .53 && p < .87 ? Math.sin(Math.PI * (p - .53) / .34) : 0,
      shine: clamp((p - .75) / .18)
    };
  }
  function cards(result, t) {
    return result.rewards.map((reward, index) => `<div class="pc-card" data-tier="${tier(reward.tier)}"><span class="pc-card-bloom" aria-hidden="true"></span><div class="pc-reward-inner"><div class="pc-card-content">
      <div class="pc-reward-label">${esc(t(tierLabels[tier(reward.tier)]))}<span>${result.total > 1 ? String(index + 1).padStart(2,'0') : 'PULSE'}</span></div>
      <div class="pc-emblem">${icon(reward.type)}</div><div class="pc-value"><span class="pc-amount">${esc(reward.amount)}</span><span class="pc-unit">${esc(reward.unit)}</span></div>
      <div class="pc-divider"></div><div class="pc-card-foot" data-pending="${Boolean(reward.pending)}">${esc(reward.status)}</div>
      </div></div><span class="pc-card-light" aria-hidden="true"><span class="pc-light-sigil">${icon(reward.type)}</span></span><span class="pc-card-frame" aria-hidden="true"></span></div>`).join('');
  }

  // Hold before fracture until a server result exists. No animation chooses a prize.
  class Timeline {
    constructor(now, reduced = false) {
      this.start = now; this.duration = 3900; this.hold = 0.44;
      this.revealStart = .46; this.shortenedAt = null;
      this.readyAt = null; this.reduced = reduced;
    }
    receive(now) { if (this.readyAt === null) this.readyAt = now; }
    skipCharge(now) {
      // Never rewind a reveal or restart its light burst when the preference changes.
      if (this.shortenedAt === null && this.sample(now).progress < this.revealStart) this.shortenedAt = now;
    }
    finish() { this.reduced = true; }
    sample(now) {
      const ready = this.readyAt !== null;
      if (this.reduced) return { progress: ready ? 1 : this.hold, phase: ready ? 'result' : 'waiting', animate: false };
      const elapsed = Math.max(0, now - this.start);
      const wait = ready ? Math.max(0, this.readyAt - this.start - this.duration * this.hold) : 0;
      const shortened = this.shortenedAt !== null;
      const progress = shortened
        ? ready ? clamp(this.revealStart + Math.max(0, now - Math.max(this.shortenedAt, this.readyAt)) / this.duration) : this.hold
        : ready ? clamp((elapsed - wait) / this.duration) : Math.min(this.hold, elapsed / this.duration);
      const phase = progress >= 1 ? 'result' : !ready && progress >= this.hold ? 'waiting' : shortened ? 'reveal' : progress < .29 ? 'gather' : progress < .54 ? 'charge' : 'reveal';
      return { progress, phase, animate: phase !== 'result' };
    }
  }

  function markup({ t }) {
    return `<section class="pulse-core" data-pulse-core data-stage="idle" aria-label="${esc(t('脉冲核心'))}">
      <div class="pc-header"><span class="pc-brand">METAR / PULSE</span><label class="pc-palette-label">${esc(t('光效'))}<select data-core-palette aria-label="${esc(t('选择脉冲光效'))}"><option value="jade">${esc(t('翡翠极光'))}</option><option value="nebula">${esc(t('紫蓝星云'))}</option></select></label></div>
      <div class="pc-heading"><div class="pc-title"><h1>${esc(t('开启你的脉冲'))}</h1><details class="pc-help"><summary aria-label="${esc(t('Pulse 介绍'))}"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M9 8a3 3 0 0 1 6 0c0 2-3 2-3 4m0 4v.5"/></svg></summary><div class="pc-help-panel"><p>${esc(t('经核验的付费调用积累脉冲券，额度奖励发往元衡 API，经验奖励计入社区等级。'))}</p></div></details></div><p class="pc-subtitle">${esc(t('让每一次积累，在这一刻绽放。'))}</p></div>
      <div class="pc-stage"><canvas aria-hidden="true"></canvas><div class="pc-horizon" aria-hidden="true"></div><span class="pc-annotation" aria-hidden="true">PULSE CORE</span>
        <div class="pc-reward" aria-hidden="true"></div>
      </div>
      <div class="pc-bottom"><p class="pc-state" role="status" aria-live="polite" aria-atomic="true"></p><div class="pc-controls"><p class="pc-tickets"></p><div class="pc-actions"><button type="button" class="pc-primary" data-action="pulse-draw" disabled>${esc(t('开启一次脉冲 · 1 券'))}</button><button type="button" class="pc-primary pc-five" data-action="pulse-draw-five" disabled>${esc(t('五连抽 · 5 券'))}</button></div></div></div>
      <div class="pc-footer"><label class="pc-skip" title="${esc(t('保留闪光与翻牌'))}"><input type="checkbox" data-core-skip>${esc(t('跳过蓄能'))}</label><button type="button" class="pc-refresh" data-action="pulse-refresh">${esc(t('刷新奖励状态'))}</button></div>
    </section>`;
  }

  function scene(root) {
    const canvas = root.querySelector('canvas');
    let ctx = null;
    try { ctx = canvas.getContext('2d'); } catch (_) { /* Keep the result card usable without Canvas. */ }
    const stage = root.querySelector('.pc-stage'), reward = root.querySelector('.pc-reward');
    const annotation = root.querySelector('.pc-annotation');
    let w = 0, h = 0, colors = {}, progress = 0, waitingTime = null, cardLayout = [];
    const frac = x => x - Math.floor(x);
    const stars = Array.from({length:76}, (_, i) => ({a:frac(Math.sin(i*91.13+4)*874.73)*Math.PI*2,r:.25+frac(Math.sin(i*6.7+2)*476.1)*.75,size:.6+frac(Math.sin(i*71.1)*162.9)*1.6,delay:frac(Math.sin(i*4.92)*381.1)}));
    function size() {
      w = stage.clientWidth; h = stage.clientHeight;
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(w*dpr); canvas.height = Math.round(h*dpr);
      if (ctx) ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      const css = getComputedStyle(root);
      for (const key of ['energy','secondary','core','gold','bg']) colors[key] = css.getPropertyValue('--pc-'+key).trim();
      // Measure once on result/resize/theme changes, never inside the animation loop.
      cardLayout = Array.from(reward.querySelectorAll('.pc-card'), card => {
        const style = getComputedStyle(card);
        return {card, x:reward.offsetLeft+card.offsetLeft+card.offsetWidth/2,
          y:reward.offsetTop+card.offsetTop+card.offsetHeight/2, width:card.offsetWidth, height:card.offsetHeight,
          power:tiers.indexOf(tier(card.dataset.tier)), flare:style.getPropertyValue('--pc-flare').trim()};
      });
      if (ctx) draw(progress, waitingTime);
    }
  function ellipse(cx,cy,rx,ry,angle,alpha,color=colors.energy,width=1) {
    ctx.save();ctx.globalAlpha=alpha;ctx.strokeStyle=color;ctx.lineWidth=width;ctx.beginPath();ctx.ellipse(cx,cy,rx,ry,angle,0,Math.PI*2);ctx.stroke();ctx.restore();
  }
  function dot(x,y,r,alpha,color=colors.energy) {ctx.globalAlpha=alpha;ctx.fillStyle=color;ctx.beginPath();ctx.arc(x,y,Math.max(.1,r),0,Math.PI*2);ctx.fill();ctx.globalAlpha=1;}
  function aura(x,y,r,alpha,color=colors.energy) {
    ctx.save();ctx.globalAlpha=alpha;const g=ctx.createRadialGradient(x,y,0,x,y,r);g.addColorStop(0,color);g.addColorStop(1,'transparent');ctx.fillStyle=g;ctx.fillRect(x-r,y-r,2*r,2*r);ctx.restore();
  }
  function cardRadiance(layout, frame, p) {
    const {x,y,width,height,power,flare} = layout;
    const glow = frame.bloom * (.20 + power * .17);
    if (glow <= 0) return;
    aura(x,y,width*(1.4+power*.18),glow,flare);
    aura(x,y,height*.74,glow*.6,'#fff3c7');
    if (power >= 2) {
      // Broad, feathered shafts radiate from the card, rather than a central wheel.
      ctx.save();ctx.translate(x,y);ctx.rotate(-.35+frame.lift*.10);
      for(let i=0;i<10;i++) {
        ctx.rotate(Math.PI*2/10);
        const length=width*(1.5+(i%3)*.38),spread=length*(.10+(i%2)*.12);
        const ray=ctx.createLinearGradient(0,0,length,0);
        ray.addColorStop(0,'#fff8df');ray.addColorStop(.18,flare);ray.addColorStop(1,'transparent');
        ctx.fillStyle=ray;ctx.globalAlpha=glow*(i%2?.16:.40);
        ctx.beginPath();ctx.moveTo(0,0);ctx.lineTo(length,-spread);ctx.quadraticCurveTo(length*1.08,0,length,spread);ctx.closePath();ctx.fill();
      }
      ctx.restore();
    }
    if (power >= 3) {
      ctx.save();ctx.globalAlpha=glow*.65;
      const streak=ctx.createLinearGradient(x-width*2,y,x+width*2,y);
      streak.addColorStop(0,'transparent');streak.addColorStop(.44,flare);streak.addColorStop(.5,'#fffbea');streak.addColorStop(.56,flare);streak.addColorStop(1,'transparent');
      ctx.fillStyle=streak;ctx.fillRect(x-width*2,y-height*.12,width*4,1.5);
      ctx.restore();
      stars.slice(0,power===4?22:14).forEach((star,i)=>{
        const travel=clamp((p-.53)/.47),px=x+Math.cos(star.a)*width*(.48+travel*star.r),py=y+Math.sin(star.a)*height*.52-travel*height*.3;
        dot(px,py,star.size*(i%3===0?1.4:.65),glow*(.3+star.r*.6),i%3===0?'#fffbea':flare);
      });
    }
  }
  function crystal(x,y,s,p) {
    const gather=smooth(p/.44), split=out((p-.46)/.22), disappear=1-smooth((p-.49)/.12);
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
  function draw(p, waiting = null) {
    if(!w||!h) return;
    ctx.clearRect(0,0,w,h);
    const breath=waiting === null ? 0 : Math.sin(waiting/650);
    const orbit=waiting === null ? 0 : waiting/1400;
    const cx=w/2,cy=h*.46+breath*3,base=Math.min(w*.18,h*.24,60)*(1+breath*.025),bound=Math.min(w*.46,300);
    const gather=smooth(p/.43),release=clamp((p-.46)/.20),isResult=p>=1;
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
        const f=clamp((p/.46-star.delay*.25)/.75),r=bound*star.r*(1-f*.91),a=star.a+f*1.3+orbit;
        const x=cx+Math.cos(a)*r,y=cy+Math.sin(a)*r*.72;
        ctx.save();ctx.globalAlpha=.10+f*.5;ctx.strokeStyle=i%3===0?colors.secondary:colors.energy;ctx.lineWidth=star.size*.65;
        ctx.beginPath();ctx.moveTo(x,y);ctx.lineTo(cx+Math.cos(a-.06-f*.15)*(r+8+f*14),cy+Math.sin(a-.06-f*.15)*(r+8+f*14)*.72);ctx.stroke();ctx.restore();dot(x,y,star.size,.4+f*.5);
      } else {
        const f=out((p-.47-star.delay*.04)/.4),distance=(base*.5+bound*star.r)*f;
        const x=cx+Math.cos(star.a)*distance,y=cy+Math.sin(star.a)*distance*.78+Math.max(0,p-.72)*60;
        const opacity=(1-smooth((p-.66)/.34))*.9;
        ctx.save();ctx.globalAlpha=opacity;ctx.strokeStyle=i%3===0?colors.secondary:colors.energy;ctx.lineWidth=star.size*.7;ctx.beginPath();ctx.moveTo(x,y);ctx.lineTo(x-Math.cos(star.a)*(4+(1-f)*24),y-Math.sin(star.a)*(4+(1-f)*24));ctx.stroke();ctx.restore();
      }
    });
    crystal(cx,cy,base,p);
    if(release>0&&release<1) {
      const radius=base*.6+out(release)*bound*1.4;
      ellipse(cx,cy,radius,radius*.74,0,(1-release)*.7,colors.energy,1.6);
      ellipse(cx,cy,radius*.81,radius*.6,0,(1-release)*.35,colors.secondary,1);
      ctx.save();ctx.globalAlpha=(1-release)*.6;const g=ctx.createLinearGradient(cx-bound,cy,cx+bound,cy);g.addColorStop(0,'transparent');g.addColorStop(.5,colors.core);g.addColorStop(1,'transparent');ctx.fillStyle=g;ctx.fillRect(cx-bound,cy-1,bound*2,2);ctx.restore();
    }
    const power = Math.max(0, tiers.indexOf(tier(root.dataset.tier)));
    stage.style.setProperty('--pc-dim', String(smooth((p-.20)/.27)*(1-smooth((p-.81)/.19))*(.30+power*.13)));
    reward.style.opacity=p>.49?'1':'0';reward.style.visibility=p>.49?'visible':'hidden';
    cardLayout.forEach((layout, i) => {
      const {card,power} = layout, frame=cardFrame(p,i);
      cardRadiance(layout,frame,p-i*.027);
      card.style.opacity=String(frame.enter);
      card.style.transform=`translateY(${24*(1-frame.enter)-frame.lift*3}px) rotateY(${-24*(1-frame.enter)}deg) scale(${.78+.22*frame.enter+frame.lift*(power>=3?.04:.015)})`;
      card.style.setProperty('--card-light',String(frame.light));
      card.style.setProperty('--card-bloom',String(frame.bloom));
      card.style.setProperty('--card-silhouette',String(frame.silhouette));
      card.style.setProperty('--card-content',String(frame.content));
      card.style.setProperty('--shine',`${frame.shine*520}px`);
    });
    annotation.style.opacity=String(1-gather);
  }
    const resize = new ResizeObserver(size); resize.observe(stage);
    const theme = new MutationObserver(size); theme.observe(document.documentElement, { attributes:true, attributeFilter:['data-theme'] });
    size();
    return {
      canAnimate: Boolean(ctx),
      paint(p, waiting = null) {
        progress = p; waitingTime = waiting;
        if (ctx) draw(p, waiting);
        else {
          reward.style.opacity = p >= 1 ? '1' : '0';
          reward.style.visibility = p >= 1 ? 'visible' : 'hidden';
          reward.style.transform = 'none';
          annotation.style.opacity = p >= 1 ? '0' : '1';
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
      this.fiveButton = root.querySelector('.pc-five');
      this.skipInput = root.querySelector('[data-core-skip]');
      this.status = root.querySelector('.pc-state');
      this.reward = root.querySelector('.pc-reward');
      this.palette = root.querySelector('[data-core-palette]');
      this.refresh = root.querySelector('.pc-refresh');
      this.help = root.querySelector('.pc-help');
      this.onHelpOutside = event => { if (!this.help.contains(event.target)) this.help.open = false; };
      this.onHelpKey = event => {
        if (event.key === 'Escape' && this.help.open) {
          this.help.open = false; this.help.querySelector('summary').focus();
        }
      };
      document.addEventListener('click', this.onHelpOutside);
      document.addEventListener('focusin', this.onHelpOutside);
      document.addEventListener('keydown', this.onHelpKey);
      this.motion = window.matchMedia('(prefers-reduced-motion: reduce)');
      let palette = 'jade';
      try { if (window.localStorage.getItem('_metar_pulse_palette') === 'nebula') palette = 'nebula'; } catch (_) { /* Visual preference is optional. */ }
      try { this.skipInput.checked = window.localStorage.getItem('_metar_pulse_skip_animation') === '1'; } catch (_) { /* Keep the checkbox usable without storage. */ }
      root.dataset.palette = this.palette.value = palette;
      this.scene = scene(root);
      this.onPalette = () => {
        root.dataset.palette = this.palette.value;
        try { window.localStorage.setItem('_metar_pulse_palette', this.palette.value); } catch (_) { /* Keep this page's selection. */ }
        this.scene.size();
      };
      this.onSkip = () => { if (this.timeline) { this.timeline.skipCharge(performance.now()); this.tick(); } };
      this.onFinish = () => { if (this.timeline) { this.timeline.finish(); this.tick(); } };
      this.onSkipPreference = () => {
        try { window.localStorage.setItem('_metar_pulse_skip_animation', this.skipInput.checked ? '1' : '0'); } catch (_) { /* Keep this page's selection. */ }
        if (this.skipInput.checked) this.onSkip();
      };
      this.onVisibility = () => { if (document.hidden) this.onFinish(); };
      this.onMotion = () => { if (this.motion.matches) this.onFinish(); };
      this.palette.addEventListener('change', this.onPalette);
      this.skipInput.addEventListener('change', this.onSkipPreference);
      document.addEventListener('visibilitychange', this.onVisibility);
      this.motion.addEventListener('change', this.onMotion);
    }
    update(options) {
      if (this.dead) return;
      this.options = options;
      this.root.querySelector('.pc-tickets').textContent = this.t('可用脉冲券：{count}', {count: options.tickets});
      this.button.dataset.action = options.pending ? 'pulse-resume' : 'pulse-draw';
      this.button.disabled = options.busy || (!options.pending && !options.canDraw);
      this.button.textContent = this.t(options.busy ? '正在处理…' : options.pending ? '继续完成本轮抽奖' : '开启一次脉冲 · 1 券');
      this.fiveButton.disabled = options.busy || options.pending || !options.canDrawFive;
      this.fiveButton.title = !options.canDrawFive && !options.pending && !options.busy ? this.t('五连抽需要至少 5 张可用脉冲券') : '';
      this.refresh.disabled = options.busy;
      this.palette.disabled = options.busy;
      if (!this.timeline) {
        if (options.result) this.showResult(options.result);
        else this.status.textContent = options.message || options.reason || this.t('一枚核心，等待被点亮');
      }
    }
    begin(total = 1) {
      if (this.dead) return;
      this.resolve?.(false); this.resolve = null;
      const now = performance.now();
      this.timeline = new Timeline(now, this.motion.matches || document.hidden || !this.scene.canAnimate);
      if (this.skipInput.checked) this.timeline.skipCharge(now);
      this.total = total; this.confirmed = 0;
      this.root.dataset.tier = ''; this.root.dataset.count = String(total);
      this.reward.innerHTML = ''; this.resultHTML = '';
      this.scene.size();
      this.button.disabled = this.fiveButton.disabled = this.refresh.disabled = this.palette.disabled = true;
      this.button.textContent = this.t('正在开启');
      this.reward.setAttribute('aria-hidden', 'true');
      this.tick();
    }
    progress(confirmed, total) { this.confirmed = confirmed; this.total = total; if (this.timeline) this.tick(); }
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
      this.root.dataset.count = String(result.total);
      this.root.dataset.tier = strongest(result.rewards);
      const html = cards(result, this.t);
      if (html !== this.resultHTML) { this.reward.innerHTML = html; this.resultHTML = html; }
      this.scene.size();
    }
    showResult(result) {
      this.fillResult(result); this.root.dataset.stage = 'result'; this.scene.paint(1);
      this.reward.setAttribute('aria-hidden', 'false');
      this.status.textContent = result.total > 1
        ? this.t('本次已揭晓 {count} / {total} 份回馈', {count:result.rewards.length,total:result.total})
        : this.t('本次回馈：{amount} {unit} · {status}', result.rewards[0]);
      if (result.rewards.some(reward => reward.pending)) this.status.textContent += ' · ' + this.t('奖励后台发放中。');
    }
    tick() {
      cancelAnimationFrame(this.frame); this.frame = 0;
      if (this.dead || !this.timeline) return;
      const state = this.timeline.sample(performance.now());
      this.root.dataset.stage = state.phase;
      this.scene.paint(state.progress, state.phase === 'waiting' && state.animate ? performance.now() - this.timeline.start : null);
      if (state.phase === 'result') {
        this.timeline = null; this.showResult(this.result);
        this.resolve?.(true); this.resolve = null;
        return;
      }
      const messages = {gather:'微光汇聚，点亮你的积累', charge:'脉冲共振，即将绽放', waiting:'正在确认本次结果，请稍候…', reveal:'这一刻，惊喜正在成形'};
      const message = state.phase === 'waiting' && this.total > 1 ? this.t('正在确认五连抽：{count} / 5', {count:this.confirmed}) : this.t(messages[state.phase]);
      if (this.status.textContent !== message) this.status.textContent = message;
      if (state.animate) this.frame = requestAnimationFrame(() => this.tick());
    }
    fail(message) {
      if (this.dead) return;
      cancelAnimationFrame(this.frame); this.frame = 0; this.timeline = null;
      this.resolve?.(false); this.resolve = null;
      this.root.dataset.stage = 'idle'; this.scene.paint(0);
      this.reward.setAttribute('aria-hidden', 'true');
      this.status.textContent = message;
    }
    dispose() {
      this.dead = true; cancelAnimationFrame(this.frame); this.frame = 0;
      this.resolve?.(false); this.resolve = null;
      this.scene.dispose();
      document.removeEventListener('click', this.onHelpOutside);
      document.removeEventListener('focusin', this.onHelpOutside);
      document.removeEventListener('keydown', this.onHelpKey);
      this.palette.removeEventListener('change', this.onPalette);
      this.skipInput.removeEventListener('change', this.onSkipPreference);
      document.removeEventListener('visibilitychange', this.onVisibility);
      this.motion.removeEventListener('change', this.onMotion);
    }
  }
  window.MetarPulseCore = Object.freeze({ Timeline, cardFrame, markup, cards, strongest, View });
})();
