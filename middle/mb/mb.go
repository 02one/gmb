package mb

import (
	"container/list"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
	modbus "github.com/thinkgos/gomodbus"
)

// modbus 请求命令延时和回复超时
const (
	DelayBetweenPolls  = 20 * time.Millisecond    // polls之间的延时
	MinResponseTimeout = 50 * time.Millisecond    // 限定最小回复超时时间
	MaxResponseTimeout = 10000 * time.Millisecond // 限定最大回复超时时间
	readyRequestCnt    = 50                       // 就绪请求最大数量
)

// 虚拟地址最大值
const (
	VirtualCoilDefaultQuantity     = 65535
	VirtualDiscreteDefaultQuantity = 65535
	VirtualInputDefaultQuantity    = 65535
	VirtualHoldingDefaultQuantity  = 65535
)

// Client 客户对象 tcp rtu ascii
type Client struct {
	modbus.Client // 客户端
	lctx          context.Context
	cancel        context.CancelFunc
	reqlist       *list.List          // 请求列表
	mu            sync.Mutex          // 请求列表锁
	readyReq      chan *GatherRequest // 就绪表
	register      *modbus.NodeRegister
	virtual2real  []*GatherPara // 虚拟地址和真实设备地址的互映射
}

// GatherPara 采集参数
type GatherPara struct {
	SlaveID                byte
	HasCoil                bool
	CoilAddress            uint16
	CoilQuantity           uint16
	CoilScanRate           time.Duration // scan rate
	CoilVirtualAddress     uint16
	HasDiscrete            bool
	DiscreteAddress        uint16
	DiscreteQuantity       uint16
	DiscreteScanRate       time.Duration // scan rate
	DiscreteVirtualAddress uint16
	HasInput               bool
	InputAddress           uint16
	InputQuantity          uint16
	InputScanRate          time.Duration // scan rate
	InputVirtualAddress    uint16
	HasHolding             bool
	HoldingAddress         uint16
	HoldingQuantity        uint16
	HoldingScanRate        time.Duration // scan rate
	HoldingVirtualAddress  uint16
}

// GatherRequest 采集轮询请求
type GatherRequest struct {
	SlaveID        byte
	FuncCode       byte
	Address        uint16 // 请求数据用实际地址
	Quantity       uint16 // 请求数量
	VirtualAddress uint16 // 写数据缓存用虚拟地址
	Retry          byte
	ScanRate       time.Duration // scan rate
	scanCnt        time.Duration
	txcnt          uint64 // tx count
	errcnt         uint64 // error count
}

// NewClient 新建客户端
func NewClient(p modbus.ClientProvider) *Client {
	lctx, cancel := context.WithCancel(context.Background())
	return &Client{
		Client:   modbus.NewClient(p),
		lctx:     lctx,
		cancel:   cancel,
		reqlist:  list.New(),
		readyReq: make(chan *GatherRequest, readyRequestCnt),
		register: modbus.NewNodeRegister2(0x01,
			0, VirtualCoilDefaultQuantity, 0, VirtualDiscreteDefaultQuantity,
			0, VirtualInputDefaultQuantity, 0, VirtualHoldingDefaultQuantity),
	}
}

// GetNodeRegister 获得虚拟地址节点
func (this *Client) GetNodeRegister() *modbus.NodeRegister {
	return this.register
}

// Start 启动
func (this *Client) Start() error {
	if err := this.Connect(); err != nil {
		return err
	}
	go this.readPoll()
	go this.scanRequestList()
	return nil
}

// Close 关闭客户端
func (this *Client) Close() error {
	this.cancel()
	return this.Client.Close()
}

