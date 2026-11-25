import { expect, test } from 'bun:test';
import { create } from '@bufbuild/protobuf';
import { ImportRecordSchema, ListImportsResponseSchema, ImportState } from '../src/gen/sellerops/screening/v1/import_pb';
import { legacyImport, legacyImportList, importPagination } from '../src/features/screening/import-list-compat';
test('typed imports retain REST nulls, zero counts, dates and dynamic extensions', () => {
 const extras = {created_at:'2026-08-03T03:04:05.123456+08:00',committed_at:null,file_size:30,tenant_id:501,latest_job_id:null,parse_generation:1};
 const item = create(ImportRecordSchema,{id:'import-id',fileName:'商品.xlsx',state:ImportState.PARTIAL,marketplace:'US',sourcePeriod:'2026-08',sourceType:'sellersprite',periodType:'calendar_month',defaultSalesScope:'unknown',counts:{total:0,valid:0,headers:[],future:{note:'原始数据'}},mapping:{asin:'ASIN'},legacyFields:extras});
 expect(legacyImport(item)).toEqual({...extras,id:'import-id',file_name:'商品.xlsx',state:'partial',marketplace:'US',source_period:'2026-08',observed_on:null,source_type:'sellersprite',period_type:'calendar_month',period_start:null,period_end:null,default_sales_scope:'unknown',counts:{total:0,valid:0,headers:[],future:{note:'原始数据'}},mapping:{asin:'ASIN'}});
 item.observedOn='2026-08-03';expect(legacyImport(item).observed_on).toBe('2026-08-03');
 const result=create(ListImportsResponseSchema,{items:[item],pagination:{page:1,pageSize:50,total:1n}});
 expect(legacyImportList(result).total).toBe(1);
 result.pagination.total=9007199254740992n;expect(()=>legacyImportList(result)).toThrow();
});
test('import pagination retains existing defaults and cap',()=>{
 for(const [query,want] of [['',{page:1,pageSize:50}],['page=-2&page_size=-1',{page:1,pageSize:50}],['page=2&page_size=1000',{page:2,pageSize:100}],['page=no&page_size=5.5',{page:1,pageSize:50}]]) expect(importPagination(new URLSearchParams(query))).toEqual(want);
});
test('every real import state maps to its legacy string',()=>{
 for(const [state,name] of [[ImportState.UPLOADED,'uploaded'],[ImportState.PARSING,'parsing'],[ImportState.PREVIEW,'preview'],[ImportState.IMPORTING,'importing'],[ImportState.COMPLETED,'completed'],[ImportState.PARTIAL,'partial'],[ImportState.FAILED,'failed']])expect(legacyImport(create(ImportRecordSchema,{state})).state).toBe(name);
 expect(()=>legacyImport(create(ImportRecordSchema))).toThrow();
});

import { GetImportResponseSchema, ImportPreviewRowSchema, ListImportRowsResponseSchema, JobRecordSchema, GetJobResponseSchema, ImportRowState, JobState } from '../src/gen/sellerops/screening/v1/import_pb';
import { legacyImportDetail, legacyImportRow, legacyJob } from '../src/features/screening/import-list-compat';
test('detail keeps latest task null or id and rejects a missing record',()=>{
 const item=create(ImportRecordSchema,{id:'import',state:ImportState.PREVIEW});
 expect(legacyImportDetail(create(GetImportResponseSchema,{item})).latest_job_id).toBeNull();
 expect(legacyImportDetail(create(GetImportResponseSchema,{item,latestJobId:'job'})).latest_job_id).toBe('job');
 expect(()=>legacyImportDetail(create(GetImportResponseSchema))).toThrow();
});
test('preview preserves original values, normalized nulls, errors and empty pages',()=>{
 const row=create(ImportPreviewRowSchema,{id:'row',importId:'import',rowNo:2,state:ImportRowState.INVALID,rawData:{'价格':'19.990000','空白':null,'zero':0},legacyFields:{normalized_data:null,errors:[{column:'ASIN',message:'无效'}],tenant_id:501}});
 expect(legacyImportRow(row)).toEqual({id:'row',import_id:'import',row_no:2,state:'invalid',member_path:'',sheet_name:'',product_id:null,raw_data:{'价格':'19.990000','空白':null,'zero':0},normalized_data:null,errors:[{column:'ASIN',message:'无效'}],tenant_id:501});
 expect(legacyImportList(create(ListImportRowsResponseSchema,{items:[],pagination:{page:99,pageSize:100,total:3n}}))).toEqual({items:[],page:99,page_size:100,total:3});
 for(const [state,text] of [[ImportRowState.VALID,'valid'],[ImportRowState.INVALID,'invalid'],[ImportRowState.COMMITTED,'committed']])expect(legacyImportRow(create(ImportPreviewRowSchema,{state})).state).toBe(text);
 expect(()=>legacyImportRow(create(ImportPreviewRowSchema))).toThrow();
});
test('task status preserves zero progress and all terminal and review states',()=>{
 for(const [state,text] of [[JobState.QUEUED,'queued'],[JobState.RUNNING,'running'],[JobState.SUCCEEDED,'succeeded'],[JobState.PARTIAL,'partial'],[JobState.FAILED,'failed'],[JobState.PREVIEW,'preview'],[JobState.APPLIED,'applied'],[JobState.UNDONE,'undone']]){
 const job=legacyJob(create(GetJobResponseSchema,{item:create(JobRecordSchema,{id:'job',kind:'parse',state,legacyFields:{result:null,error:{message:'解析错误'},created_at:'2026-08-01T00:00:00+00:00'}})}));
 expect(job).toEqual({id:'job',kind:'parse',state:text,progress:0,attempts:0,result:null,error:{message:'解析错误'},created_at:'2026-08-01T00:00:00+00:00'});
 }
 expect(()=>legacyJob(create(GetJobResponseSchema))).toThrow();
 expect(()=>legacyJob(create(GetJobResponseSchema,{item:create(JobRecordSchema)}))).toThrow();
});
