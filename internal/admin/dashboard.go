package admin

import (
	"fmt"
	"net/http"
)

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

const dashboardHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>IPv6 Proxy Console</title>
<style>
body{background:#000;color:#0f0;font-family:"Courier New",monospace;font-size:13px;margin:10px}
h1{color:#0f0;font-size:16px;border-bottom:1px solid #0f0;padding-bottom:4px}
h2{color:#0f0;font-size:14px;margin:12px 0 4px 0}
table{border-collapse:collapse;width:100%;margin-bottom:10px}
th,td{border:1px solid #0a0;padding:3px 6px;text-align:left;font-size:12px}
th{background:#001a00;color:#0f0}
td{color:#0c0}
tr:hover td{background:#001a00}
input{background:#000;color:#0f0;border:1px solid #0a0;padding:2px 6px;font-family:monospace;font-size:12px}
button{background:#001a00;color:#0f0;border:1px solid #0a0;padding:2px 10px;cursor:pointer;font-family:monospace;font-size:12px}
button:hover{background:#003300}
.red{color:#f00}
.yellow{color:#ff0}
.cyan{color:#0ff}
.dim{color:#060}
.status-bar{border:1px solid #0a0;padding:4px 8px;margin-bottom:8px;font-size:11px}
.inline{display:inline-block;margin-right:12px}
.domain-table{margin-left:20px;width:auto}
.domain-table th,.domain-table td{font-size:11px;padding:1px 6px}
.expand{cursor:pointer;color:#0ff;text-decoration:underline}
</style>
</head>
<body>
<h1>IPv6 Proxy Console</h1>
<div class="status-bar">
<span class="inline">Status: <span id="status" class="cyan">LOADING</span></span>
<span class="inline">Uptime: <span id="uptime">-</span></span>
<span class="inline">Refresh: <span id="tick">0</span>s ago</span>
</div>

<h2>[ Overview ]</h2>
<table>
<tr>
<th>Pool Size</th><th>Active Sessions</th><th>Total Requests</th><th>HTTP Conns</th>
<th>HTTP Traffic</th><th>HTTP Failed</th><th>SOCKS5 Conns</th><th>SOCKS5 Traffic</th><th>SOCKS5 Failed</th>
<th>Max Latency</th><th>Min Latency</th>
</tr>
<tr>
<td id="pool">-</td><td id="sessions">-</td><td id="reqs">-</td><td id="conns">-</td>
<td id="bytes">-</td><td id="failed" class="red">-</td>
<td id="s5conns">-</td><td id="s5bytes">-</td><td id="s5failed" class="red">-</td>
<td id="maxlat">-</td><td id="minlat">-</td>
</tr>
</table>

<h2>[ Proxy Users ]</h2>
<table>
<tr><th>Username</th><th>Action</th></tr>
<tbody id="users"></tbody>
</table>
<div>
<input id="nu" placeholder="username" size="12">
<input id="np" placeholder="password" size="16" type="password">
<button onclick="addUser()">Add</button>
</div>

<h2>[ Sticky Sessions ] <button onclick="clearAll()">Clear All</button></h2>
<table>
<tr>
<th>#</th><th>Fingerprint</th><th>Exit IPv6</th><th>Hits</th>
<th>Max(ms)</th><th>Min(ms)</th><th>Avg(ms)</th>
<th>Created</th><th>Last Seen</th><th>Domains</th><th>Act</th>
</tr>
<tbody id="sess"></tbody>
</table>

<h2>[ Banned IPs ]</h2>
<table>
<tr><th>IP</th><th>Banned At</th><th>Failures</th><th>Action</th></tr>
<tbody id="banned"></tbody>
</table>

<div id="msg" class="status-bar" style="display:none"></div>

<script>
var tick=0,timer;
function $(id){return document.getElementById(id)}
function fmt(b){if(b>1073741824)return(b/1073741824).toFixed(2)+'G';if(b>1048576)return(b/1048576).toFixed(1)+'M';if(b>1024)return(b/1024).toFixed(1)+'K';return b+'B'}
function lat(ms){return ms<0?'-':ms+'ms'}
function flash(t){var m=$('msg');m.textContent=t;m.style.display='block';setTimeout(function(){m.style.display='none'},2000)}
async function api(u,o){var r=await fetch(u,o);if(!r.ok)throw new Error(r.status);return r.json()}

async function refresh(){
try{
var ov=await api('/api/overview');
$('status').textContent='ONLINE';$('status').className='cyan';
$('uptime').textContent=ov.uptime_str;
$('pool').textContent=ov.pool_size;
$('sessions').textContent=ov.active_sessions;
$('reqs').textContent=ov.total_requests;
$('conns').textContent=ov.active_conns;
$('bytes').textContent=fmt(ov.total_bytes);
$('failed').textContent=ov.failed_requests;
$('s5conns').textContent=ov.socks5_active_conns;
$('s5bytes').textContent=fmt(ov.socks5_total_bytes);
$('s5failed').textContent=ov.socks5_failed_requests;
$('maxlat').textContent=lat(ov.max_latency_ms);
$('minlat').textContent=lat(ov.min_latency_ms);

var u=await api('/api/users');
var uh='';
for(var i=0;i<u.users.length;i++){
uh+='<tr><td>'+u.users[i]+'</td><td><button onclick="rmUser(\''+u.users[i]+'\')">Remove</button></td></tr>';
}
$('users').innerHTML=uh;

var ss=await api('/api/sessions');
var sh='';
for(var i=0;i<ss.length;i++){
var s=ss[i];
var did='d'+i;
var dhtml='<span class="expand" onclick="toggle(\''+did+'\')">'+s.domains.length+' domain(s)</span>';
dhtml+='<table class="domain-table" id="'+did+'" style="display:none">';
dhtml+='<tr><th>Domain</th><th>Hits</th><th>Max</th><th>Min</th><th>Last</th></tr>';
if(s.domains){
for(var j=0;j<s.domains.length;j++){
var d=s.domains[j];
dhtml+='<tr><td>'+d.domain+'</td><td>'+d.hits+'</td><td>'+lat(d.max_lat_ms)+'</td><td>'+lat(d.min_lat_ms)+'</td><td>'+d.last_seen+'</td></tr>';
}}
dhtml+='</table>';
sh+='<tr><td>'+(i+1)+'</td><td class="cyan">'+s.fingerprint+'</td><td>'+s.exit_ip+'</td>';
sh+='<td>'+s.total_hits+'</td><td>'+lat(s.max_lat_ms)+'</td><td>'+lat(s.min_lat_ms)+'</td><td>'+lat(s.avg_lat_ms)+'</td>';
sh+='<td class="dim">'+s.created_at+'</td><td>'+s.last_seen+'</td>';
sh+='<td>'+dhtml+'</td>';
sh+='<td><button onclick="rmSess(\''+s.fingerprint+'\')">X</button></td></tr>';
}
$('sess').innerHTML=sh||'<tr><td colspan="11" class="dim">No active sessions</td></tr>';

try{
var bn=await api('/api/banned');
var bh='';
if(bn.banned&&bn.banned.length>0){
for(var i=0;i<bn.banned.length;i++){
var b=bn.banned[i];
bh+='<tr><td class="red">'+b.ip+'</td><td>'+b.banned_at+'</td><td>'+b.failures+'</td>';
bh+='<td><button onclick="unbanIP(\''+b.ip+'\')">Unban</button></td></tr>';
}}
$('banned').innerHTML=bh||'<tr><td colspan="4" class="dim">No banned IPs</td></tr>';
}catch(e){}

tick=0;
}catch(e){$('status').textContent='ERROR';$('status').className='red'}
}

function toggle(id){var el=document.getElementById(id);el.style.display=el.style.display==='none'?'':'none'}
async function addUser(){var u=$('nu').value,p=$('np').value;if(!u||!p)return;await api('/api/users/add',{method:'POST',body:JSON.stringify({user:u,pass:p})});$('nu').value='';$('np').value='';flash('User added');refresh()}
async function rmUser(u){await api('/api/users/remove',{method:'POST',body:JSON.stringify({user:u})});flash('Removed');refresh()}
async function clearAll(){await api('/api/sessions/clear',{method:'POST'});flash('Cleared');refresh()}
async function rmSess(fp){await api('/api/sessions/remove',{method:'POST',body:JSON.stringify({fingerprint:fp})});flash('Removed');refresh()}
async function unbanIP(ip){await api('/api/banned/unban',{method:'POST',body:JSON.stringify({ip:ip})});flash('Unbanned');refresh()}

refresh();setInterval(refresh,3000);
setInterval(function(){tick++;$('tick').textContent=tick},1000);
</script>
</body>
</html>`
