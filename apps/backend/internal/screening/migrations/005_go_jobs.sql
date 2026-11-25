-- Adopt unfinished work before removing the Python/Celery runtime. Historical
-- results and payload fields remain available for audit.
CREATE INDEX ps_product_reference_idx ON ps_products(tenant_id,machine_direction,id) WHERE review_status<>'pending';
UPDATE ps_jobs j SET
 payload=j.payload||jsonb_build_object('engine','go-v1','migrated_from','python','legacy_result',j.result),
 result='{}'::jsonb,
 lease_token=NULL,lease_until=NULL,
 state=CASE WHEN state='running' THEN 'queued' ELSE state END,
 attempts=0,updated_at=now()
WHERE kind IN ('parse','import','screen','profit_batch','export')
 AND state IN ('queued','running','failed') AND NOT(payload ? 'engine');

UPDATE ps_jobs j SET payload=j.payload||jsonb_build_object(
 'policy',to_jsonb(c),
 'targets',COALESCE((SELECT jsonb_agg(target||jsonb_build_object(
    'revision',COALESCE(target->'revision',target->'data_revision'),
    'generation',COALESCE(target->'generation',target->'screen_generation')))
   FROM jsonb_array_elements(COALESCE(j.payload->'targets','[]'::jsonb)) target),'[]'::jsonb))
FROM ps_configs c
WHERE j.kind='screen' AND j.payload->>'migrated_from'='python'
 AND c.tenant_id=j.tenant_id AND c.id=(j.payload->>'policy_id')::uuid;

UPDATE ps_jobs SET state='failed',error=jsonb_build_object('message','旧初筛任务缺少规则快照，请重新创建初筛任务')
WHERE kind='screen' AND payload->>'migrated_from'='python' AND NOT(payload ? 'policy');

UPDATE ps_jobs SET payload=payload||jsonb_build_object('product_ids',
 COALESCE((SELECT jsonb_agg(target->'id') FROM jsonb_array_elements(COALESCE(payload->'targets','[]'::jsonb)) target),'[]'::jsonb))
WHERE kind='profit_batch' AND payload->>'migrated_from'='python';

UPDATE ps_imports i SET state='parsing'
WHERE i.state='uploaded' AND EXISTS(SELECT 1 FROM ps_jobs j WHERE j.tenant_id=i.tenant_id
 AND j.kind='parse' AND j.payload->>'engine'='go-v1' AND j.payload->>'import_id'=i.id::text
 AND j.state IN ('queued','running'));

-- A preview created by the retired implementation must be previewed again;
-- applied batches and their undo events remain unchanged.
UPDATE ps_jobs SET expires_at=now()-interval '1 minute',
 payload=payload||jsonb_build_object('legacy_preview_invalidated',true)
WHERE kind='review' AND state='preview' AND NOT(payload ? 'engine');
