package screening

import (
	"bytes"
	"connectrpc.com/connect"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/types/known/structpb"
	"net/http"
	"net/http/httptest"
	"reflect"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/contracts/sellerops/screening/v1/screeningv1connect"
	"strings"
	"testing"
	"time"
)

func submissionLegacy(v *screeningv1.ImportJobSubmission) Object {
	b := v.LegacyFields.AsMap()
	b["job_id"] = v.JobId
	b["state"] = v.State
	if v.ImportId != nil {
		b["import_id"] = *v.ImportId
	}
	return b
}
func testImportMutationRPC(t *testing.T, svc *service, server *httptest.Server, client screeningv1connect.ScreeningImportServiceClient) {
	t.Helper()
	ctx := context.Background()
	ids := map[string]string{}
	for i, name := range []string{"commit", "mapping", "delete", "retry"} {
		id := uuid()
		ids[name] = id
		_, err := svc.db.Exec(ctx, "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,state,mapping_hash,created_by,source_period,observed_on,period_type,source_type) VALUES(601,$1,$2,$3,'source.csv',30,'preview',$4,7,'2026-08','2026-08-03','calendar_month','sellersprite')", id, name+".csv", fmt.Sprintf("%064x", i+400), fmt.Sprintf("%064x", i+600))
		if err != nil {
			t.Fatal(err)
		}
	}
	job := uuid()
	_, err := svc.db.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,payload,created_by) VALUES(601,$1,'parse','failed',$1::uuid::text,$2::jsonb,7)", job, encoded(Object{"engine": goEngine, "import_id": ids["retry"]}))
	if err != nil {
		t.Fatal(err)
	}
	sign := func(h http.Header, procedure string) {
		signImportRequest(h, "POST", procedure, svc.cfg.Secret, 601, fmt.Sprint(time.Now().Unix()))
	}
	for _, name := range []string{"commit", "mapping", "delete", "retry"} {
		t.Run("mutation/"+name, func(t *testing.T) {
			id := ids[name]
			method, path := "POST", "imports/"+id+"/commit"
			if name == "mapping" {
				method, path = "PUT", "imports/"+id+"/mapping"
			}
			if name == "delete" {
				method, path = "DELETE", "imports/"+id
			}
			if name == "retry" {
				id = job
				path = "jobs/" + id + "/retry"
			}
			query := "?audit=1"
			key := "rpc-mutation-" + name
			meta := &screeningv1.ImportMutationMetadata{IdempotencyKey: key, OriginalQuery: query}
			body := Object{}
			invoke := func() (any, error) {
				switch name {
				case "commit":
					extra, _ := structpb.NewStruct(Object{"check": 1})
					req := connect.NewRequest(&screeningv1.CommitImportRequest{Id: id, Mutation: meta, LegacyFields: extra})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceCommitImportProcedure)
					out, e := client.CommitImport(ctx, req)
					if e != nil {
						return nil, e
					}
					return submissionLegacy(out.Msg.Submission), nil
				case "mapping":
					date, month := "2026-09-03", "2026-09"
					req := connect.NewRequest(&screeningv1.UpdateImportSettingsRequest{Id: id, Mutation: meta, Settings: &screeningv1.ImportSettings{SourcePeriod: &month, ObservedOn: &date, Mapping: &structpb.Struct{Fields: map[string]*structpb.Value{}}, ClearFields: []string{"period_start"}}})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceUpdateImportSettingsProcedure)
					out, e := client.UpdateImportSettings(ctx, req)
					if e != nil {
						return nil, e
					}
					return submissionLegacy(out.Msg.Submission), nil
				case "delete":
					req := connect.NewRequest(&screeningv1.DeleteImportRequest{Id: id, Mutation: meta})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceDeleteImportProcedure)
					out, e := client.DeleteImport(ctx, req)
					if e != nil {
						return nil, e
					}
					v := out.Msg
					return Object{"id": v.Id, "deleted": v.Deleted, "removed_products": *v.RemovedProducts, "retained_products": *v.RetainedProducts, "removed_monthly": *v.RemovedMonthly}, nil
				default:
					req := connect.NewRequest(&screeningv1.RetryImportJobRequest{Id: id, Mutation: meta})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceRetryImportJobProcedure)
					out, e := client.RetryImportJob(ctx, req)
					if e != nil {
						return nil, e
					}
					return submissionLegacy(out.Msg.Submission), nil
				}
			}
			if name == "delete" {
				preview := connect.NewRequest(&screeningv1.GetImportRemovalPreviewRequest{Id: id})
				sign(preview.Header(), screeningv1connect.ScreeningImportServiceGetImportRemovalPreviewProcedure)
				pv, e := client.GetImportRemovalPreview(ctx, preview)
				if e != nil || pv.Msg.Products != 0 || pv.Msg.SourceRows != 0 {
					t.Fatal("removal preview", e)
				}
			}
			got, err := invoke()
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				replayed, e := invoke()
				if e != nil {
					t.Fatal("RPC replay failed", e)
				}
				var first, again any
				json.Unmarshal(encoded(got), &first)
				json.Unmarshal(encoded(replayed), &again)
				if !reflect.DeepEqual(first, again) {
					t.Fatalf("replay mismatch %s %s", encoded(first), encoded(again))
				}
			}
			uri := "/api/v1/screening/" + path + query
			legacyReq, _ := http.NewRequest(method, server.URL+uri, bytes.NewReader(encoded(body)))
			signImportRequest(legacyReq.Header, method, uri, svc.cfg.Secret, 601, fmt.Sprint(time.Now().Unix()))
			legacyReq.Header.Set("Idempotency-Key", key)
			legacyRes, e := server.Client().Do(legacyReq)
			if e != nil {
				t.Fatal(e)
			}
			legacyRes.Body.Close()
			if legacyRes.StatusCode != 404 {
				t.Fatalf("legacy mutation HTTP endpoint %s still exposed: %d", uri, legacyRes.StatusCode)
			}
			if name == "commit" {
				var count int
				if err = svc.db.QueryRow(ctx, "SELECT count(*) FROM ps_jobs WHERE tenant_id=601 AND kind='import' AND payload->>'import_id'=$1", id).Scan(&count); err != nil || count != 1 {
					t.Fatal("duplicate job", count, err)
				}
				changed, _ := structpb.NewStruct(Object{"check": 2})
				req := connect.NewRequest(&screeningv1.CommitImportRequest{Id: id, Mutation: meta, LegacyFields: changed})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceCommitImportProcedure)
				if _, e := client.CommitImport(ctx, req); connect.CodeOf(e) != connect.CodeAborted {
					t.Fatal("changed key payload not rejected", e)
				}
			}
		})
	}
}

