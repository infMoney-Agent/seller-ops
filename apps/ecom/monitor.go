package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func account(r *http.Request) string {
	return "seller:" + strconv.FormatInt(r.Context().Value(userKey{}).(int64), 10)
}
func (s *Service) monitor(w http.ResponseWriter, r *http.Request, parts []string) (any, error) {
	ctx := r.Context()
	prefix := account(r)
	action := parts[0]
	asin := ""
	if len(parts) > 1 {
		var e error
		asin, e = validateASIN(parts[1])
		if e != nil {
			return nil, e
		}
	}
	switch {
	case r.Method == "GET" && action == "list":
		out := []Obj{}
		iter := s.redis.Scan(ctx, 0, prefix+":monitor:*", 100).Iterator()
		for iter.Next(ctx) {
			raw, e := s.redis.Get(ctx, iter.Val()).Bytes()
			if e == redis.Nil {
				continue
			}
			if e != nil {
				return nil, e
			}
			var v Obj
			if e = json.Unmarshal(raw, &v); e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		if e := iter.Err(); e != nil {
			return nil, e
		}
		sortMonitors(out)
		return Obj{"monitors": out}, nil
	case r.Method == "POST" && action == "add":
		b, e := decode(w, r)
		if e != nil {
			return nil, e
		}
		asin, e = validateASIN(text(b["asin"]))
		if e != nil {
			return nil, e
		}
		platform := text(b["platform"])
		if platform != "" && platform != "amazon" {
			return nil, httpError{422, "不支持的平台"}
		}
		interval := 60
		if b["check_interval_minutes"] != nil {
			interval = int(number(b["check_interval_minutes"]))
		}
		if interval < 60 || interval > 10080 || len([]rune(text(b["label"]))) > 200 {
			return nil, httpError{422, "监控间隔或标签无效"}
		}
		rules := Obj{}
		if b["alert_rules"] != nil {
			var ok bool
			rules, ok = b["alert_rules"].(map[string]any)
			if !ok {
				return nil, httpError{422, "提醒规则无效"}
			}
			for k, v := range rules {
				switch k {
				case "price_drop_pct", "price_rise_pct", "bsr_change_pct", "review_spike":
				default:
					return nil, httpError{422, "不支持的提醒规则"}
				}
				if number(v) < 0 {
					return nil, httpError{422, "提醒阈值不能小于零"}
				}
			}
		}
		value := Obj{"asin": asin, "platform": "amazon", "label": text(b["label"]), "check_interval_minutes": interval, "alert_rules": rules}
		if e = s.redis.Set(ctx, prefix+":monitor:"+asin, jsonBytes(value), 0).Err(); e != nil {
			return nil, e
		}
		return Obj{"status": "added", "asin": asin}, nil
	case r.Method == "DELETE" && action == "remove" && asin != "":
		n, e := s.redis.Del(ctx, prefix+":monitor:"+asin).Result()
		status := "not_found"
		if n > 0 {
			status = "removed"
		}
		return Obj{"status": status, "asin": asin}, e
	case r.Method == "GET" && action == "snapshots" && asin != "":
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var e error
			limit, e = strconv.Atoi(raw)
			if e != nil || limit < 1 || limit > 1000 {
				return nil, httpError{422, "数量须在1到1000之间"}
			}
		}
		rows, e := s.redis.LRange(ctx, prefix+":snapshots:"+asin, 0, int64(limit-1)).Result()
		if e != nil {
			return nil, e
		}
		out := []Obj{}
		for _, raw := range rows {
			var v Obj
			if e = json.Unmarshal([]byte(raw), &v); e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return Obj{"asin": asin, "snapshots": out}, nil
	case r.Method == "POST" && action == "snapshot" && asin != "":
		return s.snapshot(ctx, prefix, asin, false)
	}
	return nil, httpError{404, "接口不存在"}
}
func alerts(current, previous, rules Obj) (bool, string) {
	if previous == nil {
		return false, ""
	}
	old, price := number(previous["price"]), number(current["price"])
	if old > 0 {
		if threshold, ok := rules["price_drop_pct"]; ok {
			change := (old - price) / old * 100
			if change >= number(threshold) {
				return true, fmt.Sprintf("价格下降 %.1f%% ($%.2f → $%.2f)", change, old, price)
			}
		}
		if threshold, ok := rules["price_rise_pct"]; ok {
			change := (price - old) / old * 100
			if change >= number(threshold) {
				return true, fmt.Sprintf("价格上涨 %.1f%% ($%.2f → $%.2f)", change, old, price)
			}
		}
	}
	if oldRank, rank := number(previous["bsr_rank"]), number(current["bsr_rank"]); oldRank > 0 && rank > 0 {
		if threshold, ok := rules["bsr_change_pct"]; ok {
			change := math.Abs(rank-oldRank) / oldRank * 100
			if change >= number(threshold) {
				direction := "下降"
				if rank < oldRank {
					direction = "上升"
				}
				return true, fmt.Sprintf("畅销排名%s %.1f%% (#%.0f → #%.0f)", direction, change, oldRank, rank)
			}
		}
	}
	if threshold, ok := rules["review_spike"]; ok {
		added := int(number(current["review_count"]) - number(previous["review_count"]))
		if float64(added) >= number(threshold) {
			return true, fmt.Sprintf("评论数突增，新增 %d 条", added)
		}
	}
	return false, ""
}
func (s *Service) snapshot(ctx context.Context, prefix, asin string, scheduled bool) (Obj, error) {
	var bytes [16]byte
	if _, e := rand.Read(bytes[:]); e != nil {
		return nil, e
	}
	token := hex.EncodeToString(bytes[:])
	lockKey := prefix + ":snapshot-lock:" + asin
	locked, e := s.redis.SetNX(ctx, lockKey, token, 2*time.Minute).Result()
	if e != nil {
		return nil, e
	}
	if !locked {
		if scheduled {
			return nil, nil
		}
		return nil, httpError{409, "该商品正在采集，请稍后重试"}
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.redis.Eval(release, "if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) end return 0", []string{lockKey}, token)
	}()
	monitored := Obj{"asin": asin, "platform": "amazon", "label": "manual", "check_interval_minutes": 60, "alert_rules": Obj{}}
	raw, e := s.redis.Get(ctx, prefix+":monitor:"+asin).Bytes()
	if e != nil && e != redis.Nil {
		return nil, e
	}
	if scheduled && e == redis.Nil {
		return nil, nil
	}
	if e == nil {
		if e = json.Unmarshal(raw, &monitored); e != nil {
			return nil, e
		}
	}
	var previous Obj
	last, e := s.redis.LIndex(ctx, prefix+":snapshots:"+asin, 0).Bytes()
	if e != nil && e != redis.Nil {
		return nil, e
	}
	if e == nil {
		if e = json.Unmarshal(last, &previous); e != nil {
			return nil, e
		}
		if scheduled {
			stamp, e := time.Parse(time.RFC3339Nano, text(previous["timestamp"]))
			if e == nil && time.Since(stamp) < time.Duration(number(monitored["check_interval_minutes"]))*time.Minute {
				return nil, nil
			}
		}
	}
	product, e := s.adapter.Product(ctx, asin)
	if e != nil {
		return nil, e
	}
	snapshot := Obj{"asin": product.ASIN, "title": product.Title, "price": product.Price, "bsr_rank": product.BSR, "bsr_category": product.Category, "review_count": product.ReviewCount, "rating": product.Rating, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "is_in_stock": true}
	rules, _ := monitored["alert_rules"].(map[string]any)
	triggered, reason := alerts(snapshot, previous, rules)
	snapshot["alert_triggered"] = triggered
	snapshot["alert_reason"] = reason
	_, e = s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.LPush(ctx, prefix+":snapshots:"+asin, jsonBytes(snapshot))
		pipe.LTrim(ctx, prefix+":snapshots:"+asin, 0, 999)
		return nil
	})
	return snapshot, e
}
func (s *Service) Worker(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		iter := s.redis.Scan(ctx, 0, "seller:*:monitor:*", 100).Iterator()
		for iter.Next(ctx) {
			parts := strings.Split(iter.Val(), ":")
			if len(parts) != 4 {
				continue
			}
			if _, e := strconv.ParseInt(parts[1], 10, 64); e != nil {
				continue
			}
			if !asinRE.MatchString(parts[3]) {
				continue
			}
			jobCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			_, e := s.snapshot(jobCtx, strings.Join(parts[:2], ":"), parts[3], true)
			cancel()
			if e != nil {
				log.Printf("monitor snapshot failed user=%s asin=%s type=%T", parts[1], parts[3], e)
			}
		}
		if e := iter.Err(); e != nil && ctx.Err() == nil {
			log.Printf("monitor scan failed: %T", e)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func sortMonitors(rows []Obj) {
	sort.Slice(rows, func(i, j int) bool { return text(rows[i]["asin"]) < text(rows[j]["asin"]) })
}