// AddReadPoll 新建一个读轮询
func (this *Client) AddReadPoll(gp *GatherPara) error {
	addReqList := func(req *GatherRequest) {
		this.mu.Lock()
		this.reqlist.PushBack(req)
		this.mu.Unlock()
	}

	if gp.HasCoil {
		// 虚拟地址超范围
		if int(gp.CoilVirtualAddress)+int(gp.CoilQuantity) > VirtualCoilDefaultQuantity {
			return errors.New("coils address out of range")
		}
		if gp.CoilScanRate < DelayBetweenPolls {
			gp.CoilScanRate = DelayBetweenPolls
		}
	} else {
		gp.CoilAddress = 0
		gp.CoilQuantity = 0
		gp.CoilScanRate = 0
		gp.CoilVirtualAddress = 0
	}

	if gp.HasDiscrete {
		// 虚拟地址超范围
		if int(gp.DiscreteVirtualAddress)+int(gp.DiscreteQuantity) > VirtualDiscreteDefaultQuantity {
			return errors.New("discrete address out of range")
		}
		if gp.DiscreteScanRate < DelayBetweenPolls {
			gp.DiscreteScanRate = DelayBetweenPolls
		}
	} else {
		gp.DiscreteAddress = 0
		gp.DiscreteQuantity = 0
		gp.DiscreteScanRate = 0
		gp.DiscreteVirtualAddress = 0
	}
	if gp.HasInput {
		if int(gp.InputVirtualAddress)+int(gp.InputQuantity) > VirtualInputDefaultQuantity {
			return errors.New("input address out of range")
		}
		if gp.InputScanRate < DelayBetweenPolls {
			gp.InputScanRate = DelayBetweenPolls
		}
	} else {
		gp.InputAddress = 0
		gp.InputQuantity = 0
		gp.InputVirtualAddress = 0
		gp.InputScanRate = 0
	}
	if gp.HasHolding {
		if int(gp.HoldingVirtualAddress)+int(gp.HoldingQuantity) > VirtualHoldingDefaultQuantity {
			return errors.New("input address out of range")
		}
		if gp.HoldingScanRate < DelayBetweenPolls {
			gp.HoldingScanRate = DelayBetweenPolls
		}
	} else {
		gp.HoldingAddress = 0
		gp.HoldingQuantity = 0
		gp.HoldingVirtualAddress = 0
		gp.HoldingScanRate = 0
	}

	// 添加coils采集
	virtualAddress := gp.CoilVirtualAddress
	address := gp.CoilAddress
	remain := int(gp.CoilQuantity)
	for remain > 0 {
		count := remain
		if count > modbus.ReadBitsQuantityMax {
			count = modbus.ReadBitsQuantityMax
		}
		addReqList(&GatherRequest{
			SlaveID:        gp.SlaveID,
			FuncCode:       modbus.FuncCodeReadCoils,
			Address:        address,
			Quantity:       uint16(count),
			VirtualAddress: virtualAddress,
			ScanRate:       gp.CoilScanRate,
		})
		address += uint16(count)
		virtualAddress += uint16(count)
		remain -= count
	}
	// 添加discrete采集
	virtualAddress = gp.DiscreteVirtualAddress
	address = gp.DiscreteAddress
	remain = int(gp.DiscreteQuantity)
	for remain > 0 {
		count := remain
		if remain > modbus.ReadBitsQuantityMax {
			count = modbus.ReadBitsQuantityMax
		}

		addReqList(&GatherRequest{
			SlaveID:        gp.SlaveID,
			FuncCode:       modbus.FuncCodeReadDiscreteInputs,
			Address:        address,
			Quantity:       uint16(count),
			VirtualAddress: virtualAddress,
			ScanRate:       gp.DiscreteScanRate,
		})
		address += uint16(count)
		virtualAddress += uint16(count)
		remain -= count
	}
	// 输入寄存器采集
	virtualAddress = gp.InputVirtualAddress
	address = gp.InputAddress
	remain = int(gp.InputQuantity)
	for remain > 0 {
		count := remain
		if remain > modbus.ReadRegQuantityMax {
			count = modbus.ReadRegQuantityMax
		}
		addReqList(&GatherRequest{
			SlaveID:        gp.SlaveID,
			FuncCode:       modbus.FuncCodeReadInputRegisters,
			Address:        address,
			Quantity:       uint16(count),
			VirtualAddress: virtualAddress,
			ScanRate:       gp.InputScanRate,
		})
		address += uint16(count)        // 地址偏移
		virtualAddress += uint16(count) // 虚拟地址偏移
		remain -= count                 // 剩下
	}
	// 保持寄存器采集
	virtualAddress = gp.HoldingVirtualAddress
	address = gp.HoldingAddress
	remain = int(gp.HoldingQuantity)
	for remain > 0 {
		count := remain
		if remain > modbus.ReadRegQuantityMax {
			count = modbus.ReadRegQuantityMax
		}
		addReqList(&GatherRequest{
			SlaveID:        gp.SlaveID,
			FuncCode:       modbus.FuncCodeReadHoldingRegisters,
			Address:        address,
			Quantity:       uint16(count),
			VirtualAddress: virtualAddress,
			ScanRate:       gp.HoldingScanRate,
		})
		address += uint16(count)        // 地址偏移
		virtualAddress += uint16(count) // 虚拟地址偏移
		remain -= count                 // 剩下
	}
	this.virtual2real = append(this.virtual2real, gp)
	return nil
}

