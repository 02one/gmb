package misc

import (
	"os"

	"github.com/Unknwon/com"
	"github.com/go-ini/ini"
	"github.com/pkg/errors"
)

// 串口路径
const (
	usartCfgPath = "conf/usart.conf" // 串口配置
)

var (
	// UartCfg 串口配置变量
	UartCfg *ini.File
)

// CfgInit 配置初始化
func CfgInit() error {
	var err error

	if !com.IsExist(usartCfgPath) {
		FactoryUsartCfg()
	}

	if UartCfg, err = ini.LooseLoad(usartCfgPath); err != nil {
		if UartCfg, err = ini.Load([]byte(usartDefaultCfg)); err != nil {
			return errors.Wrap(err, "usart load default")
		}
	}
	return nil
}

// FactoryUsartCfg 串口配置恢复出厂设置
func FactoryUsartCfg() error {
	f, err := os.Create(usartCfgPath) // 文件存在,直接截断,清空
	if err != nil {
		return err
	}
	f.WriteString(usartDefaultCfg)
	f.Close()
	return nil
}

// SaveUsartCfg 保存串口配置
func SaveUsartCfg() {
	UartCfg.SaveTo(usartCfgPath)
}
