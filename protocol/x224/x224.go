package x224

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/icodeface/grdp/core"
	"github.com/icodeface/grdp/emission"
	"github.com/icodeface/grdp/glog"
	"github.com/icodeface/grdp/protocol/tpkt"
	"github.com/lunixbochs/struc"
	"os"
)

// take idea from https://github.com/Madnikulin50/gordp

/**
 * Message type present in X224 packet header
 */
type MessageType byte

const (
	TPDU_CONNECTION_REQUEST MessageType = 0xE0
	TPDU_CONNECTION_CONFIRM             = 0xD0
	TPDU_DISCONNECT_REQUEST             = 0x80
	TPDU_DATA                           = 0xF0
	TPDU_ERROR                          = 0x70
)

/**
 * Type of negotiation present in negotiation packet
 */
type NegotiationType byte

const (
	TYPE_RDP_NEG_REQ     NegotiationType = 0x01
	TYPE_RDP_NEG_RSP                     = 0x02
	TYPE_RDP_NEG_FAILURE                 = 0x03
)

/**
 * Protocols available for x224 layer
 */

const (
	PROTOCOL_RDP       uint32 = 0x00000000
	PROTOCOL_SSL              = 0x00000001
	PROTOCOL_HYBRID           = 0x00000002
	PROTOCOL_HYBRID_EX        = 0x00000008
)

/**
 * Use to negotiate security layer of RDP stack
 * In node-rdpjs only ssl is available
 * @param opt {object} component type options
 * @see request -> http://msdn.microsoft.com/en-us/library/cc240500.aspx
 * @see response -> http://msdn.microsoft.com/en-us/library/cc240506.aspx
 * @see failure ->http://msdn.microsoft.com/en-us/library/cc240507.aspx
 */
type Negotiation struct {
	Type   NegotiationType `struc:"byte"`
	Flag   uint8           `struc:"uint8"`
	Length uint16          `struc:"little"`
	Result uint32          `struc:"little"`
}

func NewNegotiation() *Negotiation {
	return &Negotiation{0, 0, 0x0008 /*constant*/, PROTOCOL_RDP}
}

/**
 * X224 client connection request
 * @param opt {object} component type options
 * @see	http://msdn.microsoft.com/en-us/library/cc240470.aspx
 */
type ClientConnectionRequestPDU struct {
	Len         uint8
	Code        MessageType
	Padding1    uint16
	Padding2    uint16
	Padding3    uint8
	Cookie      []byte
	ProtocolNeg *Negotiation
}

func NewClientConnectionRequestPDU(coockie []byte) *ClientConnectionRequestPDU {
	x := ClientConnectionRequestPDU{0, TPDU_CONNECTION_REQUEST, 0, 0, 0,
		coockie, NewNegotiation()}
	x.Len = uint8(len(x.Serialize()) - 1)
	return &x
}

func (x *ClientConnectionRequestPDU) Serialize() []byte {
	buff := &bytes.Buffer{}
	core.WriteUInt8(x.Len, buff)
	core.WriteUInt8(uint8(x.Code), buff)
	core.WriteUInt16BE(x.Padding1, buff)
	core.WriteUInt16BE(x.Padding2, buff)
	core.WriteUInt8(x.Padding3, buff)

	buff.Write(x.Cookie)
	if x.Len > 14 {
		core.WriteUInt16LE(0x0A0D, buff)
	}
	struc.Pack(buff, x.ProtocolNeg)
	return buff.Bytes()
}

/**
 * X224 Server connection confirm
 * @param opt {object} component type options
 * @see	http://msdn.microsoft.com/en-us/library/cc240506.aspx
 */
type ServerConnectionConfirm struct {
	Len         uint8
	Code        MessageType
	Padding1    uint16
	Padding2    uint16
	Padding3    uint8
	ProtocolNeg *Negotiation
}

/**
 * Header of each data message from x224 layer
 * @returns {type.Component}
 */
type DataHeader struct {
	Header      uint8
	MessageType MessageType
	Separator   uint8
}

func NewDataHeader() *DataHeader {
	return &DataHeader{2, TPDU_DATA /* constant */, 0x80 /*constant*/}
}

/**
 * Common X224 Automata
 * @param presentation {Layer} presentation layer
 */