// 虚拟地址转实际设备地址
func (this *Client) virtual2realPara(isHolding bool, vaddr uint16) (byte, uint16, error) {
	for _, para := range this.virtual2real {
		if !isHolding {
			if vaddr >= para.CoilVirtualAddress && vaddr < para.CoilVirtualAddress+para.CoilQuantity {
				return para.SlaveID, para.CoilAddress + (vaddr - para.CoilVirtualAddress), nil
			}
		} else {
			if vaddr >= para.HoldingVirtualAddress && vaddr < para.HoldingVirtualAddress+para.HoldingQuantity {
				return para.SlaveID, para.HoldingAddress + (vaddr - para.HoldingVirtualAddress), nil
			}
		}
	}
	return 0, 0, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataAddress}
}

// FuncWriteSingleCoil 服务回调,写线圈
func (this *Client) FuncWriteSingleCoil(_ *modbus.NodeRegister, data []byte) ([]byte, error) {
	if len(data) != modbus.FuncWriteMinSize {
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}
	vaddr := binary.BigEndian.Uint16(data)
	slaveID, raddr, err := this.virtual2realPara(false, vaddr)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(data, raddr) // 将实际地址填上去
	_, err = this.Send(slaveID, &modbus.ProtocolDataUnit{
		FuncCode: modbus.FuncCodeWriteSingleCoil,
		Data:     data,
	})
	binary.BigEndian.PutUint16(data, vaddr) // 将虚拟地址填回去作回复
	return data, err
}

// FuncWriteMultiCoil 服务回调,写多个线圈
func (this *Client) FuncWriteMultiCoil(_ *modbus.NodeRegister, data []byte) ([]byte, error) {
	if len(data) < modbus.FuncWriteMinSize {
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}
	vaddr := binary.BigEndian.Uint16(data)
	slaveID, raddr, err := this.virtual2realPara(false, vaddr)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(data, raddr) // 将实际地址填上去
	_, err = this.Send(slaveID, &modbus.ProtocolDataUnit{
		FuncCode: modbus.FuncCodeWriteMultipleCoils,
		Data:     data,
	})
	binary.BigEndian.PutUint16(data, vaddr) // 将虚拟地址填回去作回复
	return data[0:4], err
}

// FuncWriteSingleRegister 服务回调,写单个寄存器
func (this *Client) FuncWriteSingleRegister(_ *modbus.NodeRegister, data []byte) ([]byte, error) {
	if len(data) != modbus.FuncWriteMinSize {
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}
	vaddr := binary.BigEndian.Uint16(data)
	slaveID, raddr, err := this.virtual2realPara(true, vaddr)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(data, raddr) // 将实际地址填上去
	_, err = this.Send(slaveID, &modbus.ProtocolDataUnit{
		FuncCode: modbus.FuncCodeWriteSingleRegister,
		Data:     data,
	})
	if err != nil {
		logs.Debug(err)
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}
	binary.BigEndian.PutUint16(data, vaddr) // 将虚拟地址填回去作回复
	return data[:4], err
}

// FuncWriteMultiHoldingRegisters 服务回调,写多个寄存器
func (this *Client) FuncWriteMultiHoldingRegisters(_ *modbus.NodeRegister, data []byte) ([]byte, error) {
	if len(data) < modbus.FuncWriteMultiMinSize {
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}
	vaddr := binary.BigEndian.Uint16(data)
	slaveID, raddr, err := this.virtual2realPara(true, vaddr)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(data, raddr) // 将实际地址填上去
	_, err = this.Send(slaveID, &modbus.ProtocolDataUnit{
		FuncCode: modbus.FuncCodeWriteMultipleRegisters,
		Data:     data,
	})
	binary.BigEndian.PutUint16(data, vaddr) // 将虚拟地址填回去作回复
	return data[:4], err
}

// FuncReadWriteMultiHoldingRegisters 服务回调,写多个寄存器
func (this *Client) FuncReadWriteMultiHoldingRegisters(_ *modbus.NodeRegister, data []byte) ([]byte, error) {
	if len(data) < modbus.FuncReadWriteMinSize {
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}
	vreadAddress := binary.BigEndian.Uint16(data)
	vwriteAddress := binary.BigEndian.Uint16(data[4:])
	slaveID, rreadAddress, err := this.virtual2realPara(true, vreadAddress)
	if err != nil {
		return nil, err
	}
	_, rwriteAddress, err := this.virtual2realPara(true, vwriteAddress)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(data, rreadAddress)      // 将实际地址填上去
	binary.BigEndian.PutUint16(data[4:], rwriteAddress) // 将实际地址填上去
	response, err := this.Send(slaveID, &modbus.ProtocolDataUnit{
		FuncCode: modbus.FuncCodeReadWriteMultipleRegisters,
		Data:     data,
	})

	return response.Data, err
}

