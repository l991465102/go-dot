package httpapi

import (
	"github.com/gogf/gf/v2/net/ghttp"

	v1 "github.com/lzw7758/go-dot/api/system/v1"
)

type SystemController struct{}

func (SystemController) Live(r *ghttp.Request) {
	r.Response.WriteJson(v1.HealthRes{Status: "ok"})
}
