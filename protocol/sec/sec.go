package sec

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/icodeface/grdp/core"
	"github.com/icodeface/grdp/emission"
	"github.com/icodeface/grdp/glog"
	"github.com/icodeface/grdp/protocol/lic"
	"github.com/icodeface/grdp/protocol/t125"
	"github.com/icodeface/grdp/protocol/t125/gcc"
	"github.com/lunixbochs/struc"
	"io"
	"unicode/utf16"
)

/**
 * SecurityFlag
 * @see http://msdn.microsoft.com/en-us/library/cc240579.aspx
 */
const (
	EXCHANGE_PKT       uint16 = 0x0001
	TRANSPORT_REQ             = 0x0002
	TRANSPORT_RSP             = 0x0004
	ENCRYPT                   = 0x0008
	RESET_SEQNO               = 0x0010
	IGNORE_SEQNO              = 0x0020
	INFO_PKT                  = 0x0040
	LICENSE_PKT               = 0x0080
	LICENSE_ENCRYPT_CS        = 0x0200
	LICENSE_ENCRYPT_SC        = 0x0200
	REDIRECTION_PKT           = 0x0400
	SECURE_CHECKSUM           = 0x0800
	AUTODETECT_REQ            = 0x1000
	AUTODETECT_RSP            = 0x2000
	HEARTBEAT                 = 0x4000
	FLAGSHI_VALID             = 0x8000
)

const (
	INFO_MOUSE                  uint32 = 0x00000001
	INFO_DISABLECTRLALTDEL             = 0x00000002
	INFO_AUTOLOGON                     = 0x00000008
	INFO_UNICODE                       = 0x00000010
	INFO_MAXIMIZESHELL                 = 0x00000020
	INFO_LOGONNOTIFY                   = 0x00000040
	INFO_COMPRESSION                   = 0x00000080
	INFO_ENABLEWINDOWSKEY              = 0x00000100
	INFO_REMOTECONSOLEAUDIO            = 0x00002000
	INFO_FORCE_ENCRYPTED_CS_PDU        = 0x00004000
	INFO_RAIL                          = 0x00008000
	INFO_LOGONERRORS                   = 0x00010000
	INFO_MOUSE_HAS_WHEEL               = 0x00020000
	INFO_PASSWORD_IS_SC_PIN            = 0x00040000
	INFO_NOAUDIOPLAYBACK               = 0x00080000
	INFO_USING_SAVED_CREDS             = 0x00100000
	INFO_AUDIOCAPTURE                  = 0x00200000
	INFO_VIDEO_DISABLE                 = 0x00400000
	INFO_CompressionTypeMask           = 0x00001E00
)

const (
	AF_INET  uint16 = 0x00002
	AF_INET6        = 0x0017
)

const (
	PERF_DISABLE_WALLPAPER          uint32 = 0x00000001
	PERF_DISABLE_FULLWINDOWDRAG            = 0x00000002
	PERF_DISABLE_MENUANIMATIONS            = 0x00000004
	PERF_DISABLE_THEMING                   = 0x00000008
	PERF_DISABLE_CURSOR_SHADOW             = 0x00000020
	PERF_DISABLE_CURSORSETTINGS            = 0x00000040
	PERF_ENABLE_FONT_SMOOTHING             = 0x00000080
	PERF_ENABLE_DESKTOP_COMPOSITION        = 0x00000100
)

type RDPExtendedInfo struct {
	ClientAddressFamily uint16 `struc:"little"`
	CbClientAddress     uint16 `struc:"little,sizeof=ClientAddress"`
	ClientAddress       []byte `struc:"[]byte"`
	CbClientDir         uint16 `struc:"little,sizeof=ClientDir"`
	ClientDir           []byte `struc:"[]byte"`
	ClientTimeZone      []byte `struc:"[172]byte"`
	ClientSessionId     uint32 `struc:"litttle"`
	PerformanceFlags    uint32 `struc:"little"`
}

