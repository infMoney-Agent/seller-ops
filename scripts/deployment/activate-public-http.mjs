import fs from 'node:fs';
import cp from 'node:child_process';
import http from 'node:http';
import {DatabaseSync} from 'node:sqlite';
const root='/data/workspace/yijiangli/seller-ops',state='/home/yijiangli/.local/state/seller-ops-native';
const configPath=state+'/runtime.json',original=fs.readFileSync(configPath,'utf8'),c=JSON.parse(original),origin='http://43.139.227.179';
const stage=fs.readFileSync('/tmp/public-http-stage','utf8').trim();
if(!stage.startsWith('/tmp/public-http-build-')||!fs.existsSync(stage+'/apps/web/.next/BUILD_ID')||!fs.readFileSync('/tmp/public-http-build.log','utf8').includes('(Dynamic)'))throw Error('Web build is not ready');
if(!fs.existsSync('/tmp/seller-auth-public-http'))throw Error('Auth build is not ready');
// node:fetch does not preserve an overridden Host in this runtime. Use the HTTP client
// to probe loopback with the same Host that external browsers send.
const probe = path => new Promise((resolve,reject) => {
 const request=http.request('http://127.0.0.1'+path,{headers:{Host:'43.139.227.179'}},response=>{
  response.resume();resolve({status:response.statusCode,headers:{get:name=>response.headers[name.toLowerCase()]||null,getSetCookie:()=>response.headers['set-cookie']||[]}});
 });request.on('error',reject);request.setTimeout(5000,()=>request.destroy(new Error('Gateway readiness timeout')));request.end();
});
const gateway=await probe('/auth/sign-in');
if(gateway.status!==307)throw Error('Port 80 gateway is not ready');
const originalProcesses=JSON.parse(fs.readFileSync(state+'/processes.json','utf8'));
for(const name of ['auth','web'])if(fs.readlinkSync('/proc/'+originalProcesses.find(x=>x.name===name).pid+'/cwd')!==root+'/apps/'+name)throw Error('Unexpected '+name+' process');
const backup=state+'/backups/public-http-'+Date.now();fs.mkdirSync(backup,{recursive:true,mode:0o700});fs.writeFileSync(backup+'/runtime.json',original,{mode:0o600});
const db=new DatabaseSync('/home/yijiangli/.local/casdoor-src/casdoor.db');
const app=db.prepare('SELECT owner,name,redirect_uris,logo,logo_dark,favicon,homepage_url FROM application WHERE client_id=?').get(c.auth.OIDC_CLIENT_ID);if(!app)throw Error('Login application not found');
fs.writeFileSync(backup+'/application.json',JSON.stringify(app),{mode:0o600});
const redirects=JSON.parse(app.redirect_uris||'[]');if(!redirects.includes(origin+'/api/auth/callback'))redirects.push(origin+'/api/auth/callback');
const asset=value=>typeof value==='string'?value.replaceAll('http://localhost:18443',origin):value;
db.prepare('UPDATE application SET redirect_uris=?,logo=?,logo_dark=?,favicon=?,homepage_url=? WHERE owner=? AND name=?').run(JSON.stringify(redirects),asset(app.logo),asset(app.logo_dark),asset(app.favicon),origin,app.owner,app.name);
c.auth.APP_ORIGIN=origin+',http://localhost:18443';c.auth.SELLER_ALLOW_HTTP='true';c.auth.OIDC_BROWSER_AUTH_URL=origin+'/login/oauth/authorize';c.web.SELLER_APP_ORIGIN=c.auth.APP_ORIGIN;
fs.writeFileSync(configPath,JSON.stringify(c),{mode:0o600});
let processes=structuredClone(originalProcesses),swapped=false;
async function stop(){for(const name of ['auth','web']){const pid=processes.find(x=>x.name===name).pid;try{process.kill(pid,'SIGTERM')}catch{}for(let i=0;i<100;i++){try{process.kill(pid,0)}catch{break}await new Promise(r=>setTimeout(r,100));}}}
function start(config){for(const name of ['auth','web']){const log=fs.openSync(state+'/'+name+'.log','a'),command=name==='auth'?state+'/bin/seller-auth':config.node,args=name==='auth'?[]:[root+'/apps/web/node_modules/next/dist/bin/next','start','--hostname','127.0.0.1','--port','18443'];const child=cp.spawn(command,args,{cwd:root+'/apps/'+name,env:{...process.env,...config[name]},detached:true,stdio:['ignore',log,log]});child.unref();processes.find(x=>x.name===name).pid=child.pid;}fs.writeFileSync(state+'/processes.json',JSON.stringify(processes,null,2));}
try{
 await stop();fs.renameSync(root+'/apps/web/.next',backup+'/.next');fs.renameSync(stage+'/apps/web/.next',root+'/apps/web/.next');fs.renameSync(state+'/bin/seller-auth',backup+'/seller-auth');fs.renameSync('/tmp/seller-auth-public-http',state+'/bin/seller-auth');fs.chmodSync(state+'/bin/seller-auth',0o750);swapped=true;start(c);
 let ready=false;for(let i=0;i<40;i++){try{const response=await probe('/api/auth/login');const location=new URL(response.headers.get('location'));if(response.status===302&&location.origin===origin&&location.searchParams.get('redirect_uri')===origin+'/api/auth/callback'){const cookie=response.headers.getSetCookie().find(value=>value.startsWith('seller_oidc='));if(cookie&&!cookie.includes('; Secure')){ready=true;break}}}catch{}await new Promise(r=>setTimeout(r,300));}
 if(!ready)throw Error('Public login readiness check failed');
 console.log('Public HTTP entry ready: '+origin+'/dashboard/ecom/screening');
}catch(error){
 await stop();fs.writeFileSync(configPath,original,{mode:0o600});db.prepare('UPDATE application SET redirect_uris=?,logo=?,logo_dark=?,favicon=?,homepage_url=? WHERE owner=? AND name=?').run(app.redirect_uris,app.logo,app.logo_dark,app.favicon,app.homepage_url,app.owner,app.name);
 if(swapped){fs.renameSync(root+'/apps/web/.next',stage+'/apps/web/.next');fs.renameSync(backup+'/.next',root+'/apps/web/.next');fs.renameSync(state+'/bin/seller-auth','/tmp/seller-auth-public-http');fs.renameSync(backup+'/seller-auth',state+'/bin/seller-auth');}start(JSON.parse(original));throw error;
}finally{db.close();}
