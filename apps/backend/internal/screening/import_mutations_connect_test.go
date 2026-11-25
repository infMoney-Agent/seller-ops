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
			var got any
			switch name {
			case "commit":
				body = Object{"check": 1}
				extra, _ := structpb.NewStruct(body)
				req := connect.NewRequest(&screeningv1.CommitImportRequest{Id: id, Mutation: meta, LegacyFields: extra})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceCommitImportProcedure)
				out, e := client.CommitImport(ctx, req)
				err = e
				if e == nil {
					got = submissionLegacy(out.Msg.Submission)
				}
			case "mapping":
				body = Object{"source_period": "2026-09", "observed_on": "2026-09-03", "mapping": Object{}, "period_start": nil}
				date, month := "2026-09-03", "2026-09"
				req := connect.NewRequest(&screeningv1.UpdateImportSettingsRequest{Id: id, Mutation: meta, Settings: &screeningv1.ImportSettings{SourcePeriod: &month, ObservedOn: &date, Mapping: &structpb.Struct{Fields: map[string]*structpb.Value{}}, ClearFields: []string{"period_start"}}})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceUpdateImportSettingsProcedure)
				out, e := client.UpdateImportSettings(ctx, req)
				err = e
				if e == nil {
					got = submissionLegacy(out.Msg.Submission)
				}
			case "delete":
				preview := connect.NewRequest(&screeningv1.GetImportRemovalPreviewRequest{Id: id})
				sign(preview.Header(), screeningv1connect.ScreeningImportServiceGetImportRemovalPreviewProcedure)
				pv, e := client.GetImportRemovalPreview(ctx, preview)
				if e != nil || pv.Msg.Products != 0 || pv.Msg.SourceRows != 0 {
					t.Fatal("removal preview", e)
				}
				req := connect.NewRequest(&screeningv1.DeleteImportRequest{Id: id, Mutation: meta})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceDeleteImportProcedure)
				out, e := client.DeleteImport(ctx, req)
				err = e
				if e == nil {
					v := out.Msg
					got = Object{"id": v.Id, "deleted": v.Deleted, "removed_products": *v.RemovedProducts, "retained_products": *v.RetainedProducts, "removed_monthly": *v.RemovedMonthly}
				}
			case "retry":
				req := connect.NewRequest(&screeningv1.RetryImportJobRequest{Id: id, Mutation: meta})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceRetryImportJobProcedure)
				out, e := client.RetryImportJob(ctx, req)
				err = e
				if e == nil {
					got = submissionLegacy(out.Msg.Submission)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				uri := "/api/v1/screening/" + path + query
				req, _ := http.NewRequest(method, server.URL+uri, bytes.NewReader(encoded(body)))
				signImportRequest(req.Header, method, uri, svc.cfg.Secret, 601, fmt.Sprint(time.Now().Unix()))
				req.Header.Set("Idempotency-Key", key)
				res, e := server.Client().Do(req)
				if e != nil {
					t.Fatal(e)
				}
				var old, projected any
				e = json.NewDecoder(res.Body).Decode(&old)
				res.Body.Close()
				if e != nil || res.StatusCode >= 300 {
					t.Fatal("HTTP replay failed", res.StatusCode, old, e)
				}
				json.Unmarshal(encoded(got), &projected)
				if !reflect.DeepEqual(old, projected) {
					t.Fatalf("replay mismatch %s %s", encoded(old), encoded(projected))
				}
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
