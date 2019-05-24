package controllers

import (
	"github.com/astaxie/beego"
)

// BaseController 通用控制器
type BaseController struct {
	beego.Controller
}

// Prepare  验证用户信息
func (this *BaseController) Prepare() {
	islogin := false
	u := this.GetSession("username")
	if u != nil {
		islogin = true
		this.Data["IsLogin"] = true
	}

	if !islogin {
		this.Redirect("/login.html", 302)
		this.StopRun()
	}
}
