package main

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Receipts and business mutations commit in the same transaction. Advisory
// locking covers the first request too, before a receipt row exists.
func receipt(ctx context.Context, tx pgx.Tx, a identity) (Object, error) {
	if a.RequestKey == "" {
		return nil, nil
	}
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,$2))", a.RequestKey, a.Tenant); e != nil {
		return nil, e
	}
	var hash string
	var user int64
	var raw []byte
	e := tx.QueryRow(ctx, "SELECT request_hash,created_by,result FROM ps_jobs WHERE tenant_id=$1 AND kind='request' AND idempotency_key=$2", a.Tenant, a.RequestKey).Scan(&hash, &user, &raw)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if hash != a.RequestHash || user != a.User {
		return nil, fail(409, "幂等键已用于其他请求")
	}
	return decodeObject(raw), nil
}
func saveReceipt(ctx context.Context, tx pgx.Tx, a identity, out Object) error {
	if a.RequestKey == "" {
		return nil
	}
	_, e := tx.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,request_hash,payload,result,progress,created_by) VALUES($1,$2,'request','succeeded',$3,$4,'{}',$5,100,$6)", a.Tenant, uuid(), a.RequestKey, a.RequestHash, encoded(out), a.User)
	return e
}
