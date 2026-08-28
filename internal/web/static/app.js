// ---- 通用工具 ----
async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (res.status === 401) { showLogin(); throw new Error('未登录'); }
  if (!res.ok) throw new Error(await res.text());
  const ct = res.headers.get('content-type') || '';
  return ct.includes('json') ? res.json() : res.text();
}

function fmtSize(n) {
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(i ? 1 : 0) + ' ' + u[i];
}

function showLogin() {
  document.getElementById('login').classList.remove('hidden');
  document.getElementById('app').classList.add('hidden');
}
function showApp() {
  document.getElementById('login').classList.add('hidden');
  document.getElementById('app').classList.remove('hidden');
  loadAll();
}

async function doLogin() {
  const user = document.getElementById('loginUser').value;
  const pass = document.getElementById('loginPass').value;
  try {
    await api('/api/login', { method: 'POST', body: JSON.stringify({ user, pass }) });
    showApp();
  } catch (e) {
    document.getElementById('loginErr').textContent = '登录失败：' + e.message;
  }
}

// ---- 标签切换 ----
document.querySelectorAll('nav a').forEach(a => {
  a.onclick = () => {
    document.querySelectorAll('nav a').forEach(x => x.classList.remove('active'));
    a.classList.add('active');
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.getElementById('tab-' + a.dataset.tab).classList.add('active');
    if (a.dataset.tab === 'preview') loadPreview();
    if (a.dataset.tab === 'menu') loadMenu();
    if (a.dataset.tab === 'isos') loadISOs();
  };
});

// ---- 加载数据 ----
async function loadAll() {
  await Promise.all([loadStatus(), loadISOs(), loadMenu(), loadConfig()]);
}

async function loadStatus() {
  try {
    const s = await api('/api/status');
    const cards = [
      ['服务器 IP', s.server_ip || '(自动探测)'],
      ['HTTP 端口', s.http_port],
      ['TFTP 端口', s.tftp_port],
      ['ProxyDHCP', s.dhcp_running ? '运行中' : (s.enable_proxy_dhcp ? '已启用(未运行)' : '已关闭')],
      ['引导脚本 URL', s.boot_script_url],
    ];
    document.getElementById('statusCards').innerHTML = cards.map(
      ([k, v]) => `<div class="card"><div class="k">${k}</div><div class="v">${v}</div></div>`
    ).join('');
  } catch (e) {}
}

async function loadISOs() {
  try {
    const isos = await api('/api/isos') || [];
    const tb = document.querySelector('#isoTable tbody');
    tb.innerHTML = isos.map(f => `
      <tr>
        <td>${f.name}</td>
        <td>${fmtSize(f.size)}</td>
        <td>
          <button class="sm" onclick="analyzeISO('${f.name}')">分析</button>
          <button class="sm danger" onclick="deleteISO('${f.name}')">删除</button>
        </td>
      </tr>`).join('') || '<tr><td colspan="3">暂无 ISO</td></tr>';
  } catch (e) {}
}

async function loadMenu() {
  try {
    const items = await api('/api/menu') || [];
    const tb = document.querySelector('#menuTable tbody');
    tb.innerHTML = items.map(e => `
      <tr>
        <td>${e.order}</td>
        <td>${e.title}</td>
        <td><span class="tag">${e.type}</span></td>
        <td>${e.enabled ? '✓' : '✗'}</td>
        <td>
          <button class="sm" onclick='editEntry(${JSON.stringify(JSON.stringify(e))})'>编辑</button>
          <button class="sm danger" onclick="deleteEntry('${e.id}')">删除</button>
        </td>
      </tr>`).join('') || '<tr><td colspan="5">暂无启动项</td></tr>';
  } catch (e) {}
}

async function loadConfig() {
  try {
    // 先填充网卡列表
    const ifaces = await api('/api/interfaces') || [];
    const sel = document.getElementById('cfgIface');
    sel.innerHTML = '<option value="">全部网卡（自动）</option>' + ifaces.map(i =>
      `<option value="${i.name}">${i.name} — ${i.ipv4.join(', ')}${i.up ? '' : ' (未启用)'}</option>`
    ).join('');

    const c = await api('/api/config');
    document.getElementById('cfgServerIP').value = c.server_ip || '';
    document.getElementById('cfgTimeout').value = c.default_menu_timeout;
    document.getElementById('cfgDefault').value = c.default_entry_id || '';
    document.getElementById('cfgProxy').checked = c.enable_proxy_dhcp;
    document.getElementById('cfgIface').value = c.dhcp_interface || '';
    document.getElementById('cfgUser').value = c.admin_user || '';
  } catch (e) {}
}

async function loadPreview() {
  try {
    const r = await api('/api/preview');
    document.getElementById('scriptPreview').textContent = r.script;
    const s = await api('/api/status');
    document.getElementById('bootUrl').textContent = s.boot_script_url;
  } catch (e) {}
}

