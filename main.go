package main

import (
	_ "github.com/thinkgos/gmb/routers"

	"github.com/astaxie/beego"
	_ "github.com/thinkgos/gmb/apps"
)

func init() {
	beego.BConfig.WebConfig.Session.SessionOn = true
	beego.BConfig.CopyRequestBody = true
}

func main() {
	// misc.CfgInit()
	// misc.LogsInit()
	// err := models.DbInit()
	// if err != nil {
	// 	panic(err)
	// }

	beego.Run()
}