type X224 struct {
	emission.Emitter
	transport         core.Transport
	requestedProtocol uint32
	selectedProtocol  uint32
	dataHeader        *DataHeader
	host              string
}

var (
	FindSuccess = ""
)

func New(t core.Transport) *X224 {
	x := &X224{
		*emission.NewEmitter(),
		t,
		PROTOCOL_SSL | PROTOCOL_HYBRID,
		PROTOCOL_SSL,
		NewDataHeader(),
		"0",
	}

	t.On("close", func() {
		x.Emit("close")
	}).On("error", func(err error) {
		x.Emit("error", err)
	})

	return x
}

func (x *X224) Read(b []byte) (n int, err error) {
	return x.transport.Read(b)
}

func (x *X224) Write(b []byte) (n int, err error) {
	buff := &bytes.Buffer{}
	err = struc.Pack(buff, x.dataHeader)
	if err != nil {
		return 0, err
	}
	buff.Write(b)
	glog.Debug("x224 write", hex.EncodeToString(buff.Bytes()))
	return x.transport.Write(buff.Bytes())
}

func (x *X224) Close() error {
	return x.transport.Close()
}

func (x *X224) SetRequestedProtocol(p uint32) {
	x.requestedProtocol = p
}

func (x *X224) Connect(host string) error {

	x.host = host
	if x.transport == nil {
		return errors.New("no transport")
	}
	message := NewClientConnectionRequestPDU(make([]byte, 0))
	message.ProtocolNeg.Type = TYPE_RDP_NEG_REQ
	message.ProtocolNeg.Result = uint32(x.requestedProtocol)

	glog.Debug("x224 sendConnectionRequest", hex.EncodeToString(message.Serialize()))
	_, err := x.transport.Write(message.Serialize())
	x.transport.Once("data", x.recvConnectionConfirm)
	return err
}

func savefile(str string) {

	f, err := os.OpenFile("./结果.txt", os.O_WRONLY, 0644)
	if err != nil {
		// 打开文件失败处理

	} else {

		// 查找文件末尾的偏移量
		n, _ := f.Seek(0, 2)

		// 从末尾的偏移量开始写入内容
		_, err = f.WriteAt([]byte(str+"\n"), n)
	}

	defer f.Close()

}

func (x *X224) recvConnectionConfirm(s []byte) {

	glog.Debug("x224 recvConnectionConfirm", hex.EncodeToString(s))
	message := &ServerConnectionConfirm{}
	if err := struc.Unpack(bytes.NewReader(s), message); err != nil {
		glog.Error("ReadServerConnectionConfirm err", err)
		return
	}

	if message.ProtocolNeg.Type == TYPE_RDP_NEG_FAILURE {
		savefile(x.host)
		FindSuccess = x.host
		return
	}

	if message.ProtocolNeg.Type == TYPE_RDP_NEG_RSP {
		savefile(x.host)
		FindSuccess = x.host
		return

	}

	if x.selectedProtocol == PROTOCOL_HYBRID_EX {
		glog.Error("NODE_RDP_PROTOCOL_HYBRID_EX_NOT_SUPPORTED")
		return
	}

	x.transport.On("data", x.recvData)

	if x.selectedProtocol == PROTOCOL_RDP {
		glog.Info("*** RDP security selected ***")
		return
	}

	if x.selectedProtocol == PROTOCOL_SSL {
		glog.Info("*** SSL security selected ***")
		err := x.transport.(*tpkt.TPKT).Conn.StartTLS()
		if err != nil {
			glog.Error("start tls failed", err)
			return
		}
		x.Emit("connect", x.selectedProtocol)
		return
	}

	if x.selectedProtocol == PROTOCOL_HYBRID {
		glog.Info("*** NLA Security selected ***")
		err := x.transport.(*tpkt.TPKT).Conn.StartNLA()
		if err != nil {
			glog.Error("start NLA failed", err)
			return
		}
		x.Emit("connect", x.selectedProtocol)
		return
	}
}

func (x *X224) recvData(s []byte) {
	glog.Debug("x224 recvData", hex.EncodeToString(s), "emit data")
	// x224 header takes 3 bytes
	x.Emit("data", s[3:])
}
