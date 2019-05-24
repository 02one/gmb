package controllers

import (
	"crypto/md5"
	"fmt"
	"io"
	"strings"

	"github.com/astaxie/beego"
)

// LoginController 登陆控制器
type LoginController struct {
	beego.Controller
}

// Get get login html
func (this *LoginController) Get() {
	this.TplName = "login.html"
}

// Post 登录
func (this *LoginController) Post() {
	uname := this.Input().Get("username")
	pwd := this.Input().Get("password")
	if len(uname) == 0 || len(pwd) == 0 {
		this.Redirect("/login.html", 302)
		return
	}

	newPwd := GeneratePwd(uname, pwd)
	lname := beego.AppConfig.String("username")
	lpwd := beego.AppConfig.String("password")
	if strings.EqualFold(uname, lname) && strings.EqualFold(newPwd, lpwd) {
		this.SetSession("username", uname)
		this.Redirect("/index.html", 302) // 重定向到首页
	}

	this.Redirect("/login.html", 302)
}

// Delete 退出登陆
func (this *LoginController) Delete() {
	this.DestroySession()
	this.Redirect("/login.html", 302)
}

// GeneratePwd 生成密钥 加盐法 用户名+`@#$%`+md5Pwd+`^&*()`拼接
func GeneratePwd(username, password string) string {
	h := md5.New()
	io.WriteString(h, password)
	md5Pwd := fmt.Sprintf("%x", h.Sum(nil))
	// 加盐值加密
	io.WriteString(h, username)
	io.WriteString(h, `@#$%`)
	io.WriteString(h, md5Pwd)
	io.WriteString(h, `^&*()`)
	return fmt.Sprintf("%x", h.Sum(nil))
}
