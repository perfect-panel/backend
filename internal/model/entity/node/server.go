package node

import (
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type Server struct {
	Id      int64  `gorm:"primary_key"`
	Name    string `gorm:"type:varchar(100);not null;default:'';comment:Server Name"`
	Country string `gorm:"type:varchar(128);not null;default:'';comment:Country"`
	City    string `gorm:"type:varchar(128);not null;default:'';comment:City"`
	//Ratio          float32    `gorm:"type:DECIMAL(4,2);not null;default:0;comment:Traffic Ratio"`
	Address        string     `gorm:"type:varchar(100);not null;default:'';comment:Server Address"`
	Sort           int        `gorm:"type:int;not null;default:0;comment:Sort"`
	Protocols      string     `gorm:"type:text;default:null;comment:Protocol"`
	LastReportedAt *time.Time `gorm:"comment:Last Reported Time"`
	CreatedAt      time.Time  `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt      time.Time  `gorm:"comment:Update Time"`
}

func (*Server) TableName() string {
	return "servers"
}

func (m *Server) BeforeCreate(tx *gorm.DB) error {
	if m.Sort == 0 {
		var maxSort int
		if err := tx.Model(&Server{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		m.Sort = maxSort + 1
	}
	return nil
}

func (m *Server) BeforeDelete(tx *gorm.DB) error {
	if err := tx.Exec("UPDATE servers SET sort = sort - 1 WHERE sort > ?", m.Sort).Error; err != nil {
		return err
	}
	return nil
}

func (m *Server) BeforeUpdate(tx *gorm.DB) error {
	var count int64
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Model(&Server{}).
		Where("sort = ? AND id != ?", m.Sort, m.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 1 {
		// reorder sort
		if err := reorderSortWithServer(tx); err != nil {
			logger.Errorf("[Server] BeforeUpdate reorderSort error: %v", err.Error())
			return err
		}
		// get max sort
		var maxSort int
		if err := tx.Model(&Server{}).Select("MAX(sort)").Scan(&maxSort).Error; err != nil {
			return err
		}
		m.Sort = maxSort + 1
	}
	return nil
}

// MarshalProtocols Marshal server protocols to json
func (m *Server) MarshalProtocols(list []Protocol) error {
	var validate = make(map[string]bool)
	for _, protocol := range list {
		if protocol.Type == "" {
			return errors.New("protocol type is required")
		}
		if _, exists := validate[protocol.Type]; exists {
			return errors.New("duplicate protocol type: " + protocol.Type)
		}
		validate[protocol.Type] = true
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	m.Protocols = string(data)
	return nil
}

// UnmarshalProtocols Unmarshal server protocols from json
func (m *Server) UnmarshalProtocols() ([]Protocol, error) {
	var list []Protocol
	if m.Protocols == "" {
		return list, nil
	}
	err := json.Unmarshal([]byte(m.Protocols), &list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

type Protocol struct {
	Type                    string `json:"type"`
	Port                    uint16 `json:"port"`
	Enable                  bool   `json:"enable"`
	Security                string `json:"security,omitempty"`
	SNI                     string `json:"sni,omitempty"`
	AllowInsecure           bool   `json:"allow_insecure,omitempty"`
	Fingerprint             string `json:"fingerprint,omitempty"`
	RealityServerAddr       string `json:"reality_server_addr,omitempty"`
	RealityServerPort       int    `json:"reality_server_port,omitempty"`
	RealityPrivateKey       string `json:"reality_private_key,omitempty"`
	RealityPublicKey        string `json:"reality_public_key,omitempty"`
	RealityShortId          string `json:"reality_short_id,omitempty"`
	Transport               string `json:"transport,omitempty"`
	Host                    string `json:"host,omitempty"`
	Path                    string `json:"path,omitempty"`
	ServiceName             string `json:"service_name,omitempty"`
	Cipher                  string `json:"cipher,omitempty"`
	ServerKey               string `json:"server_key,omitempty"`
	Flow                    string `json:"flow,omitempty"`
	UoT                     bool   `json:"uot,omitempty"`                   // UDP over TCP
	UoTVersion              int    `json:"uot_version,omitempty"`           // UoT version (1 or 2)
	AcceptProxyProtocol     bool   `json:"accept_proxy_protocol,omitempty"` // accept proxy protocol
	HopPorts                string `json:"hop_ports,omitempty"`
	HopInterval             int    `json:"hop_interval,omitempty"`
	ObfsPassword            string `json:"obfs_password,omitempty"`
	DisableSNI              bool   `json:"disable_sni,omitempty"`
	ReduceRtt               bool   `json:"reduce_rtt,omitempty"`
	UDPRelayMode            string `json:"udp_relay_mode,omitempty"`
	CongestionController    string `json:"congestion_controller,omitempty"`
	Multiplex               string `json:"multiplex,omitempty"`                 // mux, eg: off/low/medium/high
	PaddingScheme           string `json:"padding_scheme,omitempty"`            // padding scheme
	UpMbps                  int    `json:"up_mbps,omitempty"`                   // upload speed limit
	DownMbps                int    `json:"down_mbps,omitempty"`                 // download speed limit
	Obfs                    string `json:"obfs,omitempty"`                      // obfs, 'none', 'http', 'tls'
	ObfsHost                string `json:"obfs_host,omitempty"`                 // obfs host
	ObfsPath                string `json:"obfs_path,omitempty"`                 // obfs path
	XhttpMode               string `json:"xhttp_mode,omitempty"`                // xhttp mode
	XhttpExtra              string `json:"xhttp_extra,omitempty"`               // xhttp extra path
	Encryption              string `json:"encryption,omitempty"`                // encryption，'none', 'mlkem768x25519plus'
	EncryptionMode          string `json:"encryption_mode,omitempty"`           // encryption mode，'native', 'xorpub', 'random'
	EncryptionRtt           string `json:"encryption_rtt,omitempty"`            // encryption rtt，'0rtt', '1rtt'
	EncryptionTicket        string `json:"encryption_ticket,omitempty"`         // encryption ticket
	EncryptionServerPadding string `json:"encryption_server_padding,omitempty"` // encryption server padding
	EncryptionPrivateKey    string `json:"encryption_private_key,omitempty"`    // encryption private key
	EncryptionClientPadding string `json:"encryption_client_padding,omitempty"` // encryption client padding
	EncryptionPassword      string `json:"encryption_password,omitempty"`       // encryption password
	EchEnable               bool   `json:"ech_enable,omitempty"`                // ECH enable
	EchServerName           string `json:"ech_server_name,omitempty"`           // ECH SNI

	Ratio           float64 `json:"ratio,omitempty"`             // Traffic ratio, default is 1
	CertMode        string  `json:"cert_mode,omitempty"`         // Certificate mode, `none`｜`http`｜`dns`｜`self`
	CertDNSProvider string  `json:"cert_dns_provider,omitempty"` // DNS provider for certificate
	CertDNSEnv      string  `json:"cert_dns_env"`                // Environment for DNS provider
}

// Marshal protocol to json
func (m *Protocol) Marshal() ([]byte, error) {
	type Alias Protocol
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// Unmarshal json to protocol
func (m *Protocol) Unmarshal(data []byte) error {
	type Alias Protocol
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	return json.Unmarshal(data, &aux)
}

func reorderSortWithServer(tx *gorm.DB) error {
	var servers []Server
	if err := tx.Order("sort, id").Find(&servers).Error; err != nil {
		return err
	}
	for i, server := range servers {
		if server.Sort != i+1 {
			if err := tx.Exec("UPDATE servers SET sort = ? WHERE id = ?", i+1, server.Id).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