func NewExtendedInfo() *RDPExtendedInfo {
	return &RDPExtendedInfo{
		ClientAddressFamily: AF_INET,
		ClientAddress:       []byte{0, 0},
		ClientDir:           []byte{0, 0},
		ClientTimeZone:      make([]byte, 172),
	}
}

type RDPInfo struct {
	CodePage         uint32
	Flag             uint32
	CbDomain         uint16
	CbUserName       uint16
	CbPassword       uint16
	CbAlternateShell uint16
	CbWorkingDir     uint16
	Domain           []byte
	UserName         []byte
	Password         []byte
	AlternateShell   []byte
	WorkingDir       []byte
	ExtendedInfo     *RDPExtendedInfo
}

func NewRDPInfo() *RDPInfo {
	info := &RDPInfo{
		Flag:           INFO_MOUSE | INFO_UNICODE | INFO_LOGONNOTIFY | INFO_LOGONERRORS | INFO_DISABLECTRLALTDEL | INFO_ENABLEWINDOWSKEY,
		Domain:         []byte{0, 0},
		UserName:       []byte{0, 0},
		Password:       []byte{0, 0},
		AlternateShell: []byte{0, 0},
		WorkingDir:     []byte{0, 0},
		ExtendedInfo:   NewExtendedInfo(),
	}
	return info
}

func (o *RDPInfo) Serialize(hasExtended bool) []byte {
	buff := &bytes.Buffer{}
	core.WriteUInt32LE(o.CodePage, buff)                      // 0000000
	core.WriteUInt32LE(o.Flag, buff)                          // 0530101
	core.WriteUInt16LE(uint16(len(o.Domain)-2), buff)         // 001c
	core.WriteUInt16LE(uint16(len(o.UserName)-2), buff)       // 0008
	core.WriteUInt16LE(uint16(len(o.Password)-2), buff)       //000c
	core.WriteUInt16LE(uint16(len(o.AlternateShell)-2), buff) //0000
	core.WriteUInt16LE(uint16(len(o.WorkingDir)-2), buff)     //0000
	core.WriteBytes(o.Domain, buff)
	core.WriteBytes(o.UserName, buff)
	core.WriteBytes(o.Password, buff)
	core.WriteBytes(o.AlternateShell, buff)
	core.WriteBytes(o.WorkingDir, buff)
	if hasExtended {
		struc.Pack(buff, o.ExtendedInfo)
	}
	return buff.Bytes()
}

type SecurityHeader struct {
	securityFlag   uint16
	securityFlagHi uint16
}

func readSecurityHeader(r io.Reader) *SecurityHeader {
	s := &SecurityHeader{}
	s.securityFlag, _ = core.ReadUint16LE(r)
	s.securityFlagHi, _ = core.ReadUint16LE(r)
	return s
}

type SEC struct {
	emission.Emitter
	transport   core.Transport
	info        *RDPInfo
	machineName string
	clientData  []interface{}
	serverData  []interface{}
}

func NewSEC(t core.Transport) *SEC {
	sec := &SEC{
		*emission.NewEmitter(),
		t,
		NewRDPInfo(),
		"",
		nil,
		nil,
	}

	t.On("close", func() {
		sec.Emit("close")
	}).On("error", func(err error) {
		sec.Emit("error", err)
	})
	return sec
}

func (s *SEC) Read(b []byte) (n int, err error) {
	return s.transport.Read(b)
}

func (s *SEC) Write(b []byte) (n int, err error) {
	return s.transport.Write(b)
}

func (s *SEC) Close() error {
	return s.transport.Close()
}

func (s *SEC) sendFlagged(flag uint16, data []byte) {
	glog.Debug("sendFlagged", hex.EncodeToString(data))
	buff := &bytes.Buffer{}
	core.WriteUInt16LE(flag, buff)
	core.WriteUInt16LE(0, buff)
	core.WriteBytes(data, buff)
	s.transport.Write(buff.Bytes())
}

