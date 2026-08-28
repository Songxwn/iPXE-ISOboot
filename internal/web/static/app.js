async function api(path, opts = {}) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' }, ...opts });
  if (res.status === 401) { showLogin(); throw new Error('未登录'); }
  if (!res.ok) throw new Error(await res.text());
  const ct = res.headers.get('content-type') || '';
  return ct.includes('json') ? res.json() : res.text();
}
function fmtSize(n) { const u = ['B','KB','MB','GB','TB']; let i = 0; while (n >= 1024 && i < u.length-1){ n/=1024; i++; } return n.toFixed(i?1:0)+' '+u[i]; }
function showLogin(){ document.getElementById('login').classList.remove('hidden'); document.getElementById('app').classList.add('hidden'); }
function showApp(){ document.getElementById('login').classList.add('hidden'); document.getElementById('app').classList.remove('hidden'); loadAll(); }

async function doLogin() {
  try {
    await api('/api/login', { method:'POST', body: JSON.stringify({ user: loginUser.value, pass: loginPass.value }) });
    showApp();
  } catch(e){ document.getElementById('loginErr').textContent = '登录失败：'+e.message; }
}

document.querySelectorAll('nav a').forEach(a => {
  a.onclick = () => {
    document.querySelectorAll('nav a').forEach(x=>x.classList.remove('active'));
    a.classList.add('active');
    document.querySelectorAll('.tab').forEach(t=>t.classList.remove('active'));
    document.getElementById('tab-'+a.dataset.tab).classList.add('active');
    if (a.dataset.tab==='preview') loadPreview();
    if (a.dataset.tab==='menu') loadMenu();
    if (a.dataset.tab==='isos') loadISOs();
  };
});

async function loadAll(){ await Promise.all([loadStatus(), loadISOs(), loadMenu(), loadConfig()]); }

async function loadStatus() {
  try {
    const s = await api('/api/status');
    const cards = [
      ['服务器 IP', s.server_ip||'(自动)'],
      ['HTTP 端口', s.http_port],
      ['TFTP 端口', s.tftp_port],
      ['ProxyDHCP', s.dhcp_running?'运行中':(s.enable_proxy_dhcp?'已启用(未运行)':'已关闭')],
      ['引导脚本', s.boot_script_url],
    ];
    document.getElementById('statusCards').innerHTML = cards.map(([k,v])=>`<div class="card"><div class="k">${k}</div><div class="v">${v}</div></div>`).join('');
  } catch(e){}
}

async function loadISOs() {
  try {
    const isos = await api('/api/isos') || [];
    document.querySelector('#isoTable tbody').innerHTML = isos.map(f=>`
      <tr><td>${f.name}</td><td>${fmtSize(f.size)}</td>
      <td><button class="sm" onclick="quickAdd('${f.name}')">一键加入菜单</button>
      <button class="sm danger" onclick="deleteISO('${f.name}')">删除</button></td></tr>`).join('') || '<tr><td colspan="3">暂无 ISO</td></tr>';
  } catch(e){}
}

async function loadMenu() {
  try {
    const items = await api('/api/menu') || [];
    document.querySelector('#menuTable tbody').innerHTML = items.map(e=>`
      <tr><td>${e.order}</td><td>${e.title}</td><td><span class="tag">${e.family||'-'}</span></td>
      <td>${e.enabled?'✓':'✗'}</td>
      <td><button class="sm" onclick='editEntry(${JSON.stringify(JSON.stringify(e))})'>编辑</button>
      <button class="sm danger" onclick="deleteEntry('${e.id}')">删除</button></td></tr>`).join('') || '<tr><td colspan="5">暂无启动项</td></tr>';
  } catch(e){}
}

async function loadConfig() {
  try {
    const ifaces = await api('/api/interfaces') || [];
    const sel = document.getElementById('cfgIface');
    sel.innerHTML = '<option value="">全部网卡（自动）</option>' + ifaces.map(i=>`<option value="${i.name}">${i.name} — ${i.ipv4.join(', ')}</option>`).join('');
    const c = await api('/api/config');
    cfgServerIP.value = c.server_ip||''; cfgTitle.value = c.menu_title||''; cfgTimeout.value = c.menu_timeout;
    cfgProxy.checked = c.enable_proxy_dhcp; cfgIface.value = c.dhcp_interface||''; cfgUser.value = c.admin_user||'';
  } catch(e){}
}

