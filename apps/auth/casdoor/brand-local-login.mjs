import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {DatabaseSync} from 'node:sqlite';
export function applyLoginBranding({database,clientId,origin,backup}){
 if(!database||!clientId||!origin||!backup)throw Error('Explicit database, client ID, origin and backup are required');
 const brand=JSON.parse(fs.readFileSync(new URL('./login-branding.json',import.meta.url),'utf8'));
 const base=new URL(origin);if(!['http:','https:'].includes(base.protocol))throw Error('Invalid application origin');
 const db=new DatabaseSync(database);try{
 const app=db.prepare('SELECT owner,name,display_name,title,description,logo,logo_dark,favicon,homepage_url,footer_html,form_css,signin_items FROM application WHERE client_id=?').get(clientId);if(!app)throw Error('OIDC application not found');
 fs.writeFileSync(backup,JSON.stringify(app,null,2),{mode:0o600,flag:'wx'});
 let items=JSON.parse(app.signin_items||'[]').filter(item=>item.name!=='Seller Ops intro');
 const intro={name:'Seller Ops intro',visible:true,isCustom:true,label:'',customCss:brand.introHtml,rule:'None',placeholder:''};const logoIndex=items.findIndex(item=>item.name==='Logo');items.splice(logoIndex+1,0,intro);
 for(const item of items){if(item.name==='Username'){item.label='账号';item.placeholder='用户名、邮箱或手机号';}if(item.name==='Password'){item.label='密码';item.placeholder='请输入密码';}if(item.name==='Login button')item.label='登录工作台';}
 const asset=key=>new URL(brand[key],base).href;
 db.exec('BEGIN IMMEDIATE');try{const result=db.prepare('UPDATE application SET display_name=?,title=?,description=?,logo=?,logo_dark=?,favicon=?,homepage_url=?,footer_html=?,form_css=?,signin_items=? WHERE owner=? AND name=?').run(brand.displayName,brand.title,brand.description,asset('logo'),asset('logoDark'),asset('favicon'),base.origin,brand.footerHtml,brand.formCss,JSON.stringify(items),app.owner,app.name);if(result.changes!==1)throw Error('Unexpected application update count');db.exec('COMMIT');}catch(e){db.exec('ROLLBACK');throw e}
 return {owner:app.owner,name:app.name};
 }finally{db.close()}
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)){applyLoginBranding({database:process.env.CASDOOR_SQLITE_PATH,clientId:process.env.OIDC_CLIENT_ID,origin:process.env.APP_ORIGIN,backup:process.env.CASDOOR_BRANDING_BACKUP});console.log('Seller Ops login branding applied');}
