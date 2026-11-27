import{test,expect}from'bun:test';import fs from'node:fs';import vm from'node:vm';import ts from'typescript';import{screeningRPCRoutes}from'../src/features/screening/screening-rpc-routes';import{ecomRPCRoutes}from'../src/features/ecom/ecom-rpc-routes';import{authRPCRoutes}from'../src/lib/auth-rpc-routes';import{ScreeningService}from'../src/gen/sellerops/screening/v1/screening_pb';import{ScreeningImportService}from'../src/gen/sellerops/screening/v1/import_pb';import{EcomService}from'../src/gen/sellerops/ecom/v1/ecom_pb';import{AuthService}from'../src/gen/sellerops/auth/v1/auth_pb';
const id='11111111-1111-4111-8111-111111111111',hash='a'.repeat(64);
function expand(input){let pos=0;function expr(){let result=[],seq=[''];while(pos<input.length){const c=input[pos++];if(c==='|'){result.push(...seq);seq=[''];continue}if(c===')')break;let values;if(c==='('){values=expr();if(input[pos]==='?'){pos++;values.push('')}}else{values=[c]}seq=seq.flatMap(a=>values.map(b=>a+b))}return[...result,...seq]}return expr()}
function samples(path,resource=id){return path.replaceAll('{id}',resource).replaceAll('{row_id}',resource).replaceAll('{group_key}',hash).replaceAll('{asin}','B000000001').replaceAll('{action}','approve')}
test('every exposed screening metadata route supports UUID and MySQL IDs with generated RPC coverage',()=>{
 const source=fs.readFileSync(new URL('../src/app/api/ecom/screening/[...path]/route.ts',import.meta.url),'utf8');const ast=ts.createSourceFile('route.ts',source,ts.ScriptTarget.Latest,true);let initializer;function visit(n){if(ts.isVariableDeclaration(n)&&n.name.getText(ast)==='routes')initializer=n.initializer;ts.forEachChild(n,visit)}visit(ast);expect(initializer).toBeDefined();
 const constants=source.slice(source.indexOf('const uuid ='),source.indexOf('const routes ='));
 const script=ts.transpileModule(constants+'const routes='+initializer.getText(ast)+';({rules:routes,resourceId});',{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText;
 const {rules,resourceId}=vm.runInNewContext(script);
 for(const resource of [id,'5327']){
  const coverage=new Set(screeningRPCRoutes.map(x=>x.method+' '+samples(x.path,resource)));
  for(const key of ['GET imports','GET imports/'+resource,'GET imports/'+resource+'/preview','GET jobs/'+resource,'GET imports/'+resource+'/removal-preview','POST imports/'+resource+'/commit','PUT imports/'+resource+'/mapping','DELETE imports/'+resource,'POST jobs/'+resource+'/retry'])coverage.add(key);
  const files=new Set(['POST imports','GET imports/'+resource+'/source','GET imports/'+resource+'/errors','GET exports/'+resource+'/download']);let count=0;
  for(const [method,pattern]of rules){const replaced=pattern.replaceAll(resourceId,resource).replaceAll('[a-f0-9]{64}',hash);for(const path of expand(replaced)){expect(coverage.has(method+' '+path)||files.has(method+' '+path)).toBe(true);count++}}
  expect(count).toBe(50);
 }
 const allowed=path=>rules.some(([method,pattern])=>method==='GET'&&new RegExp('^(?:'+pattern+')$').test(path));
 for(const suffix of ['','/monthly','/family','/history','/observations','/references','/profit-scenarios'])expect(allowed('products/5327'+suffix)).toBe(true);
 for(const bad of ['0','-1','1.2','abc','5327/extra','184467440737095516160'])expect(allowed('products/'+bad+'/monthly')).toBe(false);
});
test('every mapped operation has a real generated method and every method is mapped',()=>{for(const [routes,service]of [[screeningRPCRoutes,ScreeningService],[ecomRPCRoutes,EcomService],[authRPCRoutes,AuthService]]){expect(new Set(routes.map(x=>x.name))).toEqual(new Set(service.methods.map(x=>x.name)));for(const route of routes)expect(service.methods.find(x=>x.name===route.name)?.methodKind).toBe('unary')}expect(ScreeningImportService.methods.length).toBe(9)});
