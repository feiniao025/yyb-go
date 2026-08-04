package httpapi

const fallbackIndexHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>YYB Go 控制台</title>
<body style="margin:0;background:radial-gradient(1000px 600px at 8% -10%,rgba(59,130,246,.16),transparent 60%),radial-gradient(900px 560px at 108% 4%,rgba(167,139,250,.13),transparent 58%),#0b1220;color:#e6edf7;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;font-size:14px;-webkit-font-smoothing:antialiased">
<main style="max-width:960px;margin:48px auto;padding:0 24px">
<section style="background:rgba(17,24,39,.72);border:1px solid rgba(148,163,184,.16);border-radius:10px;padding:24px;box-shadow:0 12px 32px rgba(2,6,23,.6),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)">
<h1 style="margin:0 0 8px;font-size:22px">YYB Go 控制台</h1>
<p style="margin:0 0 20px;color:#94a3b8">资源模板未找到，服务仍可通过以下入口使用。</p>
<p style="display:flex;gap:10px;flex-wrap:wrap;margin:0">
<a style="padding:9px 14px;border-radius:8px;background:linear-gradient(135deg,#3b82f6,#6366f1);color:#fff;text-decoration:none;font-weight:500;box-shadow:0 4px 14px rgba(59,130,246,.35)" href="/scan">扫码添加</a>
<a style="padding:9px 14px;border-radius:8px;border:1px solid rgba(148,163,184,.32);color:#e6edf7;text-decoration:none" href="/docs/index.html">Swagger 文档</a>
<a style="padding:9px 14px;border-radius:8px;border:1px solid rgba(148,163,184,.32);color:#e6edf7;text-decoration:none" href="/openapi.json">OpenAPI JSON</a>
<button onclick="logout()" style="padding:9px 14px;border-radius:8px;border:1px solid rgba(148,163,184,.32);color:#e6edf7;background:rgba(255,255,255,.04);cursor:pointer"><span id="meName">退出登录</span></button>
</p>
</section>
<section style="background:rgba(17,24,39,.72);border:1px solid rgba(148,163,184,.16);border-radius:10px;padding:24px;margin-top:16px;box-shadow:0 12px 32px rgba(2,6,23,.6),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)">
<h2 style="margin:0 0 8px;font-size:16px">API Token</h2>
<p style="margin:0 0 12px;color:#94a3b8">调用 <code>/wxapp/*</code> 接口时需在请求头添加 <code>Authorization: Bearer &lt;token&gt;</code></p>
<textarea id="apiTokenInput" readonly spellcheck="false" style="width:100%;min-height:64px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;padding:8px;border-radius:8px;border:1px solid rgba(148,163,184,.3);box-sizing:border-box;background:#0a0f1c;color:#d7e2f0">加载中…</textarea>
<p style="display:flex;gap:10px;margin:12px 0 0">
<button onclick="loadToken()" style="padding:9px 14px;border-radius:8px;border:1px solid rgba(148,163,184,.32);background:rgba(255,255,255,.04);color:#e6edf7;cursor:pointer">获取 Token</button>
<button onclick="rotateToken()" style="padding:9px 14px;border-radius:8px;background:linear-gradient(135deg,#3b82f6,#6366f1);color:#fff;border:0;cursor:pointer;font-weight:500;box-shadow:0 4px 14px rgba(59,130,246,.35)">更换 Token</button>
</p>
</section>
<section style="background:rgba(17,24,39,.72);border:1px solid rgba(148,163,184,.16);border-radius:10px;padding:24px;margin-top:16px;box-shadow:0 12px 32px rgba(2,6,23,.6),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)">
<h2 style="margin:0 0 12px;font-size:16px">修改登录密码</h2>
<label style="display:block;margin-bottom:4px;font-size:13px;color:#94a3b8;font-weight:500">旧密码</label>
<input id="oldPassInput" type="password" autocomplete="current-password" style="width:100%;padding:9px 11px;border:1px solid rgba(148,163,184,.3);border-radius:8px;margin-bottom:12px;box-sizing:border-box;outline:none;background:rgba(255,255,255,.04);color:#e6edf7;font:inherit">
<label style="display:block;margin-bottom:4px;font-size:13px;color:#94a3b8;font-weight:500">新密码</label>
<input id="newPassInput" type="password" autocomplete="new-password" style="width:100%;padding:9px 11px;border:1px solid rgba(148,163,184,.3);border-radius:8px;margin-bottom:12px;box-sizing:border-box;outline:none;background:rgba(255,255,255,.04);color:#e6edf7;font:inherit">
<button onclick="changePassword()" style="padding:9px 14px;border-radius:8px;background:linear-gradient(135deg,#3b82f6,#6366f1);color:#fff;border:0;cursor:pointer;font-weight:500;box-shadow:0 4px 14px rgba(59,130,246,.35)">修改密码</button>
<p id="activityLine" style="margin:12px 0 0;color:#94a3b8"></p>
</section>
</main>
<script>
async function api(method,url,body){const opts={method,headers:{'Content-Type':'application/json'}};if(body!==undefined)opts.body=JSON.stringify(body);const resp=await fetch(url,opts);const text=await resp.text();let data=null;try{data=text?JSON.parse(text):null}catch(e){data=text}const isEnv=data&&typeof data==='object'&&!Array.isArray(data)&&Object.prototype.hasOwnProperty.call(data,'code')&&Object.prototype.hasOwnProperty.call(data,'msg')&&Object.prototype.hasOwnProperty.call(data,'data');if(!resp.ok||(isEnv&&data.code!==0)){throw new Error(isEnv?data.msg:'HTTP '+resp.status)}return isEnv?data.data:data}
function activity(msg){document.getElementById('activityLine').textContent=msg}
async function loadToken(){try{const d=await api('GET','/token');document.getElementById('apiTokenInput').value=d.token}catch(e){document.getElementById('apiTokenInput').value='获取失败: '+e.message}}
async function loadMe(){try{const d=await api('GET','/account/me');document.getElementById('meName').textContent=d.username}catch(e){}}
async function rotateToken(){try{const d=await api('POST','/token/rotate');document.getElementById('apiTokenInput').value=d.token;activity('API Token 已更换')}catch(e){activity('更换失败: '+e.message)}}
async function changePassword(){const oldPassword=document.getElementById('oldPassInput').value;const newPassword=document.getElementById('newPassInput').value;if(!oldPassword||!newPassword){activity('请填写旧密码和新密码');return}try{await api('POST','/account/password',{old_password:oldPassword,new_password:newPassword});activity('密码已修改，请重新登录');setTimeout(function(){location.href='/login'},1200)}catch(e){activity('修改失败: '+e.message)}}
async function logout(){try{await api('POST','/account/logout')}catch(e){}location.href='/login'}
loadToken();
loadMe();
</script>
</body></html>`

const fallbackScanHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>扫码添加账号</title>
<body style="margin:0;min-height:100vh;display:grid;place-items:center;background:radial-gradient(900px 560px at 12% -10%,rgba(59,130,246,.18),transparent 60%),radial-gradient(760px 480px at 108% 8%,rgba(167,139,250,.15),transparent 58%),#0b1220;color:#e6edf7;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;font-size:14px;-webkit-font-smoothing:antialiased">
<main style="width:min(420px,calc(100vw - 32px));background:rgba(17,24,39,.72);border:1px solid rgba(148,163,184,.16);border-radius:10px;padding:28px;text-align:center;box-shadow:0 12px 32px rgba(2,6,23,.6),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)">
<h1 style="margin:0 0 6px;font-size:20px">扫码添加账号</h1>
<p id="s" style="margin:0 0 18px;color:#94a3b8">正在生成二维码</p>
<div id="qr" style="width:240px;height:240px;margin:0 auto 18px;display:grid;place-items:center;border:1px solid rgba(148,163,184,.16);border-radius:10px;background:rgba(255,255,255,.03)">请稍候</div>
<p style="display:flex;gap:10px;justify-content:center;margin:0">
<button onclick="newQR()" style="border:0;border-radius:8px;padding:9px 14px;background:linear-gradient(135deg,#3b82f6,#6366f1);color:#fff;cursor:pointer;font-weight:500;box-shadow:0 4px 14px rgba(59,130,246,.35)">重新生成</button>
<a href="/" style="border:1px solid rgba(148,163,184,.32);border-radius:8px;padding:8px 14px;color:#e6edf7;text-decoration:none">返回首页</a>
</p>
</main>
<script>
let sid,timer;
async function api(url,options){
 const resp=await fetch(url,options);
 const text=await resp.text();
 let data=null;
 try{data=text?JSON.parse(text):null}catch(e){data=text}
 const isEnvelope=data&&typeof data==='object'&&!Array.isArray(data)&&Object.prototype.hasOwnProperty.call(data,'code')&&Object.prototype.hasOwnProperty.call(data,'msg')&&Object.prototype.hasOwnProperty.call(data,'data');
 if(!resp.ok||(isEnvelope&&data.code!==0)){throw new Error(isEnvelope?data.msg:'HTTP '+resp.status)}
 return isEnvelope?data.data:data;
}
async function newQR(){
 clearInterval(timer);
 document.getElementById('qr').textContent='请稍候';
 document.getElementById('s').textContent='正在生成二维码';
 const r=await api('/qr',{method:'POST'});
 sid=r.session_id;
 document.getElementById('qr').innerHTML='<img alt="二维码" style="width:240px;height:240px" src="'+r.image_url+'">';
 document.getElementById('s').textContent='等待扫码';
 timer=setInterval(poll,1500);
}
async function poll(){
 const r=await api('/qr/'+sid+'/poll');
 document.getElementById('s').textContent=r.status;
 if(r.status==='authorized'){
  clearInterval(timer);
  const a=await api('/qr/'+sid+'/confirm',{method:'POST'});
  document.getElementById('s').textContent='添加成功: '+(a.nickname||a.openid);
 }
 if(['expired','cancelled','unknown'].includes(r.status)){clearInterval(timer)}
}
newQR();
</script></body></html>`

const docsHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>YYB Go API 文档</title>
<body style="margin:0;min-height:100vh;background:#0b1220;color:#e6edf7;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;line-height:1.6;padding:40px">
<h1>YYB Go API</h1>
<p style="color:#94a3b8">Swagger UI: <a style="color:#60a5fa" href="/docs/index.html">/docs/index.html</a></p>
<p style="color:#94a3b8">OpenAPI JSON: <a style="color:#60a5fa" href="/openapi.json">/openapi.json</a></p>
</body></html>`

var openAPISpec = newOpenAPISpec()
