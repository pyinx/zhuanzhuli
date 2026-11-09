/* 交互设计演示；无系统接口、网络请求、持久化或真实音频。 */
(() => {
  'use strict';
  const scenes = {
    forest: {name:'森林', subtitle:'林间留白，慢慢进入', file:'forest.svg'},
    beach: {name:'沙滩', subtitle:'海风与潮汐之间', file:'beach.svg'},
    snow: {name:'雪地', subtitle:'让周围安静下来', file:'snow.svg'},
    meadow: {name:'草原', subtitle:'给眼前的事情一点空间', file:'meadow.svg'}
  };
  const apps = [
    {id:'safari',name:'Safari',mark:'↗',hint:'资料与浏览'}, {id:'codex',name:'Codex',mark:'C',hint:'AI 工作'},
    {id:'notes',name:'备忘录',mark:'≡',hint:'编写与整理'}, {id:'wechat',name:'微信',mark:'··',hint:'沟通'},
    {id:'finder',name:'访达',mark:'⌑',hint:'文件'}, {id:'terminal',name:'终端',mark:'›_',hint:'命令与任务'}
  ];
  const sampleGoal = '完成产品方案的第一版';
  const exampleHistory = () => [
    {id:'sample-1',goal:'梳理产品方案的核心流程',date:'今天 · 示例',time:'14:10',minutes:32,status:'已结束'},
    {id:'sample-2',goal:'整理用户访谈问题',date:'今天 · 示例',time:'10:30',minutes:15,status:'已结束'},
    {id:'sample-3',goal:'阅读原生交互设计资料',date:'昨天 · 示例',time:'16:20',minutes:18,status:'异常中断'}
  ];
  const state = {
    phase:'ready',goal:'',minutes:30,scene:'forest',visuals:true,allowed:new Set(['safari','codex','notes']),sound:false,volume:24,guide:false,
    protection:'ready',mainOpen:true,controlOpen:false,settings:null,historyOpen:false,dialog:null,
    customOpen:false,customText:'',error:'',mediaError:false,session:null,history:exampleHistory(),deleted:[],
    composing:false,entryTimer:null,cueUntil:0,multi:false,
    reduceMotion:matchMedia('(prefers-reduced-motion: reduce)').matches
  };
  const $ = id => document.getElementById(id);
  const esc = v => String(v).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const active = () => state.phase==='active' || state.phase==='paused';
  const elapsed = () => state.session ? state.session.accrued + (state.phase==='active' ? Math.max(0,performance.now()-state.session.runningSince) : 0) : 0;
  const overtime = () => active() && elapsed() >= state.session.minutes*60000;
  const formatTime = ms => { const s=Math.max(0,Math.floor(ms/1000));return `${String(Math.floor(s/60)).padStart(2,'0')}:${String(s%60).padStart(2,'0')}`; };
  const clockText = () => !active() ? '准备专注' : overtime() ? `+${formatTime(elapsed()-state.session.minutes*60000)}` : formatTime(state.session.minutes*60000-elapsed());
  const allowedNow = () => active() ? state.session.allowed : state.allowed;
  const announce = text => { $('announcer').textContent=text; };
  const icon = app => `<span class="app-icon ${app.id}" aria-hidden="true">${app.mark}</span>`;
  const titlebar = (title,close,extra='') => `<div class="window-titlebar"><div class="traffic">${close==='noop'?'<i aria-hidden="true"></i>':`<button data-action="${close}" aria-label="关闭${title}（窗口示意）"></button>`}<i aria-hidden="true" title="最小化：需原生应用验证"></i><i aria-hidden="true" title="缩放与全屏：需原生应用验证"></i></div><span class="window-title">${title}</span><div class="titlebar-actions">${extra}</div></div>`;
  const deleteSymbol = '<svg class="delete-symbol" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.4" aria-hidden="true" focusable="false"><path d="M3 5.5h14M7.5 5.5v-2h5v2M5 5.5l.8 12h8.4l.8-12M8 8v6.5M12 8v6.5" stroke-linecap="round" stroke-linejoin="round"/></svg>';
  const statusCopy = () => state.phase==='paused' ? '保护已暂停 · 演示' : state.protection==='ready' ? '保护就绪 · 演示' : state.protection==='unverified' ? '保护尚未验证 · 演示' : state.protection==='none' ? '需要完成配置 · 演示' : '仅部分保护可用 · 演示';
  const statusDot = () => state.phase==='paused' ? 'neutral' : state.protection==='ready' ? '' : 'warn';

  function mainView() {
    const chosen=apps.filter(a=>state.allowed.has(a.id));
    const scene=scenes[state.scene];
    const lastGoal=state.history[0]?.goal;
    return titlebar('专注','close-main','<button data-action="history" aria-label="查看专注记录">记录</button><button data-action="settings" data-panel="general" aria-label="打开专注设置">设置…</button>')+
      `<div class="main-content"><h1>开始专注</h1><p class="intro-copy">写下目标，选择一段时间。</p>
      <div class="goal-block"><label class="field-label" for="goal">这段时间，想推进什么？</label><input id="goal" type="text" value="${esc(state.goal)}" placeholder="例如：完成产品方案的第一版" autocomplete="off" aria-describedby="goal-assist" ${state.error==='goal'?'aria-invalid="true"':''}>
      <div class="field-assist" id="goal-assist">${state.error==='goal'?'<span class="field-error">先写下一件想推进的事。</span>':lastGoal?`<button class="link" data-action="reuse-goal">继续上次：${esc(lastGoal)}</button>`:'一句话就好。'}</div></div>
      <div class="duration-row"><span class="field-label" style="margin:0">专注时长</span><div class="segment" aria-label="选择专注时长">${[15,30,60].map(n=>`<button data-action="duration" data-value="${n}" aria-pressed="${state.minutes===n&&!state.customOpen}">${n} 分钟</button>`).join('')}<button data-action="custom" aria-pressed="${state.customOpen||![15,30,60].includes(state.minutes)}">${![15,30,60].includes(state.minutes)?state.minutes+' 分钟':'自定义'}</button></div></div>
      ${state.customOpen?`<div class="custom-duration"><label for="custom-minutes" class="caption">分钟</label><input id="custom-minutes" inputmode="numeric" type="text" value="${esc(state.customText)}" aria-describedby="custom-help" ${state.error==='duration'?'aria-invalid="true"':''}><button data-action="apply-duration">确定</button><span id="custom-help" class="${state.error==='duration'?'field-error':'caption'}">${state.error==='duration'?'请输入 1–180 的整数':'1–180 分钟 · 候选范围'}</span></div>`:''}
      <div class="app-row"><span class="field-label" style="margin:0">允许的应用</span><div class="app-stack">${chosen.map(icon).join('')}<span class="app-names">${chosen.length?chosen.length+' 个应用':'尚未选择'}</span><button class="link" id="app-picker" data-action="settings" data-panel="apps" ${state.error==='apps'?'aria-describedby="app-help"':''}>更改…</button></div></div>${state.error==='apps'?'<p class="field-error" id="app-help">选择至少一个本次需要的应用。</p>':''}
      <div class="scene-preview ${state.mediaError?'failed':''}">${state.mediaError?'<div class="media-fallback"><span>场景暂时不可用</span><button data-action="retry-media">重试</button></div>':`<img src="assets/${scene.file}" alt="${scene.name}场景插画示意"><div class="scene-caption"><strong>${state.visuals?scene.name:'场景已关闭'}</strong><span>${state.visuals?scene.subtitle:'沿用原桌面，声音仍可单独开启'}</span></div><button class="scene-change" data-action="settings" data-panel="scenes">场景设置…</button>`}</div>
      <div class="sound-row"><label for="sound"><input id="sound" type="checkbox" ${state.sound?'checked':''}>环境声音</label><div class="sound-volume"><span class="caption">音量</span><input id="volume" type="range" min="0" max="100" value="${state.volume}" aria-label="环境声音音量" ${!state.sound?'disabled':''}></div></div>
      <label class="guide-row" for="guide"><input id="guide" type="checkbox" ${state.guide?'checked':''}>进入时，听一段简短引导</label></div>
      <footer class="window-footer"><button class="quiet protection-inline" data-action="settings" data-panel="protection"><span class="status-dot ${statusDot()}"></span>${statusCopy()}</button><button class="primary" id="start-button" data-action="start">开始专注<span class="shortcut" aria-hidden="true">↵</span></button></footer>`;
  }
  function workspaceView() {
    return `${state.multi?'<div class="multi-label"><span>显示器 1 · 资料与编写</span><span>显示器 2 · AI 工具</span></div><div class="screen-divider"></div>':''}
      <div class="work-window work-document">${titlebar('备忘录 · 窗口示意','noop')}<div class="work-content"><span class="caption">产品思考 / 草稿</span><h2>${esc(state.session?.goal||sampleGoal)}</h2><h3>先让一个目标变得清楚。</h3><p>在这一段时间里，把注意力留给正在推进的事情。相关的资料和工具，都可以在手边。</p><div class="document-rule"></div><p>一、明确本次要解决的问题<br>二、整理最重要的使用流程<br>三、写下下一步需要验证的假设</p><span class="work-label">原生应用内容示意 · 不读取实际文档</span></div></div>
      <div class="work-window work-ai">${titlebar('Codex · 窗口示意','noop')}<div class="work-content"><div class="ai-query">帮我整理这份方案的关键问题。</div><div class="ai-message">可以从目标、使用流程和边界三方面整理。<br><br>先明确用户这次要完成什么，再逐步检查进入、工作与退出的体验。</div><span class="work-label">AI 对话示意 · 无外部连接</span></div></div>`;
  }
  function controlView() {
    const isPaused=state.phase==='paused';
    return `<div class="control-content"><div class="control-heading"><span id="control-title">${isPaused?'已暂停':overtime()?'仍在专注':'专注中'}</span></div><p class="control-goal" title="${esc(state.session?.goal)}">${esc(state.session?.goal)}</p><div class="timer" data-timer>${clockText()}</div><div class="timer-caption" data-timer-caption>${isPaused?'暂停时间不计入本次投入':overtime()?'已超过预设时长，继续也很好':'剩余时间 · 无需一直看着它'}</div><hr class="control-divider"><button class="quiet control-protection" data-action="settings" data-panel="protection"><span class="status-dot ${statusDot()}"></span>${statusCopy()}</button><div class="sound-row"><label><input id="control-sound" type="checkbox" ${state.sound?'checked':''}>环境声音</label><input id="control-volume" type="range" min="0" max="100" value="${state.volume}" aria-label="环境声音音量" ${!state.sound?'disabled':''}></div><div class="control-actions"><button class="primary" data-action="${isPaused?'resume':'pause'}">${isPaused?'继续专注':'暂停'}</button><button data-action="exit">退出专注</button></div><div class="control-links"><button class="link" data-action="settings" data-panel="apps">本次应用…</button><button class="link" data-action="history">专注记录</button></div></div>`;
  }
  function settingsView() {
    const tabs={general:'通用',apps:'应用',scenes:'场景',sound:'声音',protection:'保护状态'};
    let content='';
    if(state.settings==='apps') content=`<h2>${active()?'本次允许的应用':'工作时需要哪些应用？'}</h2><p>${active()?'调整只影响本次专注，不改写下次默认组合。':'选择相关的工具，下次会沿用这组选择。'}</p><div class="settings-group">${apps.map(a=>`<div class="setting-row"><label for="app-${a.id}">${icon(a)}<span>${a.name}<small>${a.hint}</small></span></label><input id="app-${a.id}" data-app="${a.id}" type="checkbox" ${allowedNow().has(a.id)?'checked':''} aria-label="允许使用${a.name}"></div>`).join('')}</div>${state.error==='session-apps'?'<p class="field-error">本次专注至少保留一个应用。</p>':''}<p class="inline-note">只按应用整理。不检查网页、聊天或文档内容。</p>`;
    if(state.settings==='scenes') content=`<h2>去一个安静的地方</h2><p>场景插画与入场分镜示意，非最终视频。</p><div class="settings-group"><div class="setting-row"><label for="visuals">使用场景画面</label><input id="visuals" type="checkbox" ${state.visuals?'checked':''}></div></div><div class="scene-grid">${Object.entries(scenes).map(([id,s])=>`<button class="scene-choice" data-action="scene" data-value="${id}" aria-pressed="${state.scene===id}"><img src="assets/${s.file}" alt="${s.name}方向示意"><span>${s.name}${state.scene===id?'　✓':''}</span></button>`).join('')}</div><p class="inline-note">${state.visuals?'进入时可跳过短片。工作中使用安静的场景背景。':'场景已关闭，直接进入工作并沿用原桌面。声音和人声仍由各自开关控制。'}</p>`;
    if(state.settings==='sound') content=`<h2>让声音刚刚好</h2><p>声音控制为交互示意，本原型不会播放音频。</p><div class="settings-group"><div class="setting-row"><label for="settings-sound">环境声音</label><input id="settings-sound" type="checkbox" ${state.sound?'checked':''}></div><div class="setting-row"><label for="settings-volume">音量</label><input id="settings-volume" type="range" min="0" max="100" value="${state.volume}" ${!state.sound?'disabled':''}></div><div class="setting-row"><label for="settings-guide">进入时的人声引导</label><input id="settings-guide" type="checkbox" ${state.guide?'checked':''}></div></div><p class="inline-note">人声默认关闭。工作期间不插入语音；到点只提醒一次。</p>`;
    if(state.settings==='general') content=`<h2>通用</h2><p>沿用熟悉的 Mac 操作方式。</p><div class="settings-group"><div class="setting-row"><span>外观<small>支持浅色、深色与系统辅助偏好</small></span><span class="caption">跟随系统</span></div><div class="setting-row"><span>到点以后<small>保留环境，自动继续计时</small></span><span class="caption">继续专注</span></div><div class="setting-row"><span>记录<small>自动保存，主动查看</small></span><span class="caption">仅本机</span></div></div><p class="inline-note">本轮是设计原型。浅深色与辅助偏好的模拟切换位于顶部评审工具中。</p>`;
    if(state.settings==='protection') content=`<h2>保护状态</h2><p>以下为交互演示，不代表当前电脑已生效。</p><div class="settings-group"><div class="setting-row"><span>应用整理<small>收起无关应用，保留后台任务</small></span><span class="setting-state ${state.protection==='none'?'warn':''}">${state.protection==='none'?'未配置':'就绪（示意）'}</span></div><div class="setting-row"><span>通知保护<small>目标包括弹窗、提示音及系统通知</small></span><span class="setting-state ${state.protection!=='ready'?'warn':''}">${state.protection==='ready'?'就绪（示意）':state.protection==='unverified'?'未验证':state.protection==='none'?'未配置':'暂未生效'}</span></div><div class="setting-row"><span>环境恢复<small>暂停或退出时恢复本次改动</small></span><span class="caption">需实机验证</span></div></div><p class="inline-note ${state.protection!=='ready'?'warning':''}">${state.protection==='ready'?'原型中的“就绪”仅是可评审状态。全面屏蔽能力仍需独立技术验证。':'通知保护尚未完整建立。完成配置后检查，或明确选择以部分保护继续。'}</p><div class="settings-end"><button data-action="check-protection" class="primary">演示配置与检查</button></div>`;
    return titlebar('设置','close-settings')+`<div class="settings-body"><nav class="settings-nav" aria-label="设置分类">${Object.entries(tabs).map(([k,v])=>`<button data-action="settings" data-panel="${k}" ${state.settings===k?'aria-current="page"':''}>${v}</button>`).join('')}</nav><div class="settings-content">${content}</div></div>`;
  }
  function historyView() {
    let lastDate='';
    const rows=state.history.map(r=>{const date=r.date!==lastDate?`<li class="history-date" role="presentation">${r.date}</li>`:'';lastDate=r.date;return date+`<li class="history-row"><div><div class="history-goal" title="${esc(r.goal)}">${esc(r.goal)}</div><div class="history-meta">${esc(r.time)} · 演示记录</div></div><span class="history-duration">${r.minutes} 分钟</span><span class="history-state">${r.status}</span><button data-action="delete-history" data-id="${r.id}" aria-label="删除记录：${esc(r.goal)}">${deleteSymbol}</button></li>`;}).join('');
    const empty=`<div class="empty-state"><span class="empty-mark" aria-hidden="true">◷</span><p>还没有专注记录</p><button data-action="history-to-start">${active()?'返回本次专注':'开始一次专注'}</button></div>`;
    return titlebar('专注记录','close-history')+`<div class="history-content"><h2>留下一段投入</h2><p class="history-description">自动记录时间，不评判成果。这里只展示本页的演示记录。</p>${state.history.length?`<ul class="history-list">${rows}</ul>`:empty}${state.deleted.length?'<div class="undo-row"><span>记录已删除。</span><button class="link" data-action="undo-delete">撤销</button></div>':''}</div>`;
  }
  function dialogView() {
    if(state.dialog==='outside') return `<div class="dialog-content"><div class="dialog-icon" aria-hidden="true">◉</div><h2 id="dialog-title">微信不在本次工作空间中</h2><p>如果它与眼前的事情有关，可以只在这次专注中允许使用。</p><div class="dialog-goal">当前目标：${esc(state.session.goal)}</div></div><div class="dialog-actions"><button class="primary" data-action="return-work">返回当前工作</button><button data-action="allow-outside">本次允许微信</button><button class="quiet" data-action="exit">退出专注</button></div><p class="dialog-note">范围外访问流程示意 · 不监控真实应用</p>`;
    if(state.dialog==='protection') return `<div class="dialog-content"><div class="dialog-icon" aria-hidden="true">◌</div><h2 id="dialog-title">${state.protection==='unverified'?'通知保护尚未验证':'通知保护还没有准备好'}</h2><p>当前无法确认通知已被屏蔽。可以先完成配置，也可以明确选择只使用现有的部分保护。</p></div><div class="dialog-actions"><button class="primary" data-action="configure-protection">完成配置</button><button data-action="continue-partial">以部分保护继续</button><button class="quiet" data-action="cancel-dialog">取消</button></div><p class="dialog-note">保护状态为交互演示，不控制当前电脑</p>`;
    if(state.dialog==='checking') return `<div class="dialog-content"><h2 id="dialog-title">保护检查过程 · 演示</h2><p>真实应用会等待系统检查结果。本页不执行检查；可手动切到结果状态，查看界面如何变化。</p></div><div class="dialog-actions"><button class="primary" data-action="demo-check-complete">显示就绪状态（演示）</button><button data-action="cancel-dialog">取消</button></div>`;
    if(state.dialog==='recovery') return `<div class="dialog-content"><h2 id="dialog-title">上次专注意外中断了</h2><p>保留了中断前的目标与可信计时。未运行的时间不补算，也不自动恢复保护。</p><div class="dialog-goal">${esc(state.session.goal)}<br><span class="muted">已记录 12 分钟 · 当前已暂停</span></div></div><div class="dialog-actions"><button class="primary" data-action="recover">查看暂停的专注</button><button data-action="discard-recovery">结束这次专注</button></div><p class="dialog-note">异常恢复方案示意，持久化与恢复范围需实机验证</p>`;
    return '';
  }
  function entryView() {
    const copy=`<h1 class="entry-title" id="entry-title">${state.visuals?'走进'+scenes[state.scene].name:'准备进入专注'}</h1><p class="entry-subtitle">这段时间，只推进眼前这一件事。</p>${state.guide?'<p class="entry-guide">把其他事情先放一边。<br>让呼吸慢一点，准备好时，从这里开始。<br><small style="color:inherit">引导文案示意 · 不播放人声</small></p>':''}`;
    return `<div class="entry-person" aria-hidden="true"></div><div class="entry-copy">${copy}</div><button class="entry-skip" data-action="skip-entry">跳过，开始工作</button><p class="entry-note">6 秒入场分镜／引导示意 · 非最终媒体 · 可随时跳过</p>`;
  }

  function render() {
    const focused=document.activeElement;
    const focusId=focused?.id;
    const focusKey=focused?.dataset.action?{...focused.dataset,window:focused.closest('.mac-window')?.id}:null;
    const selection=focused&&typeof focused.selectionStart==='number'?[focused.selectionStart,focused.selectionEnd]:null;
    if(state.dialog)state.controlOpen=false;
    const desktop=$('desktop');desktop.dataset.scene=state.scene;desktop.dataset.visuals=String(state.visuals);desktop.dataset.working=String(active());desktop.dataset.paused=String(state.phase==='paused');desktop.dataset.phase=state.phase;
    $('main-window').hidden=!state.mainOpen||state.phase==='entering';$('main-window').innerHTML=mainView();
    $('workspace').hidden=!active();$('workspace').innerHTML=active()?workspaceView():'';
    $('control-window').hidden=!state.controlOpen||!active();$('control-window').innerHTML=active()?controlView():'';
    $('settings-window').hidden=!state.settings;$('settings-window').innerHTML=state.settings?settingsView():'';
    $('history-window').hidden=!state.historyOpen;$('history-window').innerHTML=state.historyOpen?historyView():'';
    $('dialog-window').hidden=!state.dialog;$('dialog-window').innerHTML=state.dialog?dialogView():'';
    $('dialog-scrim').hidden=!state.dialog;
    $('status-item').setAttribute('aria-expanded',String(active()?state.controlOpen:state.mainOpen&&state.phase!=='entering'));
    $('status-item').setAttribute('aria-controls',active()?'control-window':'main-window');
    $('status-item').setAttribute('aria-label',active()?'专注控制':'开始专注窗口');
    if(active())$('status-item').setAttribute('aria-haspopup','dialog');else $('status-item').removeAttribute('aria-haspopup');
    $('entry-view').hidden=state.phase!=='entering';$('entry-view').innerHTML=state.phase==='entering'?entryView():'';
    $('desktop-caption').textContent=state.phase==='paused'?'暂停中 · 恢复原工作环境的示意':'场景插画与系统窗口示意 · 非最终媒体资源';
    $('menubar-time').textContent=active()?clockText():'准备专注';
    const blocked=!!state.dialog||state.phase==='entering';
    ['main-window','settings-window','history-window','control-window','workspace'].forEach(id=>{$(id).inert=blocked;});
    document.querySelector('.system-bar').inert=blocked;
    document.querySelector('.desktop-dock').inert=blocked;
    if(state.dialog) queueMicrotask(()=>$('dialog-window').querySelector('button')?.focus());
    else if(focusId&&$(focusId)&&!$(focusId).closest('[hidden]')) { $(focusId).focus();if(selection&&typeof $(focusId).setSelectionRange==='function') $(focusId).setSelectionRange(...selection); }
    else if(focusKey) {
      const scope=focusKey.window?$(focusKey.window):document;
      [...scope.querySelectorAll('[data-action]')].find(n=>!n.closest('[hidden]')&&['action','value','panel','id'].every(k=>n.dataset[k]===focusKey[k]))?.focus();
    }
    updateClock();
    positionControl();
  }
  function positionControl() {
    const panel=$('control-window');
    if(panel.hidden)return;
    const desktop=$('desktop').getBoundingClientRect();
    const trigger=$('status-item').getBoundingClientRect();
    const gutter=12,width=panel.getBoundingClientRect().width;
    const origin=trigger.left+trigger.width/2-desktop.left;
    const left=Math.max(gutter,Math.min(desktop.width-width-gutter,origin-width/2));
    const top=trigger.bottom-desktop.top+8;
    panel.style.left=left+'px';panel.style.right='auto';panel.style.top=top+'px';
    panel.style.setProperty('--control-origin',Math.max(16,Math.min(width-16,origin-left))+'px');
    panel.style.setProperty('--control-max-height',Math.max(0,desktop.height-top-gutter)+'px');
  }
  function focusCurrent() { queueMicrotask(()=>{if(state.dialog)$('dialog-window').querySelector('button')?.focus();else if(state.controlOpen)$('control-window').querySelector('.control-actions button')?.focus();else if(state.settings)$('settings-window').querySelector('button[aria-current="page"]')?.focus();else if(state.historyOpen)$('history-window').querySelector('button')?.focus();else if(state.mainOpen)$('goal')?.focus();else $('status-item').focus();}); }
  function updateClock() {
    if(!active())return;
    const over=overtime();
    $('menubar-time').textContent=(performance.now()<state.cueUntil?'预设时间到 · ':'')+clockText();
    document.querySelectorAll('[data-timer]').forEach(n=>n.textContent=clockText());
    document.querySelectorAll('[data-timer-caption]').forEach(n=>n.textContent=state.phase==='paused'?'暂停时间不计入本次投入':over?'已超过预设时长，继续也很好':'剩余时间 · 无需一直看着它');
    if(over&&!state.session.notified) { state.session.notified=true;state.cueUntil=performance.now()+6000;announce('预设时间已到。可以继续专注，也可以随时暂停或退出。');if(state.controlOpen)render(); }
    $('desktop').dataset.timerStage=over?'overtime':'countdown';
  }
  function begin(partial=false) {
    if(state.phase!=='ready'){state.controlOpen=true;render();return;}
    if(!state.goal.trim()){state.error='goal';render();$('goal').focus();return;}
    if(!state.allowed.size){state.error='apps';render();$('app-picker').focus();announce('选择至少一个本次需要的应用。');return;}
    if(state.customOpen){if(!applyCustom())return;}
    if(!partial&&state.protection!=='ready'){state.dialog='protection';render();return;}
    state.error='';state.cueUntil=0;state.dialog=null;state.settings=null;state.historyOpen=false;state.mainOpen=false;state.phase='entering';
    state.session={goal:state.goal.trim(),minutes:state.minutes,allowed:new Set(state.allowed),accrued:0,runningSince:null,notified:false};
    render();announce('进入专注。入场分镜可以跳过。');
    if(state.mediaError||((state.reduceMotion||!state.visuals)&&!state.guide)){finishEntry();return;}
    $('entry-view').querySelector('[data-action="skip-entry"]').focus();
    state.entryTimer=setTimeout(finishEntry,6000);
  }
  function finishEntry() {
    if(state.phase!=='entering')return;
    clearTimeout(state.entryTimer);state.entryTimer=null;state.phase='active';state.session.runningSince=performance.now();state.controlOpen=false;render();announce('专注已开始。计时入口在菜单栏。');$('status-item').focus();
  }
  function pause() {
    if(state.phase!=='active')return;
    state.session.accrued=elapsed();state.session.runningSince=null;state.phase='paused';render();focusCurrent();announce('已暂停，计时停止，保护暂时解除。');
  }
  function resume() {
    if(state.phase!=='paused')return;
    if(state.protection!=='ready'&&state.protection!=='partial-accepted'){state.dialog='protection';render();return;}
    state.session.runningSince=performance.now();state.phase='active';state.controlOpen=true;render();focusCurrent();announce('已恢复专注。');
  }
  function exit() {
    if(state.session&&active())state.history.unshift({id:'demo-'+Date.now(),goal:state.session.goal,date:'本次演示',time:'本页计时',minutes:Math.round(elapsed()/6000)/10,status:'已结束'});
    clearTimeout(state.entryTimer);state.entryTimer=null;state.cueUntil=0;state.phase='ready';state.session=null;state.dialog=null;state.settings=null;state.historyOpen=false;state.controlOpen=false;state.mainOpen=true;render();focusCurrent();
  }
  function applyCustom() {
    const raw=state.customText.trim();
    if(!/^\d+$/.test(raw)||Number(raw)<1||Number(raw)>180){state.error='duration';render();$('custom-minutes').focus();return false;}
    state.minutes=Number(raw);state.customOpen=false;state.error='';render();return true;
  }
  function seedSession(minutes=8,paused=false) {
    state.goal=state.goal.trim()||sampleGoal;state.session={goal:state.goal,minutes:30,allowed:new Set(state.allowed.size?state.allowed:['safari','codex','notes']),accrued:minutes*60000,runningSince:paused?null:performance.now(),notified:minutes>=30};state.phase=paused?'paused':'active';state.mainOpen=false;state.controlOpen=true;
  }
  function scenario(name) {
    clearTimeout(state.entryTimer);state.entryTimer=null;state.cueUntil=0;state.phase='ready';state.session=null;state.mainOpen=true;state.controlOpen=false;state.settings=null;state.historyOpen=false;state.dialog=null;state.error='';state.mediaError=false;state.multi=false;state.protection='ready';state.customOpen=false;state.deleted=[];state.allowed=new Set(['safari','codex','notes']);state.history=exampleHistory();state.minutes=30;
    if(name==='start')state.goal='';
    if(name==='first'){state.allowed=new Set();state.protection='none';state.goal='';state.history=[];state.settings='protection';}
    if(['focus','overtime','paused','outside','multi'].includes(name)){state.protection='ready';seedSession(name==='overtime'?34:8,name==='paused');}
    if(name==='outside')state.dialog='outside';
    if(name==='overtime')state.cueUntil=performance.now()+6000;
    if(name==='multi'){state.multi=true;state.controlOpen=false;}
    if(name==='limited'||name==='unverified'){state.protection=name==='limited'?'limited':'unverified';state.goal=sampleGoal;state.dialog='protection';}
    if(name==='history'||name==='empty'){state.mainOpen=false;state.historyOpen=true;state.history=name==='empty'?[]:exampleHistory();}
    if(name==='recovery'){seedSession(12,true);state.controlOpen=false;state.dialog='recovery';}
    if(name==='media-error'){state.mediaError=true;state.goal=sampleGoal;}
    if(name==='long')state.goal='整理多轮用户访谈与现有产品资料，在不改变既定产品范围的前提下，完成原生应用工作空间、通知保护、跨屏使用与异常恢复流程的第一版可评审方案';
    render();focusCurrent();
  }
  const actions={
    'close-main':()=>{state.mainOpen=false;render();$('status-item').focus();},
    'open-main':()=>{if(active()){state.controlOpen=true;}else state.mainOpen=true;render();focusCurrent();},
    'toggle-control':()=>{if(active())state.controlOpen=!state.controlOpen;else state.mainOpen=!state.mainOpen;render();focusCurrent();},
    'reuse-goal':()=>{state.goal=state.history[0]?.goal||'';state.error='';render();$('goal').focus();},
    'duration':el=>{state.minutes=Number(el.dataset.value);state.customOpen=false;state.error='';render();},
    'custom':()=>{state.customOpen=!state.customOpen;state.customText=String(state.minutes);render();if(state.customOpen)$('custom-minutes').focus();},
    'apply-duration':applyCustom,'start':()=>begin(),
    'settings':el=>{state.controlOpen=false;state.historyOpen=false;state.settings=el.dataset.panel||'general';render();focusCurrent();},
    'close-settings':()=>{state.settings=null;render();focusCurrent();},
    'scene':el=>{if(scenes[el.dataset.value])state.scene=el.dataset.value;state.mediaError=false;render();},
    'retry-media':()=>{state.mediaError=false;render();},
    'skip-entry':finishEntry,'pause':pause,'resume':resume,'exit':exit,
    'history':()=>{state.controlOpen=false;state.historyOpen=true;state.settings=null;render();queueMicrotask(()=>$('history-window').querySelector('button')?.focus());},
    'close-history':()=>{state.historyOpen=false;render();focusCurrent();},
    'history-to-start':()=>{state.historyOpen=false;if(active())state.controlOpen=true;else state.mainOpen=true;render();focusCurrent();},
    'delete-history':el=>{const i=state.history.findIndex(r=>r.id===el.dataset.id);if(i>=0){state.deleted.push({row:state.history[i],beforeId:state.history[i+1]?.id});state.history.splice(i,1);render();$('history-window').querySelector('[data-action="undo-delete"]')?.focus();announce('记录已删除，可以撤销。');}},
    'undo-delete':()=>{const deleted=state.deleted.pop();if(deleted){const next=state.history.findIndex(r=>r.id===deleted.beforeId);state.history.splice(next<0?state.history.length:next,0,deleted.row);render();[...$('history-window').querySelectorAll('[data-action="delete-history"]')].find(n=>n.dataset.id===deleted.row.id)?.focus();announce('记录已恢复。');}},
    'return-work':()=>{state.dialog=null;render();$('status-item').focus();},
    'allow-outside':()=>{state.session.allowed.add('wechat');state.dialog=null;render();$('status-item').focus();announce('微信已加入本次允许范围，下次默认组合不变。');},
    'cancel-dialog':()=>{state.dialog=null;if(active())state.controlOpen=true;render();focusCurrent();},
    'configure-protection':()=>{state.dialog=null;state.controlOpen=false;state.settings='protection';render();focusCurrent();},
    'check-protection':()=>{state.dialog='checking';render();},
    'demo-check-complete':()=>{state.protection='ready';state.dialog=null;render();focusCurrent();announce('已显示演示就绪状态。未执行系统检查。');},
    'continue-partial':()=>{state.protection='partial-accepted';state.dialog=null;if(state.phase==='paused')resume();else begin(true);},
    'recover':()=>{state.dialog=null;state.controlOpen=true;render();focusCurrent();},
    'discard-recovery':exit
  };
  $('desktop').addEventListener('click',e=>{
    const el=e.target.closest('[data-action]');
    if(!el||el.disabled||!actions[el.dataset.action])return;
    if(state.controlOpen&&!el.closest('#control-window,#status-item,[data-action="open-main"]'))state.controlOpen=false;
    actions[el.dataset.action](el);e.stopPropagation();
  });
  document.addEventListener('click',e=>{
    if(!state.controlOpen||e.target.closest('#control-window,#status-item,.review-bar'))return;
    const restoreFocus=$('control-window').contains(document.activeElement);
    state.controlOpen=false;$('control-window').hidden=true;$('status-item').setAttribute('aria-expanded','false');
    if(restoreFocus)$('status-item').focus();
  });
  window.addEventListener('resize',positionControl);
  $('desktop').addEventListener('input',e=>{
    const t=e.target;
    if(t.id==='goal'){state.goal=t.value;if(state.error==='goal'){state.error='';t.removeAttribute('aria-invalid');$('goal-assist').textContent='一句话就好。';}}
    if(t.id==='custom-minutes')state.customText=t.value;
    if(['volume','control-volume','settings-volume'].includes(t.id)){state.volume=Number(t.value);document.querySelectorAll('#volume,#control-volume,#settings-volume').forEach(n=>n.value=state.volume);}
  });
  $('desktop').addEventListener('change',e=>{
    const t=e.target;
    if(t.dataset.app){const selected=allowedNow();if(!t.checked&&active()&&selected.size===1){state.error='session-apps';render();announce('本次专注至少保留一个应用。');return;}state.error='';if(t.checked)selected.add(t.dataset.app);else selected.delete(t.dataset.app);render();}
    if(['sound','control-sound','settings-sound'].includes(t.id)){state.sound=t.checked;render();}
    if(['guide','settings-guide'].includes(t.id)){state.guide=t.checked;render();}
    if(t.id==='visuals'){state.visuals=t.checked;render();}
  });
  $('desktop').addEventListener('compositionstart',()=>{state.composing=true;});
  $('desktop').addEventListener('compositionend',()=>{state.composing=false;});
  document.addEventListener('keydown',e=>{
    if(e.isComposing||state.composing||e.keyCode===229)return;
    if(e.key==='Escape'&&e.target.closest('#desktop')){
      e.preventDefault();if(state.dialog==='checking')state.dialog=null;
      else if(state.dialog==='recovery'){state.dialog=null;state.controlOpen=true;}
      else if(state.dialog)state.dialog=null;
      else if(state.controlOpen){state.controlOpen=false;render();$('status-item').focus();return;}
      else if(state.settings)state.settings=null;
      else if(state.historyOpen)state.historyOpen=false;
      else if(state.phase==='entering'){clearTimeout(state.entryTimer);state.entryTimer=null;state.phase='ready';state.session=null;state.mainOpen=true;}
      else state.mainOpen=false;
      render();focusCurrent();return;
    }
    if(e.key==='Enter'&&e.target.id==='goal'){e.preventDefault();begin();}
    if(e.key==='Enter'&&e.target.id==='custom-minutes'){e.preventDefault();applyCustom();}
    if((state.dialog||state.controlOpen||state.phase==='entering')&&e.key==='Tab'&&e.target.closest('#desktop')){
      const scope=state.dialog?$('dialog-window'):state.phase==='entering'?$('entry-view'):$('control-window');
      const buttons=[...scope.querySelectorAll('button:not([disabled]),input:not([disabled])')];const first=buttons[0],last=buttons[buttons.length-1];
      if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus();}
    }
  });
  $('scenario').addEventListener('change',e=>scenario(e.target.value));
  const darkAppearance=matchMedia('(prefers-color-scheme:dark)');
  function updateAppearanceLabel() {
    const dark=document.documentElement.dataset.theme==='dark'||(!document.documentElement.dataset.theme&&darkAppearance.matches);
    $('appearance').textContent=dark?'浅色':'深色';
    $('appearance').setAttribute('aria-label',dark?'预览浅色外观':'预览深色外观');
  }
  $('appearance').addEventListener('click',()=>{
    const dark=document.documentElement.dataset.theme==='dark'||(!document.documentElement.dataset.theme&&darkAppearance.matches);
    document.documentElement.dataset.theme=dark?'light':'dark';updateAppearanceLabel();
  });
  darkAppearance.addEventListener('change',updateAppearanceLabel);updateAppearanceLabel();
  function bindPreference(id,query,key,onValue,offValue,apply=()=>{}) {
    const media=matchMedia(query),input=$(id);let overridden=false;
    const update=value=>{input.checked=value;document.documentElement.dataset[key]=value?onValue:offValue;apply(value);};
    update(media.matches);
    media.addEventListener('change',()=>{if(!overridden)update(media.matches);});
    input.addEventListener('change',()=>{overridden=true;update(input.checked);});
  }
  bindPreference('reduce-motion','(prefers-reduced-motion:reduce)','motion','reduced','normal',value=>{
    state.reduceMotion=value;if(value&&state.phase==='entering'&&!state.guide)finishEntry();
  });
  bindPreference('reduce-transparency','(prefers-reduced-transparency:reduce)','transparency','reduced','normal');
  bindPreference('high-contrast','(prefers-contrast:more)','contrast','high','normal');
  const initial=new URLSearchParams(location.search).get('state');
  if(initial&&[...$('scenario').options].some(o=>o.value===initial)){$('scenario').value=initial;scenario(initial);}else{render();$('goal').focus();}
  setInterval(updateClock,250);
})();