// FuncMaskWriteRegisters 屏蔽写寄存器
func (this *Client) FuncMaskWriteRegisters(_ *modbus.NodeRegister, data []byte) ([]byte, error) {
	if len(data) != modbus.FuncMaskWriteMinSize {
		return nil, &modbus.ExceptionError{ExceptionCode: modbus.ExceptionCodeIllegalDataValue}
	}

	vaddr := binary.BigEndian.Uint16(data)
	slaveID, raddr, err := this.virtual2realPara(true, vaddr)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(data, raddr) // 将实际地址填上去
	_, err = this.Send(slaveID, &modbus.ProtocolDataUnit{
		FuncCode: modbus.FuncCodeMaskWriteRegister,
		Data:     data,
	})
	binary.BigEndian.PutUint16(data, vaddr) // 将虚拟地址填回去作回复
	return data, err
}

// 扫描请求列表
func (this *Client) scanRequestList() {
	var req *GatherRequest
	var tmp *list.Element

	for {
		select {
		case <-this.lctx.Done():
			return
		case <-time.After(DelayBetweenPolls):
		}

		this.mu.Lock()
		for e := this.reqlist.Front(); e != nil; e = tmp {
			req = e.Value.(*GatherRequest)
			req.scanCnt += DelayBetweenPolls
			if req.scanCnt > req.ScanRate {
				req.scanCnt = 0
				tmp = e.Next()
				this.reqlist.Remove(e)
				this.mu.Unlock()
				this.readyReq <- req
				this.mu.Lock()
			} else {
				tmp = e.Next()
			}
		}
		this.mu.Unlock()
	}
}

// 读协程
func (this *Client) readPoll() {
	var results []byte
	var cureq *GatherRequest
	var err error

	for {
		select {
		case <-this.lctx.Done():
			return
		case cureq = <-this.readyReq: // 查看是否有准备好的请求
		}
		cureq.txcnt++
		switch cureq.FuncCode {
		// Bit access read
		case modbus.FuncCodeReadCoils:
			results, err = this.ReadCoils(cureq.SlaveID, cureq.Address, cureq.Quantity)
			if err != nil {
				cureq.errcnt++
			} else if err = this.register.WriteCoils(cureq.VirtualAddress, cureq.Quantity, results); err != nil {
				logs.Debug("Wirte local coils failed")
			}

		case modbus.FuncCodeReadDiscreteInputs:
			results, err = this.ReadDiscreteInputs(cureq.SlaveID, cureq.Address, cureq.Quantity)
			if err != nil {
				cureq.errcnt++
			} else if err = this.register.WriteDiscretes(cureq.VirtualAddress, cureq.Quantity, results); err != nil {
				logs.Debug("Wirte local discretes failed")
			}
		// 16-bit access read
		case modbus.FuncCodeReadHoldingRegisters:
			results, err = this.ReadHoldingRegistersBytes(cureq.SlaveID, cureq.Address, cureq.Quantity)
			if err != nil {
				cureq.errcnt++
			} else if err = this.register.WriteHoldingsBytes(cureq.VirtualAddress, cureq.Quantity, results); err != nil {
				logs.Debug("Wirte local hodling failed,", err)
			}
		case modbus.FuncCodeReadInputRegisters:
			results, err = this.ReadInputRegistersBytes(cureq.SlaveID, cureq.Address, cureq.Quantity)
			if err != nil {
				cureq.errcnt++
			} else if err = this.register.WriteInputsBytes(cureq.VirtualAddress, cureq.Quantity, results); err != nil {
				logs.Debug("Wirte local inputs failed,", err)
			}

		// FIFO read
		case modbus.FuncCodeReadFIFOQueue:
			_, err = this.ReadFIFOQueue(cureq.SlaveID, cureq.Address)
			if err != nil {
				cureq.errcnt++
			}
		}

		if cureq.ScanRate > 0 {
			this.mu.Lock()
			this.reqlist.PushBack(cureq)
			this.mu.Unlock()
		}

		// logs.Debug("---------------------------------------------------------------")
		logs.Debug("mb: Tx=%d,Err=%d,ID=%d,F=%d,Address=%d,Quantity=%d,SR=%dms",
			cureq.txcnt, cureq.errcnt, cureq.SlaveID, cureq.FuncCode,
			cureq.Address, cureq.Quantity, cureq.ScanRate/time.Millisecond)
		// if err == nil {
		// 	logs.Debug("funcCode: % x, Value: % x", cureq.FuncCode, results)
		// }
	}
}
