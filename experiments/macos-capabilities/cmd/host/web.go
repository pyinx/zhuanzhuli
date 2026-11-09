package main

// A new, deliberately small technical form, unrelated to design/prototype.
const webPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'">
<style>:root{color-scheme:light dark;font:13px -apple-system,system-ui}body{padding:24px;margin:0}h1{font-size:22px}label{display:block;margin:12px 0}input,button{font:inherit}input{padding:5px}button{margin:4px;padding:5px 10px}#state,#message{white-space:pre-wrap}button:focus-visible,input:focus-visible{outline:3px solid Highlight;outline-offset:2px}</style>
<title>专注力技术验证 · WebView</title><h1>独立 WebView 对照</h1>
<p>通知保护未验证；这是新建技术表单。</p>
<label>合成目标 <input id="goal" maxlength="200" value="合成目标"></label>
<label>自定义分钟 <input id="minutes" type="number" min="1" max="180" step="1" value="30"></label>
<label><input id="partial" type="checkbox"> 我理解通知未验证，仅以部分保护测试</label>
<div><button id="start">开始测试</button><button data-kind="entry-complete">跳过验证入场</button><button data-kind="pause">暂停</button><button data-kind="resume">继续</button><button data-kind="end">结束测试</button></div>
<div id="choice" hidden><button data-kind="return">返回</button><button data-kind="allow">加入本次</button><button data-kind="end">退出专注</button></div>
<p id="state"></p><p id="message" role="status" aria-live="polite"></p>
<button id="native">返回 Native UI 对照</button><button id="stop">停止并清理测试程序</button>
<script>
const el=id=>document.getElementById(id);let composing=false;
el('goal').addEventListener('compositionstart',()=>composing=true);el('goal').addEventListener('compositionend',()=>composing=false);
async function call(name,...args){try{const result=await mygo.call('Probe.'+name,...args);el('message').textContent='';await refresh();return result}catch(error){el('message').textContent=String(error)}}
async function refresh(){const s=await mygo.call('Probe.State');el('state').textContent='状态 '+s.phase+' · 有效 '+Math.floor(s.elapsed/1e9)+' 秒';el('choice').hidden=!s.pendingChoice}
el('start').onclick=()=>{if(!composing)call('Start',el('goal').value,Number(el('minutes').value),el('partial').checked)};
document.querySelectorAll('[data-kind]').forEach(b=>b.onclick=()=>call('Act',b.dataset.kind));
el('native').onclick=()=>call('Native');el('stop').onclick=()=>call('Stop');
document.addEventListener('keydown',event=>{if(event.key==='Enter'&&composing)event.stopPropagation()});
refresh().catch(error=>el('message').textContent=String(error));
setInterval(()=>refresh().catch(error=>el('message').textContent=String(error)),1000);
</script></html>`