async function loadPreview() {
  try { const r = await api('/api/grub-preview'); document.getElementById('cfgPreview').textContent = r.cfg; } catch(e){}
}

async function saveConfig() {
  const body = {
    server_ip: cfgServerIP.value, menu_title: cfgTitle.value,
    menu_timeout: parseInt(cfgTimeout.value)||30, enable_proxy_dhcp: cfgProxy.checked,
    dhcp_interface: cfgIface.value, admin_user: cfgUser.value, admin_pass: cfgPass.value,
  };
  try { await api('/api/config',{method:'POST',body:JSON.stringify(body)}); cfgStatus.textContent='已保存'; cfgPass.value='';
    setTimeout(()=>cfgStatus.textContent='',2000); loadStatus();
  } catch(e){ cfgStatus.textContent='失败：'+e.message; }
}

function uploadISO() {
  const input = document.getElementById('isoFile');
  if (!input.files.length){ alert('请选择 ISO'); return; }
  const fd = new FormData(); fd.append('file', input.files[0]);
  const bar = document.getElementById('uploadBar'), box = document.getElementById('uploadProgress'), st = document.getElementById('uploadStatus');
  box.classList.remove('hidden');
  const xhr = new XMLHttpRequest();
  xhr.open('POST','/api/upload');
  xhr.upload.onprogress = e => { if(e.lengthComputable){ const p=(e.loaded/e.total*100).toFixed(1); bar.style.width=p+'%'; st.textContent=p+'%'; } };
  xhr.onload = () => { st.textContent = xhr.status===200?'上传完成':'失败：'+xhr.responseText; loadISOs(); setTimeout(()=>{box.classList.add('hidden');bar.style.width='0';st.textContent='';},2000); };
  xhr.onerror = () => st.textContent='上传错误';
  xhr.send(fd);
}

async function deleteISO(name){ if(!confirm('删除 '+name+'？'))return; await api('/api/delete-iso',{method:'POST',body:JSON.stringify({name})}); loadISOs(); }

async function quickAdd(name){
  try { const r = await api('/api/quick-add',{method:'POST',body:JSON.stringify({name})});
    alert('已加入：'+r.entry.title+'\n配方族：'+r.family+'\n'+(r.note||'')); loadMenu();
  } catch(e){ alert('失败：'+e.message); }
}

function openEditor() {
  document.getElementById('editModal').dataset.id = '';
  mTitle.value=''; mISO.value=''; mFamily.value='generic'; mOrder.value=0; mEnabled.checked=true; mCustom.value='';
  document.getElementById('editModal').classList.remove('hidden');
}
function editEntry(json){
  const e = JSON.parse(json);
  openEditor();
  document.getElementById('editModal').dataset.id = e.id;
  mTitle.value=e.title||''; mISO.value=e.iso_name||''; mFamily.value=e.family||'generic';
  mOrder.value=e.order||0; mEnabled.checked=!!e.enabled; mCustom.value=e.custom_cfg||'';
}
function closeEditor(){ document.getElementById('editModal').classList.add('hidden'); }

async function saveEntry() {
  const body = {
    id: document.getElementById('editModal').dataset.id||'',
    title: mTitle.value, iso_name: mISO.value, family: mFamily.value,
    order: parseInt(mOrder.value)||0, enabled: mEnabled.checked, custom_cfg: mCustom.value,
  };
  if (!body.title){ alert('标题不能为空'); return; }
  try { await api('/api/menu',{method:'POST',body:JSON.stringify(body)}); closeEditor(); loadMenu(); }
  catch(e){ alert('保存失败：'+e.message); }
}
async function deleteEntry(id){ if(!confirm('删除该项？'))return; await api('/api/menu/'+id,{method:'DELETE'}); loadMenu(); }

async function loadVersion(){
  try { const v = await fetch('/api/version').then(r=>r.json()); headerVer.textContent=v.version||''; loginVer.textContent=v.version||''; } catch(e){}
}

(async () => {
  loadVersion();
  try { await api('/api/status'); showApp(); } catch { showLogin(); }
})();