func TestImportMutationGuards(t *testing.T) {
	id := uuid()
	ctx := context.WithValue(context.Background(), identityKey{}, identity{Tenant: 601, User: 7, Role: "member"})
	for _, tc := range []struct {
		name, id string
		meta     *screeningv1.ImportMutationMetadata
		body     Object
		status   int
	}{
		{"missing-key", id, nil, Object{}, 422},
		{"wrong-id", "bad", &screeningv1.ImportMutationMetadata{IdempotencyKey: "key"}, Object{}, 422},
		{"bad-query", id, &screeningv1.ImportMutationMetadata{IdempotencyKey: "key", OriginalQuery: "?a=1\n"}, Object{}, 422},
		{"negative-revision", id, &screeningv1.ImportMutationMetadata{IdempotencyKey: "key"}, Object{"expected_review_revision": json.Number("-1")}, 422},
		{"oversize", id, &screeningv1.ImportMutationMetadata{IdempotencyKey: "key"}, Object{"text": strings.Repeat("x", 256*1024)}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareImportMutation(ctx, tc.id, "POST", "imports/"+id+"/commit", tc.meta, tc.body)
			h, ok := err.(apiError)
			if !ok || h.status != tc.status {
				t.Fatal(err)
			}
		})
	}
	if _, err := prepareImportMutation(context.Background(), id, "POST", "imports/"+id+"/commit", &screeningv1.ImportMutationMetadata{IdempotencyKey: "key"}, Object{}); err == nil {
		t.Fatal("missing identity accepted")
	}
}