type Client struct {
	*SEC
	userId    uint16
	channelId uint16
}

func NewClient(t core.Transport) *Client {
	c := &Client{
		SEC: NewSEC(t),
	}
	t.On("connect", c.connect)
	return c
}

func (c *Client) SetUser(user string) {
	buff := &bytes.Buffer{}
	for _, ch := range utf16.Encode([]rune(user)) {
		core.WriteUInt16LE(ch, buff)
	}
	core.WriteUInt16LE(0, buff)
	c.info.UserName = buff.Bytes()
	fmt.Println(c.info.UserName)
}

func (c *Client) SetPwd(pwd string) {
	buff := &bytes.Buffer{}
	for _, ch := range utf16.Encode([]rune(pwd)) {
		core.WriteUInt16LE(ch, buff)
	}
	core.WriteUInt16LE(0, buff)
	c.info.Password = buff.Bytes()
}

func (c *Client) SetDomain(domain string) {
	buff := &bytes.Buffer{}
	for _, ch := range utf16.Encode([]rune(domain)) {
		core.WriteUInt16LE(ch, buff)
	}
	core.WriteUInt16LE(0, buff)
	c.info.Domain = buff.Bytes()
}

func (c *Client) connect(clientData []interface{}, serverData []interface{}, userId uint16, channels []t125.MCSChannelInfo) {
	glog.Debug("sec on connect")
	c.clientData = clientData
	c.serverData = serverData
	c.userId = userId
	for _, channel := range channels {
		if channel.Name == "global" {
			c.channelId = channel.ID
			break
		}
	}
	c.sendInfoPkt()
	c.transport.Once("global", c.recvLicenceInfo)
}

func (c *Client) sendInfoPkt() {
	c.sendFlagged(INFO_PKT, c.info.Serialize(c.clientData[0].(*gcc.ClientCoreData).RdpVersion == gcc.RDP_VERSION_5_PLUS))
}

func (c *Client) recvLicenceInfo(s []byte) {
	glog.Debug("sec recvLicenceInfo", hex.EncodeToString(s))
	r := bytes.NewReader(s)
	if (readSecurityHeader(r).securityFlag & LICENSE_PKT) <= 0 {
		c.Emit("error", errors.New("NODE_RDP_PROTOCOL_PDU_SEC_BAD_LICENSE_HEADER"))
		return
	}

	p := lic.ReadLicensePacket(r)

	switch p.BMsgtype {
	case lic.NEW_LICENSE:
		glog.Info("sec NEW_LICENSE")
		c.Emit("success")
		goto connect
	case lic.ERROR_ALERT:
		glog.Info("sec ERROR_ALERT")
		message := p.LicensingMessage.(*lic.ErrorMessage)
		if message.DwErrorCode == lic.STATUS_VALID_CLIENT && message.DwStateTransaction == lic.ST_NO_TRANSITION {
			goto connect
		}
		goto retry
	case lic.LICENSE_REQUEST:
		c.sendClientNewLicenseRequest()
		goto retry
	case lic.PLATFORM_CHALLENGE:
		c.sendClientChallengeResponse()
		goto retry
	default:
		glog.Error("Not a valid license packet")
		c.Emit("error", errors.New("Not a valid license packet"))
		return
	}

connect:
	c.transport.On("global", c.recvData)
	c.Emit("connect", c.clientData[0].(*gcc.ClientCoreData), c.userId, c.channelId)
	return

retry:
	c.transport.Once("global", c.recvLicenceInfo)
	return
}

func (c *Client) sendClientNewLicenseRequest() {
	glog.Debug("sec sendClientNewLicenseRequest todo")

}

func (c *Client) sendClientChallengeResponse() {
	glog.Debug("sec sendClientChallengeResponse todo")
}

func (c *Client) recvData(s []byte) {
	glog.Debug("sec recvData", hex.EncodeToString(s))
	c.Emit("data", s)
}
