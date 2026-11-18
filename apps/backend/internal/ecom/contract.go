package ecom

import "strings"

func apiContract() Obj {
	paths := Obj{}
	routes := []struct {
		path    string
		methods []string
	}{
		{"product-research/categories", []string{"get"}},
		{"product-research/research", []string{"post"}},
		{"product-research/score/{asin}", []string{"get"}},
		{"listing/languages", []string{"get"}},
		{"listing/generate", []string{"post"}},
		{"listing/optimize", []string{"post"}},
		{"reviews/analyze", []string{"post"}},
		{"reviews/analyze/{asin}", []string{"get"}},
		{"monitor/list", []string{"get"}},
		{"monitor/add", []string{"post"}},
		{"monitor/remove/{asin}", []string{"delete"}},
		{"monitor/snapshots/{asin}", []string{"get"}},
		{"monitor/snapshot/{asin}", []string{"post"}},
		{"ads/optimize", []string{"post"}},
	}
	for _, entry := range routes {
		operations := Obj{}
		for _, method := range entry.methods {
			operation := Obj{"summary": entry.path, "responses": Obj{
				"200": Obj{"description": "成功"},
				"401": Obj{"description": "签名身份无效"},
				"422": Obj{"description": "请求字段无效"},
				"503": Obj{"description": "外部服务尚未配置"},
			}, "security": []Obj{{"internalHMAC": []any{}}}}
			if strings.Contains(entry.path, "{asin}") {
				operation["parameters"] = []Obj{{"name": "asin", "in": "path", "required": true, "schema": Obj{"type": "string", "pattern": "^[A-Za-z0-9]{10}$"}}}
			}
			operations[method] = operation
		}
		paths["/api/v1/"+entry.path] = operations
	}
	return Obj{"openapi": "3.1.0", "info": Obj{"title": "Seller Ops Go E-commerce API", "version": "2.0.0", "description": "浏览器通过 /api/ecom 代理；内部请求由 HMAC 绑定用户ID、时间、方法和含查询参数的路径。"}, "paths": paths, "components": Obj{"securitySchemes": Obj{"internalHMAC": Obj{"type": "apiKey", "in": "header", "name": "x-ecom-signature", "description": "需要一并提交 x-ecom-time 与 x-seller-user；由已验证会话的 Next.js 代理生成。"}}}}
}
