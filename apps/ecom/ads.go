package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Amazon's published Postman collection specifies this v3 read-only operation:
// https://github.com/amzn/ads-advanced-tools-docs/tree/main/postman
func (a *Amazon) Campaigns(ctx context.Context) ([]Obj, error) {
	token, e := a.adsAccessToken(ctx)
	if e != nil {
		return nil, e
	}
	if osEnv("AMAZON_ADS_PROFILE_ID") == "" {
		return nil, httpError{503, "尚未配置广告Profile"}
	}
	out := []Obj{}
	next := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		payload := Obj{"maxResults": 100}
		if next != "" {
			payload["nextToken"] = next
		}
		req, e := http.NewRequestWithContext(ctx, "POST", env("AMAZON_ADS_API_URL", "https://advertising-api.amazon.com")+"/sp/campaigns/list", bytes.NewReader(jsonBytes(payload)))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Amazon-Advertising-API-ClientId", osEnv("AMAZON_ADS_CLIENT_ID"))
		req.Header.Set("Amazon-Advertising-API-Scope", osEnv("AMAZON_ADS_PROFILE_ID"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/vnd.spCampaign.v3+json")
		req.Header.Set("Accept", "application/vnd.spCampaign.v3+json")
		r, e := a.client.Do(req)
		if e != nil {
			return nil, e
		}
		var data struct {
			Campaigns []Obj  `json:"campaigns"`
			Next      string `json:"nextToken"`
		}
		if r.StatusCode >= 300 {
			r.Body.Close()
			return nil, fmt.Errorf("Amazon ads HTTP %d", r.StatusCode)
		}
		e = json.NewDecoder(io.LimitReader(r.Body, 8*1024*1024)).Decode(&data)
		r.Body.Close()
		if e != nil {
			return nil, e
		}
		for _, c := range data.Campaigns {
			budget := 0.0
			if b, ok := c["budget"].(map[string]any); ok {
				budget = number(b["budget"])
			}
			out = append(out, Obj{"campaign_id": text(c["campaignId"]), "name": c["name"], "state": c["state"], "budget": budget, "metrics_available": false})
		}
		if data.Next == "" {
			return out, nil
		}
		if seen[data.Next] {
			return nil, fmt.Errorf("repeated Amazon ads pagination token")
		}
		seen[data.Next] = true
		next = data.Next
	}
	return nil, fmt.Errorf("Amazon ads pagination limit exceeded")
}