// ---- 配置保存 ----
async function saveConfig() {
  const body = {
    server_ip: document.getElementById('cfgServerIP').value,
    default_menu_timeout: parseInt(document.getElementById('cfgTimeout').value) || 10,
    default_entry_id: document.getElementById('cfgDefault').value,
    enable_proxy_dhcp: document.getElementById('cfgProxy').checked,
    dhcp_interface: document.getElementById('cfgIface').value,
    admin_user: document.getElementById('cfgUser').value,
    admin_pass: document.getElementById('cfgPass').value,
  };
  try {
    await api('/api/config', { method: 'POST', body: JSON.stringify(body) });
    document.getElementById('cfgStatus').textContent = '已保存';
    document.getElementById('cfgPass').value = '';
    setTimeout(() => document.getElementById('cfgStatus').textContent = '', 2000);
    loadStatus();
  } catch (e) {
    document.getElementById('cfgStatus').textContent = '失败：' + e.message;
  }
}

// ---- ISO 上传（带进度）----
function uploadISO() {
  const input = document.getElementById('isoFile');
  if (!input.files.length) { alert('请选择 ISO 文件'); return; }
  const file = input.files[0];
  const fd = new FormData();
  fd.append('file', file);

  const bar = document.getElementById('uploadBar');
  const box = document.getElementById('uploadProgress');
  const status = document.getElementById('uploadStatus');
  box.classList.remove('hidden');

  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/upload');
  xhr.upload.onprogress = e => {
    if (e.lengthComputable) {
      const pct = (e.loaded / e.total * 100).toFixed(1);
      bar.style.width = pct + '%';
      status.textContent = pct + '%';
    }
  };
  xhr.onload = () => {
    if (xhr.status === 200) {
      status.textContent = '上传完成';
      bar.style.width = '100%';
      loadISOs();
    } else {
      status.textContent = '失败：' + xhr.responseText;
    }
    setTimeout(() => { box.classList.add('hidden'); bar.style.width = '0'; status.textContent = ''; }, 2000);
  };
  xhr.onerror = () => { status.textContent = '上传错误'; };
  xhr.send(fd);
}

async function deleteISO(name) {
  if (!confirm('删除 ' + name + '？')) return;
  await api('/api/delete-iso', { method: 'POST', body: JSON.stringify({ name }) });
  loadISOs();
}

let _analyzeName = '';
async function analyzeISO(name) {
  _analyzeName = name;
  const box = document.getElementById('analyzeResult');
  box.classList.remove('hidden');
  box.innerHTML = '分析中…';
  try {
    const info = await api('/api/analyze', { method: 'POST', body: JSON.stringify({ name }) });
    renderAnalyze(name, info);
  } catch (e) {
    box.innerHTML = '分析失败：' + e.message;
  }
}

function renderAnalyze(name, info) {
  const box = document.getElementById('analyzeResult');
  const files = (info.files || []).map(f =>
    `<label><input type="checkbox" class="exfile" value="${f}"
      ${(f === info.kernel || f === info.initrd) ? 'checked' : ''}> ${f}</label>`
  ).join('');
  box.innerHTML = `
    <h3>分析结果：${name}</h3>
    <p>类型：<span class="tag">${info.kind}</span> 发行版：<b>${info.distro || '-'}</b></p>
    <p>探测内核：<code>${info.kernel || '未找到'}</code></p>
    <p>探测 initrd：<code>${info.initrd || '未找到'}</code></p>
    <p style="color:var(--muted)">${info.note || ''}</p>
    <p style="margin-top:12px"><b>选择要提取的文件</b>（提取后可通过 HTTP 直接下载给 iPXE）：</p>
    <div class="files">${files || '无文件'}</div>
    <div style="margin-top:12px">
      <input id="extractDest" placeholder="提取目录名（默认用 ISO 名）" style="max-width:260px;display:inline-block">
      <button onclick="extractFiles()">提取选中文件</button>
      <span id="extractStatus"></span>
    </div>`;
}

async function extractFiles() {
  const files = Array.from(document.querySelectorAll('.exfile:checked')).map(c => c.value);
  if (!files.length) { alert('请勾选文件'); return; }
  const dest = document.getElementById('extractDest').value;
  document.getElementById('extractStatus').textContent = '提取中…';
  try {
    const r = await api('/api/extract', { method: 'POST', body: JSON.stringify({ name: _analyzeName, files, dest }) });
    document.getElementById('extractStatus').innerHTML =
      '完成，生成路径：<br>' + (r.paths || []).map(p => `<code>${p}</code>`).join('<br>');
  } catch (e) {
    document.getElementById('extractStatus').textContent = '失败：' + e.message;
  }
}

// ---- 菜单编辑 ----
function onTypeChange() {
  const t = document.getElementById('eType').value;
  document.querySelectorAll('.type-fields').forEach(d => {
    d.classList.toggle('hidden', d.dataset.for !== t);
  });
}

function openEntryEditor() {
  document.getElementById('entryModalTitle').textContent = '新增启动项';
  ['eTitle','eKernel','eInitrd','eAppend','eBCD','eBootSDI','eBootWIM','eWinExtras',
   'eMbootC32','eMbootEFI','eBootCFG','eSanURL','eScript'].forEach(id => document.getElementById(id).value = '');
  document.getElementById('eWimboot').value = '/files/tftp/wimboot';
  document.getElementById('eOrder').value = 0;
  document.getElementById('eEnabled').checked = true;
  document.getElementById('eType').value = 'linux';
  document.getElementById('entryModal').dataset.id = '';
  onTypeChange();
  document.getElementById('entryModal').classList.remove('hidden');
}

