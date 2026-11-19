import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
const require = createRequire(new URL('../apps/web/package.json', import.meta.url));
const ts = require('typescript');
const source = readFileSync(new URL('../apps/web/src/features/screening/profit-validation.ts',import.meta.url),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2023}}).outputText;
const exports={};vm.runInNewContext(compiled,{exports});
const validate=exports.validateProfitDraft;
const base={name:'基准',sale_price_usd:'19.99',discount_usd:'0',purchase_cny:'18',packaging_cny:'2',freight_cny:'8',other_landed_cny:'0',fx_cny_per_usd:'7.20',commission_rate:'0.15',fba_fee_usd:'4.00',storage_usd:'0.10',ad_rate:'0.10',return_rate:'0.03',loss_per_return_usd:'12',other_usd:'0',monthly_units:100,fee_source:'',note:''};
assert.equal(Object.keys(validate(base)).length,0);
for (const [field,value] of [['sale_price_usd','0'],['fx_cny_per_usd','0'],['commission_rate','1.01'],['ad_rate','-0.1'],['return_rate','2'],['purchase_cny','-1'],['monthly_units',1.5],['monthly_units',Number.MAX_SAFE_INTEGER+1],['name',''],['discount_usd','19.99'],['fba_fee_usd','1e3']]) {
 assert.ok(validate({...base,[field]:value})[field],field+' boundary must be rejected');
}
assert.equal(Object.keys(validate({...base,commission_rate:'1',ad_rate:'1',return_rate:'0',purchase_cny:'0',monthly_units:0})).length,0,'zero and boundary rates are valid, even when break-even cannot be computed');
const missing={...base,purchase_cny:null,fba_fee_usd:null,sale_price_usd:null};
assert.equal(Object.keys(validate(missing)).length,0,'missing costs or batch prices must stay incomplete, not become zero');
assert.equal(missing.purchase_cny,null);
console.log('Profit form boundary checks passed (rates, zero/negative costs, positive FX, integer units, missing costs).');
