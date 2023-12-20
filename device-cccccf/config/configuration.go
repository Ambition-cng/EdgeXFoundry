package config

import (
	"net"
)

type ConnectionInfo struct {
	ClientIP string
	Conn     net.Conn
}

type EducationInfo struct {
	StudentID string `json:"student_id"`
	TaskID    string `json:"task_id"`
}

type DeviceInfo struct {
	FacialeegConn   ConnectionInfo
	KeyboardMouse   ConnectionInfo
	DeviceDataLabel EducationInfo
}

type ServiceConfig struct {
	SocketInfo SocketInfo
}

// SimpleCustomConfig is example of service's custom structured configuration that is specified in the service's
// configuration.toml file and Configuration Provider (aka Consul), if enabled.
type SocketInfo struct {
	Host                 string
	SocketServerPort     string
	FacialAndEEGPort     string
	KeyboardAndMousePort string
	BufferSize           int64
	SocketType           string
	TimeOut              int
}

// UpdateFromRaw updates the service's full configuration from raw data received from
// the Service Provider.
func (sw *ServiceConfig) UpdateFromRaw(rawConfig interface{}) bool {
	configuration, ok := rawConfig.(*ServiceConfig)
	if !ok {
		return false //errors.New("unable to cast raw config to type 'ServiceConfig'")
	}

	*sw = *configuration

	return true
}

// Validate ensures your custom configuration has proper values.
// Example of validating the sample custom configuration
func (info *SocketInfo) Validate() error {

	return nil
}