function editEntry(json) {
  const e = JSON.parse(json);
  openEntryEditor();
  document.getElementById('entryModalTitle').textContent = '编辑启动项';
  document.getElementById('entryModal').dataset.id = e.id;
  document.getElementById('eTitle').value = e.title || '';
  document.getElementById('eType').value = e.type || 'linux';
  document.getElementById('eOrder').value = e.order || 0;
  document.getElementById('eEnabled').checked = !!e.enabled;
  document.getElementById('eKernel').value = e.kernel || '';
  document.getElementById('eInitrd').value = e.initrd || '';
  document.getElementById('eAppend').value = e.append || '';
  document.getElementById('eWimboot').value = e.wimboot || '/files/tftp/wimboot';
  document.getElementById('eBCD').value = e.bcd || '';
  document.getElementById('eBootSDI').value = e.boot_sdi || '';
  document.getElementById('eBootWIM').value = e.boot_wim || '';
  document.getElementById('eWinExtras').value = e.win_extras || '';
  document.getElementById('eMbootC32').value = e.mboot_c32 || '';
  document.getElementById('eMbootEFI').value = e.mboot_efi || '';
  document.getElementById('eBootCFG').value = e.boot_cfg || '';
  document.getElementById('eSanURL').value = e.san_url || '';
  document.getElementById('eScript').value = e.script || '';
  onTypeChange();
}

function closeEntryEditor() {
  document.getElementById('entryModal').classList.add('hidden');
}

async function saveEntry() {
  const body = {
    id: document.getElementById('entryModal').dataset.id || '',
    title: document.getElementById('eTitle').value,
    type: document.getElementById('eType').value,
    order: parseInt(document.getElementById('eOrder').value) || 0,
    enabled: document.getElementById('eEnabled').checked,
    kernel: document.getElementById('eKernel').value,
    initrd: document.getElementById('eInitrd').value,
    append: document.getElementById('eAppend').value,
    wimboot: document.getElementById('eWimboot').value,
    bcd: document.getElementById('eBCD').value,
    boot_sdi: document.getElementById('eBootSDI').value,
    boot_wim: document.getElementById('eBootWIM').value,
    win_extras: document.getElementById('eWinExtras').value,
    mboot_c32: document.getElementById('eMbootC32').value,
    mboot_efi: document.getElementById('eMbootEFI').value,
    boot_cfg: document.getElementById('eBootCFG').value,
    san_url: document.getElementById('eSanURL').value,
    script: document.getElementById('eScript').value,
  };
  if (!body.title) { alert('标题不能为空'); return; }
  try {
    await api('/api/menu', { method: 'POST', body: JSON.stringify(body) });
    closeEntryEditor();
    loadMenu();
  } catch (e) {
    alert('保存失败：' + e.message);
  }
}

async function deleteEntry(id) {
  if (!confirm('删除该启动项？')) return;
  await api('/api/menu/' + id, { method: 'DELETE' });
  loadMenu();
}

// ---- 引导 ISO 生成 ----
function onBiModeChange() {
  const m = document.getElementById('biIPMode').value;
  document.getElementById('biStatic').classList.toggle('hidden', m !== 'static');
}

function biParams() {
  return {
    chain_url: document.getElementById('biChain').value.trim(),
    ip_mode: document.getElementById('biIPMode').value,
    net_if: document.getElementById('biNetIf').value.trim(),
    ip: document.getElementById('biIP').value.trim(),
    netmask: document.getElementById('biMask').value.trim(),
    gateway: document.getElementById('biGw').value.trim(),
    dns: document.getElementById('biDns').value.trim(),
    vlan_id: parseInt(document.getElementById('biVlan').value) || 0,
    timeout: parseInt(document.getElementById('biTimeout').value) || 5,
  };
}

async function previewAutoexec() {
  try {
    const r = await api('/api/preview-autoexec', { method: 'POST', body: JSON.stringify(biParams()) });
    const pre = document.getElementById('biPreview');
    pre.textContent = r.script;
    pre.classList.remove('hidden');
  } catch (e) {
    document.getElementById('biStatus').textContent = '失败：' + e.message;
  }
}

async function genBootISO() {
  const status = document.getElementById('biStatus');
  status.textContent = '生成中（首次需下载 iPXE 二进制）…';
  try {
    const res = await fetch('/api/gen-boot-iso', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(biParams()),
    });
    if (!res.ok) throw new Error(await res.text());
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'ipxe-boot.iso';
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    status.textContent = '已生成并开始下载';
  } catch (e) {
    status.textContent = '失败：' + e.message;
  }
}

// 初始化：尝试拉取状态判断是否已登录
(async () => {
  try { await api('/api/status'); showApp(); }
  catch { showLogin(); }
})();
